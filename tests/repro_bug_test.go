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

func TestRepro_StopLossAtBase_Failure(t *testing.T) {
	// 1. Setup SQLite
	dbPath := "test_repro.db"
	os.Remove(dbPath)
	defer os.Remove(dbPath)

	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("Failed to init store: %v", err)
	}

	// 2. Setup Mock Exchange
	mockEx := &MockExchange{Price: 100.0}

	// 3. Setup Service
	marketService := usecase.NewMarketService(mockEx)
	svc := usecase.NewLevelService(store, store, mockEx, marketService)

	// 4. Create Level with StopLossAtBase = true
	ctx := context.Background()
	level := &domain.Level{
		ID:             "repro-level-1",
		Exchange:       "mock",
		Symbol:         "BTCUSDT",
		LevelPrice:     100.0,
		BaseSize:       0.1,
		StopLossAtBase: true,  // ENABLED
		StopLossMode:   "app", // Programmatic
		CreatedAt:      time.Now(),
	}
	if err := store.SaveLevel(ctx, level); err != nil {
		t.Fatalf("Failed to save level: %v", err)
	}

	// Save Tiers (required for ProcessTick)
	tiers := &domain.SymbolTiers{
		Exchange:  "mock",
		Symbol:    "BTCUSDT",
		Tier1Pct:  0.01,
		Tier2Pct:  0.02,
		Tier3Pct:  0.03,
		UpdatedAt: time.Now(),
	}
	if err := store.SaveSymbolTiers(ctx, tiers); err != nil {
		t.Fatalf("Failed to save tiers: %v", err)
	}

	// Update Cache
	if err := svc.UpdateCache(ctx); err != nil {
		t.Fatalf("Failed to update cache: %v", err)
	}

	// 5. Simulate Open Position (Long at 100.5)
	// We are Long, Price is above Level (100). Safe.
	mockEx.SetPosition("BTCUSDT", domain.SideLong, 0.1, 100.5)

	// 6. Process Tick: Price drops to 99.0 (Below Base Level 100.0)
	// Should trigger Stop Loss at Base
	if err := svc.ProcessTick(ctx, "mock", "BTCUSDT", 99.0); err != nil {
		t.Fatalf("ProcessTick failed: %v", err)
	}

	// 7. Verify Position Closed
	if mockEx.Position != nil {
		t.Errorf("Expected position to be closed, but it is still active: %+v", mockEx.Position)
	} else {
		t.Log("Success: Position closed correctly.")
	}
}
