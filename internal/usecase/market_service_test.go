package usecase

import (
	"context"
	"testing"
	"time"

	"github.com/vitos/crypto_trade_level/internal/domain"
)

// MockExchange for MarketService
type MockExchange struct {
	OrderBook *domain.OrderBook
}

func (m *MockExchange) GetOrderBook(ctx context.Context, symbol string, category string) (*domain.OrderBook, error) {
	return m.OrderBook, nil
}

// Stubs for other methods
func (m *MockExchange) GetCurrentPrice(ctx context.Context, symbol string) (float64, error) {
	return 0, nil
}
func (m *MockExchange) MarketBuy(ctx context.Context, symbol string, size float64, leverage int, marginType string, stopLoss float64) error {
	return nil
}
func (m *MockExchange) MarketSell(ctx context.Context, symbol string, size float64, leverage int, marginType string, stopLoss float64) error {
	return nil
}
func (m *MockExchange) ClosePosition(ctx context.Context, symbol string) error { return nil }
func (m *MockExchange) GetPosition(ctx context.Context, symbol string) (*domain.Position, error) {
	return nil, nil
}
func (m *MockExchange) GetCandles(ctx context.Context, symbol, interval string, limit int) ([]domain.Candle, error) {
	return nil, nil
}
func (m *MockExchange) OnTradeUpdate(callback func(symbol string, side string, size float64, price float64)) {
}

func (m *MockExchange) GetRecentTrades(ctx context.Context, symbol string, limit int) ([]domain.PublicTrade, error) {
	return nil, nil
}

func (m *MockExchange) GetInstruments(ctx context.Context, category string) ([]domain.Instrument, error) {
	return nil, nil
}

func (m *MockExchange) Subscribe(symbols []string) error {
	return nil
}

func TestMarketService_GetMarketStats_DepthAverage(t *testing.T) {
	mockEx := &MockExchange{}
	service := NewMarketService(mockEx)

	// Mock Time
	currentTime := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
	service.timeNow = func() time.Time {
		return currentTime
	}

	ctx := context.Background()
	symbol := "BTCUSDT"

	// 1. First Call: Initial Depth
	// Mid Price: (100+101)/2 = 100.5
	// Range: 0.5% -> +/- 0.5025
	// MinBid: 99.9975, MaxAsk: 101.0025
	//
	// Orders:
	// Bid @ 100 (In) -> 10
	// Bid @ 90 (Out) -> 100 (Should be ignored)
	// Ask @ 101 (In) -> 20
	// Ask @ 110 (Out) -> 200 (Should be ignored)
	mockEx.OrderBook = &domain.OrderBook{
		Bids: []domain.OrderBookEntry{
			{Price: 100, Size: 10},
			{Price: 90, Size: 100},
		},
		Asks: []domain.OrderBookEntry{
			{Price: 101, Size: 20},
			{Price: 110, Size: 200},
		},
	}

	stats, err := service.GetMarketStats(ctx, symbol)
	if err != nil {
		t.Fatalf("GetMarketStats failed: %v", err)
	}

	if stats.DepthBid != 10 || stats.DepthAsk != 20 {
		t.Errorf("Expected Depth 10/20 (filtered), got %.2f/%.2f", stats.DepthBid, stats.DepthAsk)
	}

	// 2. Advance Time by 10s (Trigger new fetch)
	currentTime = currentTime.Add(10 * time.Second)
	// New Depth: Bid: 20, Ask: 40
	mockEx.OrderBook = &domain.OrderBook{
		Bids: []domain.OrderBookEntry{{Price: 100, Size: 20}},
		Asks: []domain.OrderBookEntry{{Price: 101, Size: 40}},
	}

	stats, err = service.GetMarketStats(ctx, symbol)
	if err != nil {
		t.Fatalf("GetMarketStats failed: %v", err)
	}

	// Expected Average:
	// T0: 10/20
	// T10: 20/40
	// Avg Bid: (10+20)/2 = 15
	// Avg Ask: (20+40)/2 = 30
	if stats.DepthBid != 15 || stats.DepthAsk != 30 {
		t.Errorf("Expected Avg Depth 15/30, got %.2f/%.2f", stats.DepthBid, stats.DepthAsk)
	}

	// 3. Advance Time by 30s (Total 40s)
	// T0: 10/20 (Kept)
	// T10: 20/40 (Kept)
	// T40: 30/60 (New)
	currentTime = currentTime.Add(30 * time.Second)
	mockEx.OrderBook = &domain.OrderBook{
		Bids: []domain.OrderBookEntry{{Price: 100, Size: 30}},
		Asks: []domain.OrderBookEntry{{Price: 101, Size: 60}},
	}

	stats, err = service.GetMarketStats(ctx, symbol)
	if err != nil {
		t.Fatalf("GetMarketStats failed: %v", err)
	}

	// Avg Bid: (10+20+30)/3 = 20
	// Avg Ask: (20+40+60)/3 = 40
	if stats.DepthBid != 20 || stats.DepthAsk != 40 {
		t.Errorf("Expected Avg Depth 20/40, got %.2f/%.2f", stats.DepthBid, stats.DepthAsk)
	}

	// 4. Advance Time by 40s (Total 80s)
	// Cutoff: T20
	// T0 (Pruned)
	// T10 (Pruned)
	// T40: 30/60 (Kept)
	// T80: 40/80 (New)
	currentTime = currentTime.Add(40 * time.Second)
	mockEx.OrderBook = &domain.OrderBook{
		Bids: []domain.OrderBookEntry{{Price: 100, Size: 40}},
		Asks: []domain.OrderBookEntry{{Price: 101, Size: 80}},
	}

	stats, err = service.GetMarketStats(ctx, symbol)
	if err != nil {
		t.Fatalf("GetMarketStats failed: %v", err)
	}

	// Avg Bid: (30+40)/2 = 35
	// Avg Ask: (60+80)/2 = 70
	if stats.DepthBid != 35 || stats.DepthAsk != 70 {
		t.Errorf("Expected Avg Depth 35/70, got %.2f/%.2f", stats.DepthBid, stats.DepthAsk)
	}
}
