package domain

import "time"

type Side string

const (
	SideLong  Side = "LONG"
	SideShort Side = "SHORT"
)

// Position represents an open position on the exchange.
type Position struct {
	Exchange      string
	Symbol        string
	Side          Side
	Size          float64
	EntryPrice    float64
	CurrentPrice  float64
	UnrealizedPnL float64
	Leverage      int
	MarginType    string
}

// Order represents a trade executed by the bot.
type Order struct {
	ID          string // Exchange Order ID or internal ID
	Exchange    string
	Symbol      string
	LevelID     string
	Side        Side
	Size        float64
	Price       float64
	RealizedPnL float64
	CreatedAt   time.Time
}

// PositionHistory represents a closed position.
type PositionHistory struct {
	ID          int64
	Exchange    string
	Symbol      string
	Side        Side
	Size        float64
	EntryPrice  float64
	ExitPrice   float64
	RealizedPnL float64
	Leverage    int
	MarginType  string
	ClosedAt    time.Time
}
