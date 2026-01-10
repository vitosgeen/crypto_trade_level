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

type MockTradeRepo struct {
}

func (m *MockTradeRepo) SaveTrade(ctx context.Context, order *domain.Order) error { return nil }
func (m *MockTradeRepo) ListTrades(ctx context.Context, limit int) ([]*domain.Order, error) {
	return nil, nil
}
func (m *MockTradeRepo) SavePositionHistory(ctx context.Context, history *domain.PositionHistory) error {
	return nil
}
func (m *MockTradeRepo) ListPositionHistory(ctx context.Context, limit int) ([]*domain.PositionHistory, error) {
	return nil, nil
}
func (m *MockTradeRepo) SaveTradeSessionLog(ctx context.Context, log *domain.TradeSessionLog) error {
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
	nextFunding := now + 30 // 30 seconds from now (within 60s threshold, strictly > 0)

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
		config:    config,
		exchange:  mockEx,
		tradeRepo: &MockTradeRepo{},
		logger:    logger,
		running:   true,
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
	if shortOrder.Side != domain.SideShort || shortOrder.Type != "Limit" {
		t.Errorf("Expected Short Limit order, got %s %s", shortOrder.Side, shortOrder.Type)
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
		config:    config,
		exchange:  mockEx,
		tradeRepo: &MockTradeRepo{},
		logger:    logger,
		running:   true,
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
		config:    config,
		exchange:  mockEx,
		tradeRepo: &MockTradeRepo{},
		logger:    logger,
		running:   true,
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
		config:    config,
		exchange:  mockEx,
		tradeRepo: &MockTradeRepo{},
		logger:    logger,
		running:   true,
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

func TestEvaluate_ThresholdBoundary(t *testing.T) {
	logger := zap.NewNop()
	now := time.Now().Unix()
	threshold := 60 * time.Second
	thresholdSeconds := int64(threshold.Seconds())

	config := FundingBotConfig{
		Symbol:             "BTCUSDT",
		PositionSize:       0.1,
		CountdownThreshold: threshold,
		MinFundingRate:     0.0001,
		WallCheckEnabled:   false,
	}

	tests := []struct {
		name          string
		nextFunding   int64
		shouldTrigger bool
	}{
		{
			name:          "Just outside threshold (T+1s)",
			nextFunding:   now + thresholdSeconds + 1,
			shouldTrigger: false,
		},
		{
			name:          "Exactly at threshold boundary (T)",
			nextFunding:   now + thresholdSeconds,
			shouldTrigger: true,
		},
		{
			name:          "Just inside threshold (T-1s)",
			nextFunding:   now + thresholdSeconds - 1,
			shouldTrigger: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockEx := &MockFundingExchange{
				Tickers: []domain.Ticker{
					{Symbol: "BTCUSDT", LastPrice: 50000, FundingRate: 0.0005, NextFundingTime: tt.nextFunding},
				},
			}

			bot := &FundingBot{
				config:    config,
				exchange:  mockEx,
				tradeRepo: &MockTradeRepo{},
				logger:    logger,
				running:   true,
			}

			err := bot.evaluate(context.Background())
			if err != nil {
				t.Fatalf("Evaluate failed: %v", err)
			}

			triggered := len(mockEx.PlacedOrders) > 0
			if triggered != tt.shouldTrigger {
				t.Errorf("Expected triggered=%v, got %v", tt.shouldTrigger, triggered)
			}
		})
	}
}

func TestEvaluate_DelayedClosure(t *testing.T) {
	logger := zap.NewNop()
	symbol := "BTCUSDT"

	now := time.Now()
	// Initial state: 1 minute before funding
	nextFunding := now.Unix() + 60

	mockEx := &MockFundingExchange{
		Tickers: []domain.Ticker{
			{Symbol: symbol, LastPrice: 50000, FundingRate: 0.0005, NextFundingTime: nextFunding},
		},
		Position: &domain.Position{Symbol: symbol, Size: 0.1},
	}

	bot := &FundingBot{
		config: FundingBotConfig{
			Symbol:             symbol,
			PositionSize:       0.1,
			CountdownThreshold: 10 * time.Second,
			MinFundingRate:     0.0001,
		},
		exchange:  mockEx,
		tradeRepo: &MockTradeRepo{},
		logger:    logger,
		running:   true,
	}

	// 1. First evaluation: just setting lastNextFundingTime
	err := bot.evaluate(context.Background())
	if err != nil {
		t.Fatalf("First evaluate failed: %v", err)
	}
	if !bot.fundingEventTime.IsZero() {
		t.Fatal("fundingEventTime should be zero before rollover")
	}

	// 2. Rollover occurs: NextFundingTime jumps 8 hours
	mockEx.Tickers[0].NextFundingTime += 8 * 3600
	err = bot.evaluate(context.Background())
	if err != nil {
		t.Fatalf("Second evaluate failed: %v", err)
	}

	if bot.fundingEventTime.IsZero() {
		t.Fatal("fundingEventTime should be set after rollover")
	}
	if mockEx.Position.Size != 0.1 {
		t.Fatal("Position should NOT be closed immediately after rollover")
	}

	// 3. Evaluate after 5 seconds: still shouldn't close
	// We simulate time by overriding bot's locked time? No, we just need time.Since to be < 10s.
	// Since we just set it, it's definitely < 10s.
	err = bot.evaluate(context.Background())
	if err != nil {
		t.Fatalf("Third evaluate failed: %v", err)
	}
	if mockEx.Position.Size != 0.1 {
		t.Fatal("Position should NOT be closed after only 5 seconds (simulated)")
	}

	// 4. Manually advance fundingEventTime to simulate 11 seconds passed
	bot.mu.Lock()
	bot.fundingEventTime = time.Now().Add(-11 * time.Second)
	bot.mu.Unlock()

	err = bot.evaluate(context.Background())
	if err != nil {
		t.Fatalf("Fourth evaluate failed: %v", err)
	}

	if mockEx.Position.Size != 0 {
		t.Fatal("Position SHOULD be closed after 10 seconds passed")
	}
	if !bot.fundingEventTime.IsZero() {
		t.Fatal("fundingEventTime should be reset after closure")
	}
}
