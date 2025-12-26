package usecase

import (
	"context"
	"testing"
	"time"

	"github.com/vitos/crypto_trade_level/internal/domain"
	"go.uber.org/zap"
)

// MockExchange for FundingBot tests
type MockFundingExchange struct {
	Tickers       []domain.Ticker
	OrderBook     *domain.OrderBook
	Position      *domain.Position
	PlacedOrders  []*domain.Order
	GetOrderErr   error
	PlaceOrderErr error
}

func (m *MockFundingExchange) GetTickers(ctx context.Context, category string) ([]domain.Ticker, error) {
	return m.Tickers, nil
}

func (m *MockFundingExchange) GetOrderBook(ctx context.Context, symbol, category string) (*domain.OrderBook, error) {
	return m.OrderBook, nil
}

func (m *MockFundingExchange) PlaceOrder(ctx context.Context, order *domain.Order) (*domain.Order, error) {
	if m.PlaceOrderErr != nil {
		return nil, m.PlaceOrderErr
	}
	// assign ID
	order.OrderID = "mock-order-id"
	m.PlacedOrders = append(m.PlacedOrders, order)
	return order, nil
}

func (m *MockFundingExchange) GetOrder(ctx context.Context, symbol, orderID string) (*domain.Order, error) {
	if m.GetOrderErr != nil {
		return nil, m.GetOrderErr
	}
	// Return a filled order by default for tests
	return &domain.Order{OrderID: orderID, Symbol: symbol, Status: "Filled"}, nil
}

func (m *MockFundingExchange) CancelOrder(ctx context.Context, symbol, orderID string) error {
	return nil
}

func (m *MockFundingExchange) GetPosition(ctx context.Context, symbol string) (*domain.Position, error) {
	return m.Position, nil
}

func (m *MockFundingExchange) GetPositions(ctx context.Context) ([]*domain.Position, error) {
	if m.Position != nil && m.Position.Size > 0 {
		return []*domain.Position{m.Position}, nil
	}
	return []*domain.Position{}, nil
}

func (m *MockFundingExchange) ClosePosition(ctx context.Context, symbol string) error {
	m.Position = &domain.Position{Symbol: symbol, Size: 0}
	return nil
}

// Stubs for other interface methods
func (m *MockFundingExchange) ConnectWS() error                                          { return nil }
func (m *MockFundingExchange) Subscribe(channels []string) error                         { return nil }
func (m *MockFundingExchange) OnPriceUpdate(callback func(symbol string, price float64)) {}
func (m *MockFundingExchange) GetPrivateChannels() []string                              { return nil }
func (m *MockFundingExchange) GetKlines(ctx context.Context, symbol, interval string, limit int) ([]domain.Candle, error) {
	return nil, nil
}
func (m *MockFundingExchange) GetCandles(ctx context.Context, symbol, interval string, limit int) ([]domain.Candle, error) {
	return nil, nil
}
func (m *MockFundingExchange) GetCurrentPrice(ctx context.Context, symbol string) (float64, error) {
	return 0, nil
}
func (m *MockFundingExchange) GetInstruments(ctx context.Context, category string) ([]domain.Instrument, error) {
	return nil, nil
}
func (m *MockFundingExchange) GetRecentTrades(ctx context.Context, symbol string, limit int) ([]domain.PublicTrade, error) {
	return nil, nil
}
func (m *MockFundingExchange) MarketBuy(ctx context.Context, symbol string, size float64, leverage int, marginType string, stopLoss float64) error {
	return nil
}
func (m *MockFundingExchange) MarketSell(ctx context.Context, symbol string, size float64, leverage int, marginType string, stopLoss float64) error {
	return nil
}
func (m *MockFundingExchange) OnTradeUpdate(callback func(symbol string, side string, size float64, price float64)) {
}

func TestEvaluate_ProfitableFunding(t *testing.T) {
	logger := zap.NewNop()

	now := time.Now().Unix()
	nextFunding := now // 0 seconds from now

	mockEx := &MockFundingExchange{
		Tickers: []domain.Ticker{
			{Symbol: "BTCUSDT", LastPrice: 50000, FundingRate: 0.0005, NextFundingTime: nextFunding},
		},
	}

	config := FundingBotConfig{
		Symbol:             "BTCUSDT",
		PositionSize:       0.1,
		CountdownThreshold: 60 * time.Second,
		MinFundingRate:     0.0001,
		WallCheckEnabled:   false,
	}

	bot := &FundingBot{
		config:   config,
		exchange: mockEx,
		logger:   logger,
		running:  true,
	}

	err := bot.evaluate(context.Background())
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	// Should place 2 orders: Short Market and Long Limit (TP)
	if len(mockEx.PlacedOrders) != 2 {
		t.Fatalf("Expected 2 orders placed, got %d", len(mockEx.PlacedOrders))
	}

	shortOrder := mockEx.PlacedOrders[0]
	if shortOrder.Side != domain.SideShort || shortOrder.Type != "Market" {
		t.Errorf("Expected Short Market order, got %s %s", shortOrder.Side, shortOrder.Type)
	}

	longOrder := mockEx.PlacedOrders[1]
	if longOrder.Side != domain.SideLong || longOrder.Type != "Limit" {
		t.Errorf("Expected Long Limit order, got %s %s", longOrder.Side, longOrder.Type)
	}
}

func TestEvaluate_WallDetected(t *testing.T) {
	logger := zap.NewNop()
	now := time.Now().Unix()
	nextFunding := now // triggered at 0

	// Wall at 50000 (LastPrice)
	mockEx := &MockFundingExchange{
		Tickers: []domain.Ticker{
			{Symbol: "BTCUSDT", LastPrice: 50000, FundingRate: 0.0005, NextFundingTime: nextFunding},
		},
		OrderBook: &domain.OrderBook{
			Asks: []domain.OrderBookEntry{
				{Price: 50000, Size: 1000.0}, // Huge wall
				{Price: 50100, Size: 1.0},
			},
		},
	}

	config := FundingBotConfig{
		Symbol:                  "BTCUSDT",
		PositionSize:            0.1,
		CountdownThreshold:      60 * time.Second,
		MinFundingRate:          0.0001,
		WallCheckEnabled:        true,
		WallThresholdMultiplier: 1.5,
	}

	bot := &FundingBot{
		config:   config,
		exchange: mockEx,
		logger:   logger,
		running:  true,
	}

	err := bot.evaluate(context.Background())
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	if len(mockEx.PlacedOrders) != 0 {
		t.Fatalf("Expected 0 orders (blocked by wall), got %d", len(mockEx.PlacedOrders))
	}
}

func TestEvaluate_TooEarly(t *testing.T) {
	logger := zap.NewNop()
	now := time.Now().Unix()
	nextFunding := now + 120 // 2 minutes from now

	mockEx := &MockFundingExchange{
		Tickers: []domain.Ticker{
			{Symbol: "BTCUSDT", LastPrice: 50000, FundingRate: 0.0005, NextFundingTime: nextFunding},
		},
	}

	config := FundingBotConfig{
		Symbol:             "BTCUSDT",
		PositionSize:       0.1,
		CountdownThreshold: 60 * time.Second,
		MinFundingRate:     0.0001,
	}

	bot := &FundingBot{
		config:   config,
		exchange: mockEx,
		logger:   logger,
		running:  true,
	}

	err := bot.evaluate(context.Background())
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	if len(mockEx.PlacedOrders) != 0 {
		t.Fatalf("Expected 0 orders (too early), got %d", len(mockEx.PlacedOrders))
	}
}

func TestHandleFundingEvent_NegativeFunding(t *testing.T) {
	logger := zap.NewNop()
	now := time.Now().Unix()

	// Negative funding: -0.05%
	fundingRate := -0.0005
	currentPrice := 50000.0

	mockEx := &MockFundingExchange{
		Tickers: []domain.Ticker{
			{Symbol: "BTCUSDT", LastPrice: currentPrice, FundingRate: fundingRate, NextFundingTime: now},
		},
	}

	config := FundingBotConfig{
		Symbol:             "BTCUSDT",
		PositionSize:       0.1,
		CountdownThreshold: 60 * time.Second,
		MinFundingRate:     0.0001,
	}

	bot := &FundingBot{
		config:   config,
		exchange: mockEx,
		logger:   logger,
		running:  true,
	}

	err := bot.handleFundingEvent(context.Background())
	if err != nil {
		t.Fatalf("HandleFundingEvent failed: %v", err)
	}

	if len(mockEx.PlacedOrders) != 2 {
		t.Fatalf("Expected 2 orders placed, got %d", len(mockEx.PlacedOrders))
	}

	// Test with a higher negative funding rate to trigger the bug
	// tpDistance = -0.01 + 0.005 = -0.005. tpPrice = 50000 * (1 - (-0.005)) = 50250 (ABOVE current price)
	fundingRate = -0.01

	mockEx.Tickers[0].FundingRate = fundingRate
	mockEx.PlacedOrders = nil // reset

	err = bot.handleFundingEvent(context.Background())
	if err != nil {
		t.Fatalf("HandleFundingEvent failed: %v", err)
	}

	if len(mockEx.PlacedOrders) != 2 {
		t.Fatalf("Expected 2 orders placed, got %d", len(mockEx.PlacedOrders))
	}

	longOrder := mockEx.PlacedOrders[1]
	if longOrder.Price >= currentPrice {
		t.Errorf("Expected TP price (%.2f) to be below current price (%.2f) for funding rate %.2f",
			longOrder.Price, currentPrice, fundingRate)
	}
}
