package usecase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/vitos/crypto_trade_level/internal/domain"
	"github.com/vitos/crypto_trade_level/internal/usecase"
)

// MockLevelRepo
type MockLevelRepo struct {
	Levels []*domain.Level
	Tiers  *domain.SymbolTiers
}

func (m *MockLevelRepo) SaveLevel(ctx context.Context, level *domain.Level) error { return nil }
func (m *MockLevelRepo) GetLevel(ctx context.Context, id string) (*domain.Level, error) {
	return m.Levels[0], nil
}
func (m *MockLevelRepo) ListLevels(ctx context.Context) ([]*domain.Level, error) {
	return m.Levels, nil
}
func (m *MockLevelRepo) DeleteLevel(ctx context.Context, id string) error { return nil }
func (m *MockLevelRepo) GetSymbolTiers(ctx context.Context, exchange, symbol string) (*domain.SymbolTiers, error) {
	return m.Tiers, nil
}
func (m *MockLevelRepo) SaveSymbolTiers(ctx context.Context, tiers *domain.SymbolTiers) error {
	return nil
}

// MockTradeRepo
type MockTradeRepo struct{}

func (m *MockTradeRepo) SaveTrade(ctx context.Context, trade *domain.Order) error { return nil }
func (m *MockTradeRepo) ListTrades(ctx context.Context, limit int) ([]*domain.Order, error) {
	return nil, nil
}

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
func (m *MockExchange) GetCandles(ctx context.Context, symbol, interval string, limit int) ([]domain.Candle, error) {
	return nil, nil
}
func (m *MockExchange) OnTradeUpdate(callback func(symbol string, side string, size float64, price float64)) {
}

func TestLevelService_ClosePositionFailure_ResetsState(t *testing.T) {
	// Setup
	level := &domain.Level{
		ID:             "test-level",
		Symbol:         "BTCUSDT",
		Exchange:       "bybit",
		LevelPrice:     10000,
		BaseSize:       0.1,
		StopLossAtBase: true,
	}
	tiers := &domain.SymbolTiers{
		Tier1Pct: 0.005, // 10050
		Tier2Pct: 0.010,
		Tier3Pct: 0.015,
	}

	mockLevelRepo := &MockLevelRepo{Levels: []*domain.Level{level}, Tiers: tiers}
	mockTradeRepo := &MockTradeRepo{}
	mockEx := &MockExchangeForService{} // Reusing MockExchange from trade_executor_test.go if in same package, but we are in usecase_test

	// We need to define MockExchange here since we are in a new file/package (or same package test)
	// Let's redefine minimal mock

	service := usecase.NewLevelService(mockLevelRepo, mockTradeRepo, mockEx)
	ctx := context.Background()

	// Populate Cache
	service.UpdateCache(ctx)

	// 1. Trigger Open (Tier 1)
	// Prev: 10000 (Init), Curr: 10060
	// We need to feed ticks.
	// First tick to init lastPrice
	service.ProcessTick(ctx, "bybit", "BTCUSDT", 10000)

	// Second tick to trigger Open
	mockEx.BuyCalled = false
	service.ProcessTick(ctx, "bybit", "BTCUSDT", 10060)

	if !mockEx.BuyCalled {
		t.Fatal("Expected Buy on Tier 1")
	}

	// 2. Trigger Close (Base Level)
	// Prev: 10060, Curr: 10000
	// Mock ClosePosition to FAIL
	mockEx.CloseError = errors.New("position not found")

	service.ProcessTick(ctx, "bybit", "BTCUSDT", 10000)

	// 3. Trigger Open Again (Re-entry)
	// Prev: 10000, Curr: 10060
	mockEx.BuyCalled = false // Reset
	mockEx.CloseError = nil

	service.ProcessTick(ctx, "bybit", "BTCUSDT", 10060)

	if !mockEx.BuyCalled {
		t.Fatal("Expected Buy (Re-entry) after failed Close. State should have been reset.")
	}
}

// Enhanced MockExchange
type MockExchangeForService struct {
	BuyCalled  bool
	SellCalled bool
	CloseError error
}

func (m *MockExchangeForService) GetCurrentPrice(ctx context.Context, symbol string) (float64, error) {
	return 0, nil
}
func (m *MockExchangeForService) MarketBuy(ctx context.Context, symbol string, size float64, leverage int, marginType string, stopLoss float64) error {
	m.BuyCalled = true
	return nil
}
func (m *MockExchangeForService) MarketSell(ctx context.Context, symbol string, size float64, leverage int, marginType string, stopLoss float64) error {
	m.SellCalled = true
	return nil
}
func (m *MockExchangeForService) ClosePosition(ctx context.Context, symbol string) error {
	return m.CloseError
}
func (m *MockExchangeForService) GetPosition(ctx context.Context, symbol string) (*domain.Position, error) {
	return &domain.Position{Size: 0.1}, nil // Always return position so Close is attempted
}
func (m *MockExchangeForService) GetCandles(ctx context.Context, symbol, interval string, limit int) ([]domain.Candle, error) {
	return nil, nil
}
func (m *MockExchangeForService) GetOrderBook(ctx context.Context, symbol string, category string) (*domain.OrderBook, error) {
	return nil, nil
}
func (m *MockExchangeForService) OnTradeUpdate(callback func(symbol string, side string, size float64, price float64)) {
}
