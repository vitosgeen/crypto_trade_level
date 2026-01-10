package domain

import "context"

// Exchange defines the interface for interacting with a crypto exchange.
type Exchange interface {
	GetCurrentPrice(ctx context.Context, symbol string) (float64, error)
	MarketBuy(ctx context.Context, symbol string, size float64, leverage int, marginType string, stopLoss float64) error
	MarketSell(ctx context.Context, symbol string, size float64, leverage int, marginType string, stopLoss float64) error
	ClosePosition(ctx context.Context, symbol string) error
	GetPosition(ctx context.Context, symbol string) (*Position, error)
	GetPositions(ctx context.Context) ([]*Position, error)
	GetCandles(ctx context.Context, symbol, interval string, limit int) ([]Candle, error)
	GetOrderBook(ctx context.Context, symbol string, category string) (*OrderBook, error)
	GetRecentTrades(ctx context.Context, symbol string, limit int) ([]PublicTrade, error)
	GetInstruments(ctx context.Context, category string) ([]Instrument, error)
	GetTickers(ctx context.Context, category string) ([]Ticker, error)
	OnTradeUpdate(callback func(symbol string, side string, size float64, price float64))
	Subscribe(symbols []string) error

	// Order management for funding bot
	PlaceOrder(ctx context.Context, order *Order) (*Order, error)
	GetOrder(ctx context.Context, symbol, orderID string) (*Order, error)
	CancelOrder(ctx context.Context, symbol, orderID string) error
}

type Candle struct {
	Time   int64   `json:"time"`
	Open   float64 `json:"open"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Close  float64 `json:"close"`
	Volume float64 `json:"volume"`
}

type OrderBookEntry struct {
	Price float64 `json:"price"`
	Size  float64 `json:"size"`
}

type OrderBook struct {
	Symbol string           `json:"symbol"`
	Bids   []OrderBookEntry `json:"bids"`
	Asks   []OrderBookEntry `json:"asks"`
}

type PublicTrade struct {
	Symbol string  `json:"symbol"`
	Side   string  `json:"side"`
	Size   float64 `json:"size"`
	Price  float64 `json:"price"`
	Time   int64   `json:"time"`
}

// LevelRepository defines storage operations for levels and tiers.
type LevelRepository interface {
	SaveLevel(ctx context.Context, level *Level) error
	GetLevel(ctx context.Context, id string) (*Level, error)
	ListLevels(ctx context.Context) ([]*Level, error)
	GetLevelsBySymbol(ctx context.Context, symbol string) ([]*Level, error)
	DeleteLevel(ctx context.Context, id string) error
	CountActiveLevels(ctx context.Context, symbol string) (int, error)

	SaveSymbolTiers(ctx context.Context, tiers *SymbolTiers) error
	GetSymbolTiers(ctx context.Context, exchange, symbol string) (*SymbolTiers, error)

	SaveLiquiditySnapshot(ctx context.Context, snap *LiquiditySnapshot) error
	ListLiquiditySnapshots(ctx context.Context, symbol string, limit int) ([]*LiquiditySnapshot, error)
}

// TradeRepository defines storage operations for trades.
type TradeRepository interface {
	SaveTrade(ctx context.Context, order *Order) error
	ListTrades(ctx context.Context, limit int) ([]*Order, error)

	SavePositionHistory(ctx context.Context, history *PositionHistory) error
	ListPositionHistory(ctx context.Context, limit int) ([]*PositionHistory, error)
	SaveTradeSessionLog(ctx context.Context, log *TradeSessionLog) error
}
