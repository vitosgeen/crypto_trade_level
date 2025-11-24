package usecase_test

import (
	"context"
	"testing"

	"github.com/vitos/crypto_trade_level/internal/domain"
	"github.com/vitos/crypto_trade_level/internal/usecase"
)

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
