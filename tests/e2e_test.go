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

type MockExchange struct {
	Price      float64
	BuyCalled  bool
	SellCalled bool
	Position   *domain.Position
}

func (m *MockExchange) SetPosition(symbol string, side domain.Side, size, entryPrice float64) {
	m.Position = &domain.Position{
		Symbol:     symbol,
		Side:       side,
		Size:       size,
		EntryPrice: entryPrice,
	}
}

func (m *MockExchange) GetCurrentPrice(ctx context.Context, symbol string) (float64, error) {
	return m.Price, nil
}
func (m *MockExchange) MarketBuy(ctx context.Context, symbol string, size float64, leverage int, marginType string, stopLoss float64) error {
	m.BuyCalled = true
	// Simulate Position Update
	if m.Position == nil {
		m.Position = &domain.Position{
			Symbol:     symbol,
			Side:       domain.SideLong,
			Size:       size,
			EntryPrice: m.Price, // Use current price as entry
		}
	} else {
		// Add to position (simplified average entry price logic omitted for mock unless needed)
		// Update average entry price when adding to long position
		m.Position.EntryPrice = (m.Position.EntryPrice*m.Position.Size + m.Price*size) / (m.Position.Size + size)
		m.Position.Size += size
	}
	return nil
}
func (m *MockExchange) MarketSell(ctx context.Context, symbol string, size float64, leverage int, marginType string, stopLoss float64) error {
	m.SellCalled = true
	// Simulate Position Update
	if m.Position == nil {
		m.Position = &domain.Position{
			Symbol:     symbol,
			Side:       domain.SideShort,
			Size:       size,
			EntryPrice: m.Price,
		}
	} else {
		// Update average entry price when adding to short position
		m.Position.EntryPrice = (m.Position.EntryPrice*m.Position.Size + m.Price*size) / (m.Position.Size + size)
		m.Position.Size += size
	}
	return nil
}
func (m *MockExchange) ClosePosition(ctx context.Context, symbol string) error {
	m.Position = nil // Simulate close
	return nil
}
func (m *MockExchange) GetPosition(ctx context.Context, symbol string) (*domain.Position, error) {
	if m.Position != nil && m.Position.Symbol == symbol {
		return m.Position, nil
	}
	return &domain.Position{Symbol: symbol, Size: 0}, nil
}
func (m *MockExchange) GetCandles(ctx context.Context, symbol, interval string, limit int) ([]domain.Candle, error) {
	return nil, nil
}

func (m *MockExchange) GetOrderBook(ctx context.Context, symbol string, category string) (*domain.OrderBook, error) {
	return nil, nil
}

func (m *MockExchange) OnTradeUpdate(callback func(symbol string, side string, size float64, price float64)) {
}

func (m *MockExchange) GetRecentTrades(ctx context.Context, symbol string, limit int) ([]domain.PublicTrade, error) {
	return nil, nil
}

func (m *MockExchange) GetInstruments(ctx context.Context, category string) ([]domain.Instrument, error) {
	return nil, nil
}

func TestEndToEnd_LevelDefense(t *testing.T) {
	// Enable logs
	// log.SetOutput(os.Stdout) // Default is stderr which go test shows on failure

	// 1. Setup SQLite
	dbPath := "test_e2e.db"
	os.Remove(dbPath)
	defer os.Remove(dbPath)

	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("Failed to init store: %v", err)
	}

	// 2. Setup Mock Exchange
	mockEx := &MockExchange{Price: 51000.0}

	// 3. Setup Service
	marketService := usecase.NewMarketService(mockEx)
	svc := usecase.NewLevelService(store, store, mockEx, marketService)

	// 4. Create Level & Tiers
	ctx := context.Background()
	level := &domain.Level{
		ID:         "test-level-1",
		Exchange:   "mock",
		Symbol:     "BTCUSDT",
		LevelPrice: 50000.0,
		BaseSize:   0.1,
		CoolDownMs: 0,
		CreatedAt:  time.Now(),
	}
	if err := store.SaveLevel(ctx, level); err != nil {
		t.Fatalf("Failed to save level: %v", err)
	}

	tiers := &domain.SymbolTiers{
		Exchange:  "mock",
		Symbol:    "BTCUSDT",
		Tier1Pct:  0.005,  // 50250
		Tier2Pct:  0.003,  // 50150
		Tier3Pct:  0.0015, // 50075
		UpdatedAt: time.Now(),
	}
	if err := store.SaveSymbolTiers(ctx, tiers); err != nil {
		t.Fatalf("Failed to save tiers: %v", err)
	}

	// Update Cache
	if err := svc.UpdateCache(ctx); err != nil {
		t.Fatalf("Failed to update cache: %v", err)
	}

	// Verify Tiers saved
	savedTiers, err := store.GetSymbolTiers(ctx, "mock", "BTCUSDT")
	if err != nil {
		t.Fatalf("Failed to get tiers: %v", err)
	}
	if savedTiers.Tier1Pct != 0.005 {
		t.Fatalf("Saved tiers mismatch: %v", savedTiers)
	}

	// 5. Run Scenario
	// Initial Tick (Price 51000)
	if err := svc.ProcessTick(ctx, "mock", "BTCUSDT", 51000.0); err != nil {
		t.Fatalf("ProcessTick 1 failed: %v", err)
	}

	// Move Price to 50240 (Definitely below Tier1: 50250)
	if err := svc.ProcessTick(ctx, "mock", "BTCUSDT", 50240.0); err != nil {
		t.Fatalf("ProcessTick 2 failed: %v", err)
	}

	// Verify Trade Executed
	if !mockEx.BuyCalled {
		t.Error("Expected MarketBuy to be called for Tier1 trigger")
	}

	// Verify Trade Saved
	trades, err := store.ListTrades(ctx, 10)
	if err != nil {
		t.Fatalf("Failed to list trades: %v", err)
	}
	if len(trades) != 1 {
		t.Errorf("Expected 1 trade, got %d", len(trades))
	} else {
		if trades[0].Side != domain.SideLong {
			t.Errorf("Expected Long trade, got %s", trades[0].Side)
		}
		if trades[0].Size != 0.1 {
			t.Errorf("Expected Size 0.1, got %f", trades[0].Size)
		}
	}
}
