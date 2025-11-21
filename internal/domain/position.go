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
	ID         string // Exchange Order ID or internal ID
	Exchange   string
	Symbol     string
	LevelID    string
	Side       Side
	Size       float64
	Price      float64
	CreatedAt  time.Time
}
