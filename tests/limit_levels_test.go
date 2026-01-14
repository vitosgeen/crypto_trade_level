package tests

import (
	"context"
	"testing"
	"time"

	"github.com/vitos/crypto_trade_level/internal/domain"
	"github.com/vitos/crypto_trade_level/internal/infrastructure/storage"
	"github.com/vitos/crypto_trade_level/internal/usecase"
)

func TestLevelLimit(t *testing.T) {
	// 1. Setup DB
	dbPath := ":memory:"
	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	// 2. Setup Service
	mockEx := &MockExchange{}
	marketService := usecase.NewMarketService(mockEx, store)
	svc := usecase.NewLevelService(store, store, mockEx, marketService)

	ctx := context.Background()
	symbol := "BTCUSDT"

	// 3. Create Level 1
	l1 := &domain.Level{
		ID:         "l1",
		Exchange:   "mock",
		Symbol:     symbol,
		LevelPrice: 50000,
		CreatedAt:  time.Now(),
	}
	if err := svc.CreateLevel(ctx, l1); err != nil {
		t.Fatalf("Failed to create level 1: %v", err)
	}

	// 4. Create Level 2
	l2 := &domain.Level{
		ID:         "l2",
		Exchange:   "mock",
		Symbol:     symbol,
		LevelPrice: 51000,
		CreatedAt:  time.Now(),
	}
	if err := svc.CreateLevel(ctx, l2); err != nil {
		t.Fatalf("Failed to create level 2: %v", err)
	}

	// 5. Create Level 3 (Should Succeed)
	l3 := &domain.Level{
		ID:         "l3",
		Exchange:   "mock",
		Symbol:     symbol,
		LevelPrice: 52000,
		CreatedAt:  time.Now(),
	}
	err = svc.CreateLevel(ctx, l3)
	if err != nil {
		t.Fatalf("Expected success creating level 3, got error: %v", err)
	}

	// 6. Verify Count
	count, _ := store.CountActiveLevels(ctx, symbol)
	if count != 3 {
		t.Fatalf("Expected 3 active levels, got %d", count)
	}
}
