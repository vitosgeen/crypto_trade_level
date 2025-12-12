package tests

import (
	"context"
	"testing"
	"time"

	"github.com/vitos/crypto_trade_level/internal/domain"
	"github.com/vitos/crypto_trade_level/internal/infrastructure/storage"
	"github.com/vitos/crypto_trade_level/internal/usecase"
)


func (m *MockExchange) PlaceOrder(ctx context.Context, order *domain.Order) (*domain.Order, error) {
return order, nil
}

func (m *MockExchange) GetOrder(ctx context.Context, symbol, orderID string) (*domain.Order, error) {
return &domain.Order{OrderID: orderID, Symbol: symbol, Status: "Filled"}, nil
}

func (m *MockExchange) CancelOrder(ctx context.Context, symbol, orderID string) error {
return nil
}

func TestSplitLimit(t *testing.T) {
	// 1. Setup DB
	dbPath := ":memory:"
	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	// 2. Setup Service
	mockEx := &MockExchange{}
	marketService := usecase.NewMarketService(mockEx)
	svc := usecase.NewLevelService(store, store, mockEx, marketService)

	ctx := context.Background()
	symbol := "BTCUSDT"

	// 3. Create Level 1 (Parent)
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

	// 4. Trigger Split on Level 1 (Count 1 -> 2)
	// Should succeed.
	err = svc.SplitLevel(ctx, l1, 50100, 49900)
	if err != nil {
		t.Fatalf("Failed to split level 1: %v", err)
	}

	// Verify Count = 2
	count, _ := store.CountActiveLevels(ctx, symbol)
	if count != 2 {
		t.Fatalf("Expected 2 active levels after split, got %d", count)
	}

	// Verify Parent Deleted
	_, err = store.GetLevel(ctx, "l1")
	if err == nil {
		t.Fatal("Expected parent level l1 to be deleted")
	}

	// 5. Get one of the new levels
	levels, _ := store.ListLevels(ctx)
	var l2 *domain.Level
	for _, l := range levels {
		if l.Symbol == symbol {
			l2 = l
			break
		}
	}
	if l2 == nil {
		t.Fatal("No levels found after split")
	}

	// 6. Trigger Split on Level 2 (Count 2 -> 2)
	// Should Succeed (Replace existing auto levels)
	err = svc.SplitLevel(ctx, l2, l2.LevelPrice+100, l2.LevelPrice-100)
	if err != nil {
		t.Fatalf("SplitLevel returned error: %v", err)
	}

	// Verify Count still 2
	count, _ = store.CountActiveLevels(ctx, symbol)
	if count != 2 {
		t.Fatalf("Expected count to remain 2, got %d", count)
	}

	// Verify l2 is deleted
	_, err = store.GetLevel(ctx, l2.ID)
	if err == nil {
		t.Fatal("Expected level l2 to be deleted")
	}
}
