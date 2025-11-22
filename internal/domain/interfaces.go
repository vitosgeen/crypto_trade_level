package domain

import "context"

// Exchange defines the interface for interacting with a crypto exchange.
type Exchange interface {
	GetCurrentPrice(ctx context.Context, symbol string) (float64, error)
	MarketBuy(ctx context.Context, symbol string, size float64, leverage int, marginType string, stopLoss float64) error
	MarketSell(ctx context.Context, symbol string, size float64, leverage int, marginType string, stopLoss float64) error
	ClosePosition(ctx context.Context, symbol string) error
	GetPosition(ctx context.Context, symbol string) (*Position, error)
}

// LevelRepository defines storage operations for levels and tiers.
type LevelRepository interface {
	SaveLevel(ctx context.Context, level *Level) error
	GetLevel(ctx context.Context, id string) (*Level, error)
	ListLevels(ctx context.Context) ([]*Level, error)
	DeleteLevel(ctx context.Context, id string) error

	SaveSymbolTiers(ctx context.Context, tiers *SymbolTiers) error
	GetSymbolTiers(ctx context.Context, exchange, symbol string) (*SymbolTiers, error)
}

// TradeRepository defines storage operations for trades.
type TradeRepository interface {
	SaveTrade(ctx context.Context, order *Order) error
	ListTrades(ctx context.Context, limit int) ([]*Order, error)
}
