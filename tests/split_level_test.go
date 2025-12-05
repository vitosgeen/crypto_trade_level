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

func TestSplitLevel(t *testing.T) {
	// 1. Setup SQLite
	dbPath := "test_split_level.db"
	os.Remove(dbPath)
	defer os.Remove(dbPath)

	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("Failed to init store: %v", err)
	}

	// 2. Setup Mock Exchange
	mockEx := &MockExchange{Price: 50000.0}

	// 3. Setup Service
	marketService := usecase.NewMarketService(mockEx)
	svc := usecase.NewLevelService(store, store, mockEx, marketService)

	// 4. Create Auto-Level
	ctx := context.Background()
	level := &domain.Level{
		ID:                       "auto-level-split-test",
		Exchange:                 "mock",
		Symbol:                   "BTCUSDT",
		LevelPrice:               50000.0,
		BaseSize:                 0.1,
		StopLossAtBase:           true,
		MaxConsecutiveBaseCloses: 2,
		BaseCloseCooldownMs:      1000,
		IsAuto:                   true,
		AutoModeEnabled:          true,
		CreatedAt:                time.Now(),
	}
	if err := store.SaveLevel(ctx, level); err != nil {
		t.Fatalf("Failed to save level: %v", err)
	}

	// Create Tiers (Required for trading)
	tiers := &domain.SymbolTiers{
		Exchange:  "mock",
		Symbol:    "BTCUSDT",
		Tier1Pct:  0.005,
		Tier2Pct:  0.003,
		Tier3Pct:  0.0015,
		UpdatedAt: time.Now(),
	}
	if err := store.SaveSymbolTiers(ctx, tiers); err != nil {
		t.Fatalf("Failed to save tiers: %v", err)
	}

	// Update Cache
	if err := svc.UpdateCache(ctx); err != nil {
		t.Fatalf("Failed to update cache: %v", err)
	}

	// 5. Simulate Price Movement to establish range
	// High: 51000, Low: 49000
	svc.ProcessTick(ctx, "mock", "BTCUSDT", 50500)
	svc.ProcessTick(ctx, "mock", "BTCUSDT", 51000) // High
	svc.ProcessTick(ctx, "mock", "BTCUSDT", 49000) // Low
	svc.ProcessTick(ctx, "mock", "BTCUSDT", 50000)

	// 6. Simulate Base Close 1
	// Wait for cache to expire
	time.Sleep(1100 * time.Millisecond)
	// Open Short at Level
	mockEx.SetPosition("BTCUSDT", domain.SideShort, 0.1, 50000)
	// Close at Base (Price >= Level)
	svc.ProcessTick(ctx, "mock", "BTCUSDT", 50000)

	// 7. Simulate Base Close 2 (Max Reached)
	// Wait for cache to expire
	time.Sleep(1100 * time.Millisecond)
	// Open Short at Level
	mockEx.SetPosition("BTCUSDT", domain.SideShort, 0.1, 50000)
	// Close at Base
	svc.ProcessTick(ctx, "mock", "BTCUSDT", 50000)

	// 8. Wait for goroutine to finish
	time.Sleep(200 * time.Millisecond)

	// 9. Verify
	levels, err := store.ListLevels(ctx)
	if err != nil {
		t.Fatalf("Failed to list levels: %v", err)
	}

	// Should have 2 levels: 2 New (Original Deleted)
	if len(levels) != 2 {
		t.Errorf("Expected 2 levels, got %d", len(levels))
		for _, l := range levels {
			t.Logf("Level: %s Price: %f", l.ID, l.LevelPrice)
		}
	}

	var highLevel, lowLevel *domain.Level
	for _, l := range levels {
		if l.ID == "auto-level-split-test" {
			t.Error("Original level should be deleted")
		} else if l.LevelPrice == 51000 {
			highLevel = l
		} else if l.LevelPrice == 49000 {
			lowLevel = l
		}
	}

	if highLevel == nil {
		t.Error("High level should be created at 51000")
	} else {
		if !highLevel.IsAuto {
			t.Error("High level should be IsAuto")
		}
		if highLevel.Source != "auto-split" {
			t.Errorf("High level source should be auto-split, got %s", highLevel.Source)
		}
	}

	if lowLevel == nil {
		t.Error("Low level should be created at 49000")
	}
}
