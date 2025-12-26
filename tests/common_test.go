package tests

import (
	"context"

	"github.com/vitos/crypto_trade_level/internal/domain"
)

type MockExchange struct {
	Price      float64
	BuyCalled  bool
	SellCalled bool
	Position   *domain.Position
	OrderBook  *domain.OrderBook
}

func (m *MockExchange) SetPosition(symbol string, side domain.Side, size, entryPrice float64) {
	m.Position = &domain.Position{
		Symbol:     symbol,
		Side:       side,
		Size:       size,
		EntryPrice: entryPrice,
	}
}

func (m *MockExchange) GetCurrentPrice(ctx context.Context, symbol string) (float64, error) {
	return m.Price, nil
}

func (m *MockExchange) MarketBuy(ctx context.Context, symbol string, size float64, leverage int, marginType string, stopLoss float64) error {
	m.BuyCalled = true
	if m.Position == nil {
		m.Position = &domain.Position{
			Symbol:     symbol,
			Side:       domain.SideLong,
			Size:       size,
			EntryPrice: m.Price,
		}
	} else {
		m.Position.EntryPrice = (m.Position.EntryPrice*m.Position.Size + m.Price*size) / (m.Position.Size + size)
		m.Position.Size += size
	}
	return nil
}

func (m *MockExchange) MarketSell(ctx context.Context, symbol string, size float64, leverage int, marginType string, stopLoss float64) error {
	m.SellCalled = true
	if m.Position == nil {
		m.Position = &domain.Position{
			Symbol:     symbol,
			Side:       domain.SideShort,
			Size:       size,
			EntryPrice: m.Price,
		}
	} else {
		m.Position.EntryPrice = (m.Position.EntryPrice*m.Position.Size + m.Price*size) / (m.Position.Size + size)
		m.Position.Size += size
	}
	return nil
}

func (m *MockExchange) ClosePosition(ctx context.Context, symbol string) error {
	m.Position = nil
	return nil
}

func (m *MockExchange) GetPosition(ctx context.Context, symbol string) (*domain.Position, error) {
	if m.Position != nil && m.Position.Symbol == symbol {
		return m.Position, nil
	}
	return &domain.Position{Symbol: symbol, Size: 0}, nil
}

func (m *MockExchange) GetPositions(ctx context.Context) ([]*domain.Position, error) {
	if m.Position != nil && m.Position.Size > 0 {
		return []*domain.Position{m.Position}, nil
	}
	return []*domain.Position{}, nil
}

func (m *MockExchange) GetCandles(ctx context.Context, symbol, interval string, limit int) ([]domain.Candle, error) {
	return nil, nil
}

func (m *MockExchange) GetOrderBook(ctx context.Context, symbol string, category string) (*domain.OrderBook, error) {
	if m.OrderBook != nil {
		return m.OrderBook, nil
	}
	return &domain.OrderBook{Symbol: symbol}, nil
}

func (m *MockExchange) GetTickers(ctx context.Context, category string) ([]domain.Ticker, error) {
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

func (m *MockExchange) PlaceOrder(ctx context.Context, order *domain.Order) (*domain.Order, error) {
	return order, nil
}

func (m *MockExchange) GetOrder(ctx context.Context, symbol, orderID string) (*domain.Order, error) {
	return &domain.Order{OrderID: orderID, Symbol: symbol, Status: "Filled"}, nil
}

func (m *MockExchange) CancelOrder(ctx context.Context, symbol, orderID string) error {
	return nil
}
