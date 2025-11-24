package domain

import "time"

// Level represents a price level to defend.
type Level struct {
	ID             string
	Exchange       string
	Symbol         string
	LevelPrice     float64
	BaseSize       float64
	Leverage       int
	MarginType     string // "isolated" or "cross"
	CoolDownMs     int64
	StopLossAtBase bool
	StopLossMode   string // "exchange" or "app"
	Source         string
	CreatedAt      time.Time
}

// SymbolTiers defines the scaling tiers for a specific symbol on an exchange.
type SymbolTiers struct {
	Exchange  string
	Symbol    string
	Tier1Pct  float64
	Tier2Pct  float64
	Tier3Pct  float64
	UpdatedAt time.Time
}
