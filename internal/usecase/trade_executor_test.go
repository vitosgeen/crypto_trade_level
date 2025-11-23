package usecase_test

import (
	"context"
	"testing"

	"github.com/vitos/crypto_trade_level/internal/domain"
	"github.com/vitos/crypto_trade_level/internal/usecase"
)

type MockExchange struct {
	BuyCalled  bool
	SellCalled bool
}

func (m *MockExchange) GetCurrentPrice(ctx context.Context, symbol string) (float64, error) {
	return 0, nil
}

func (m *MockExchange) MarketBuy(ctx context.Context, symbol string, size float64, leverage int, marginType string, stopLoss float64) error {
	m.BuyCalled = true
	return nil
}

func (m *MockExchange) MarketSell(ctx context.Context, symbol string, size float64, leverage int, marginType string, stopLoss float64) error {
	m.SellCalled = true
	return nil
}

func (m *MockExchange) ClosePosition(ctx context.Context, symbol string) error {
	return nil
}

func (m *MockExchange) GetPosition(ctx context.Context, symbol string) (*domain.Position, error) {
	return nil, nil
}

func (m *MockExchange) GetOrderBook(ctx context.Context, symbol string, category string) (*domain.OrderBook, error) {
	return nil, nil
}

func TestTradeExecutor_Execute(t *testing.T) {
	mockEx := &MockExchange{}
	executor := usecase.NewTradeExecutor(mockEx)
	ctx := context.Background()

	// Test Long
	err := executor.Execute(ctx, "BTCUSDT", domain.SideLong, 0.1, 10, "isolated", 0.0)
	if err != nil {
		t.Errorf("Execute Long failed: %v", err)
	}
	if !mockEx.BuyCalled {
		t.Error("Expected MarketBuy to be called")
	}

	// Test Short
	err = executor.Execute(ctx, "BTCUSDT", domain.SideShort, 0.1, 10, "isolated", 0.0)
	if err != nil {
		t.Errorf("Execute Short failed: %v", err)
	}
	if !mockEx.SellCalled {
		t.Error("Expected MarketSell to be called")
	}
}
