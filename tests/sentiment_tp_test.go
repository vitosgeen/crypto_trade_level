package tests

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/vitos/crypto_trade_level/internal/domain"
	"github.com/vitos/crypto_trade_level/internal/infrastructure/storage"
	"github.com/vitos/crypto_trade_level/internal/usecase"
)

// Local Mock Exchange for this test
type SentimentMockExchange struct {
	MockExchange // Embed the common mock
	RecentTrades []domain.PublicTrade
}

func (m *SentimentMockExchange) GetRecentTrades(ctx context.Context, symbol string, limit int) ([]domain.PublicTrade, error) {
	return m.RecentTrades, nil
}

func TestSentimentTP_Execution(t *testing.T) {
	// 1. Setup SQLite
	dbPath := "test_sentiment_tp_exec.db"
	os.Remove(dbPath)
	defer os.Remove(dbPath)

	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("Failed to init store: %v", err)
	}

	// 2. Setup Mock Exchange with Bullish Data
	mockEx := &SentimentMockExchange{
		MockExchange: MockExchange{
			Price: 50000.0,
			OrderBook: &domain.OrderBook{
				Symbol: "BTCUSDT",
				Bids: []domain.OrderBookEntry{
					{Price: 49900, Size: 100}, // OBI = 1
				},
				Asks: []domain.OrderBookEntry{
					{Price: 50100, Size: 10}, // Small Ask to allow OBI calc
				},
			},
		},
	}
	// Add Bullish Trades
	now := time.Now().UnixMilli()
	for i := 0; i < 100; i++ {
		mockEx.RecentTrades = append(mockEx.RecentTrades, domain.PublicTrade{
			Symbol: "BTCUSDT",
			Side:   "Buy",
			Size:   1.0,
			Price:  50000.0,
			Time:   now,
		})
	}

	// 3. Setup Service
	marketService := usecase.NewMarketService(mockEx, store)
	svc := usecase.NewLevelService(store, store, mockEx, marketService)

	// 4. Create Level with Sentiment TP
	ctx := context.Background()
	level := &domain.Level{
		ID:             "sentiment-level",
		Exchange:       "mock",
		Symbol:         "BTCUSDT",
		LevelPrice:     50000.0,
		BaseSize:       0.1,
		TakeProfitPct:  0.02, // Base 2%
		TakeProfitMode: "sentiment",
		CreatedAt:      time.Now(),
	}
	store.SaveLevel(ctx, level)

	// Create Tiers
	tiers := &domain.SymbolTiers{
		Exchange:  "mock",
		Symbol:    "BTCUSDT",
		Tier1Pct:  0.005,
		Tier2Pct:  0.003,
		Tier3Pct:  0.0015,
		UpdatedAt: time.Now(),
	}
	store.SaveSymbolTiers(ctx, tiers)

	svc.UpdateCache(ctx)

	// 5. Open Long Position
	mockEx.SetPosition("BTCUSDT", domain.SideLong, 0.1, 50000)

	// 6. Verify Score Calculation
	stats, _ := marketService.GetMarketStats(ctx, "BTCUSDT")

	if stats.ConclusionScore < 0.9 {
		t.Logf("Warning: Conclusion Score is %f, expected ~1.0", stats.ConclusionScore)
	}

	// 7. Calculate Expected TP
	// Score ~ 1.0.
	// Multiplier = 1 + (1.0 * 0.5) = 1.5.
	// AdjustedPct = 0.02 * 1.5 = 0.03 (3%).
	// TP Price = 50000 * 1.03 = 51500.

	// 8. Tick at 51200 (2.4% gain). Should NOT close.
	svc.ProcessTick(ctx, "mock", "BTCUSDT", 51200)
	pos, _ := mockEx.GetPosition(ctx, "BTCUSDT")
	if pos == nil || pos.Size == 0 {
		t.Fatal("Position closed prematurely at 51200 (2.4%)")
	}

	// 9. Tick at 51600 (3.2% gain). Should CLOSE.
	svc.ProcessTick(ctx, "mock", "BTCUSDT", 51600)
	pos, _ = mockEx.GetPosition(ctx, "BTCUSDT")
	if pos != nil && pos.Size > 0 {
		t.Fatal("Position FAILED to close at 51600 (3.2%)")
	}
}
