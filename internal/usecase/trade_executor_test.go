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
	LastSymbol string
	LastSize   float64
}

func (m *MockExchange) GetCurrentPrice(ctx context.Context, symbol string) (float64, error) {
	return 0, nil
}
func (m *MockExchange) MarketBuy(ctx context.Context, symbol string, size float64, leverage int, marginType string) error {
	m.BuyCalled = true
	m.LastSymbol = symbol
	m.LastSize = size
	return nil
}
func (m *MockExchange) MarketSell(ctx context.Context, symbol string, size float64, leverage int, marginType string) error {
	m.SellCalled = true
	m.LastSymbol = symbol
	m.LastSize = size
	return nil
}
func (m *MockExchange) ClosePosition(ctx context.Context, symbol string) error { return nil }
func (m *MockExchange) GetPosition(ctx context.Context, symbol string) (*domain.Position, error) {
	return nil, nil
}

func TestTradeExecutor_Execute(t *testing.T) {
	mockEx := &MockExchange{}
	executor := usecase.NewTradeExecutor(mockEx)

	// Test Buy
	err := executor.Execute(context.Background(), "BTCUSDT", domain.SideLong, 1.5, 10, "isolated")
	if err != nil {
		t.Errorf("Execute failed: %v", err)
	}
	if !mockEx.BuyCalled {
		t.Error("Expected MarketBuy to be called")
	}
	if mockEx.LastSize != 1.5 {
		t.Errorf("Expected size 1.5, got %f", mockEx.LastSize)
	}

	// Test Sell
	mockEx.BuyCalled = false
	mockEx.SellCalled = false
	err = executor.Execute(context.Background(), "BTCUSDT", domain.SideShort, 2.0, 10, "isolated")
	if err != nil {
		t.Errorf("Execute failed: %v", err)
	}
	if !mockEx.SellCalled {
		t.Error("Expected MarketSell to be called")
	}
}
