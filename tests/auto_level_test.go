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

func TestAutoLevelCreation(t *testing.T) {
	// 1. Setup SQLite
	dbPath := "test_auto_level.db"
	os.Remove(dbPath)
	defer os.Remove(dbPath)

	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("Failed to init store: %v", err)
	}

	// 2. Setup Mock Exchange with Liquidity
	mockEx := &MockExchange{
		Price: 50000.0,
		OrderBook: &domain.OrderBook{
			Symbol: "BTCUSDT",
			Bids: []domain.OrderBookEntry{
				{Price: 49500, Size: 10.0}, // Cluster 1
				{Price: 49000, Size: 5.0},
			},
			Asks: []domain.OrderBookEntry{
				{Price: 50500, Size: 2.0},
				{Price: 51000, Size: 1.0},
			},
		},
	}

	// 3. Setup Service
	marketService := usecase.NewMarketService(mockEx)
	svc := usecase.NewLevelService(store, store, mockEx, marketService)

	// 4. Create Initial Auto-Level
	ctx := context.Background()
	level := &domain.Level{
		ID:                       "auto-level-1",
		Exchange:                 "mock",
		Symbol:                   "BTCUSDT",
		LevelPrice:               50000.0,
		BaseSize:                 0.1,
		CoolDownMs:               0,
		StopLossAtBase:           true,
		MaxConsecutiveBaseCloses: 1,    // Fail after 1 close
		BaseCloseCooldownMs:      1000, // 1 sec cooldown
		IsAuto:                   true, // Created by system
		AutoModeEnabled:          true, // Enable auto-recreation
		CreatedAt:                time.Now(),
	}
	if err := store.SaveLevel(ctx, level); err != nil {
		t.Fatalf("Failed to save level: %v", err)
	}

	// 4b. Create Tiers
	tiers := &domain.SymbolTiers{
		Exchange:  "mock",
		Symbol:    "BTCUSDT",
		Tier1Pct:  0.005,  // 0.5%
		Tier2Pct:  0.003,  // 0.3%
		Tier3Pct:  0.0015, // 0.15%
		UpdatedAt: time.Now(),
	}
	if err := store.SaveSymbolTiers(ctx, tiers); err != nil {
		t.Fatalf("Failed to save tiers: %v", err)
	}

	// 4c. Update Cache
	if err := svc.UpdateCache(ctx); err != nil {
		t.Fatalf("Failed to update cache: %v", err)
	}

	// 5. Trigger Failure (Base Close)
	// Open Short (Tier 1 at 49750)
	// T1: 50000 * (1 - 0.005) = 49750
	if err := svc.ProcessTick(ctx, "mock", "BTCUSDT", 49700.0); err != nil {
		t.Fatalf("Tick 1 failed: %v", err)
	}
	if err := svc.ProcessTick(ctx, "mock", "BTCUSDT", 49760.0); err != nil { // Cross T1 Up -> Open Short
		t.Fatalf("Tick 2 failed: %v", err)
	}

	// Verify Open
	trades, _ := store.ListTrades(ctx, 10)
	if len(trades) != 1 {
		t.Fatalf("Expected 1 trade (Open), got %d", len(trades))
	}

	// Close at Base (50000) -> Triggers "Base Close" logic
	if err := svc.ProcessTick(ctx, "mock", "BTCUSDT", 50000.0); err != nil {
		t.Fatalf("Tick 3 failed: %v", err)
	}

	// 6. Wait for Async Auto-Level Creation
	// The logic runs in a goroutine, so we need to wait/poll
	time.Sleep(200 * time.Millisecond)

	// 7. Verify Old Level Deleted
	oldLevel, err := store.GetLevel(ctx, "auto-level-1")
	if err == nil {
		t.Errorf("Expected old level to be deleted, but found it: %v", oldLevel)
	}

	// 8. Verify New Levels Created
	levels, err := store.ListLevels(ctx)
	if err != nil {
		t.Fatalf("Failed to list levels: %v", err)
	}
	if len(levels) != 2 {
		t.Fatalf("Expected 2 levels (High/Low), got %d", len(levels))
	}

	for _, newLevel := range levels {
		if newLevel.ID == "auto-level-1" {
			t.Errorf("New level has same ID as old level")
		}
		if !newLevel.IsAuto {
			t.Errorf("New level should be IsAuto=true")
		}
		if !newLevel.AutoModeEnabled {
			t.Errorf("New level should have AutoModeEnabled=true")
		}
		if newLevel.Source != "auto-split" {
			t.Errorf("New level Source should be 'auto-split', got '%s'", newLevel.Source)
		}
	}

	// 9. Verify Price Logic (Observed Range)
	// Ticks: 49700 (Low), 49760, 50000 (High/Close).
	// High: 50000. Low: 49700.
	var foundHigh, foundLow bool
	for _, l := range levels {
		if l.LevelPrice == 50000.0 {
			foundHigh = true
		} else if l.LevelPrice == 49700.0 {
			foundLow = true
		}
	}

	if !foundHigh {
		t.Error("Expected High level at 50000.0")
	}
	if !foundLow {
		t.Error("Expected Low level at 49700.0")
	}
}
