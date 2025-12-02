package domain

import "time"

// Level represents a price level to defend.
type Level struct {
	ID                       string
	Exchange                 string
	Symbol                   string
	LevelPrice               float64
	BaseSize                 float64
	Leverage                 int
	MarginType               string // "isolated" or "cross"
	CoolDownMs               int64
	StopLossAtBase           bool
	StopLossMode             string  // "exchange" or "app"
	DisableSpeedClose        bool    // Disable sentiment/speed-based position closing
	MaxConsecutiveBaseCloses int     // Max number of consecutive base closes before cooldown
	BaseCloseCooldownMs      int64   // Cooldown duration in milliseconds after max base closes
	TakeProfitPct            float64 // Take profit percentage (e.g. 0.02 for 2%)
	TakeProfitMode           string  // "fixed" or "liquidity"
	IsAuto                   bool    // Created automatically by the system
	AutoModeEnabled          bool    // Enable auto-recreation on failure
	Source                   string
	CreatedAt                time.Time
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
