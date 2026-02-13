package domain

import "time"

// Level represents a price level to defend.
type Level struct {
	ID                       string
	Exchange                 string
	Symbol                   string
	LevelPrice               float64
	Side                     Side
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
	IgnoreSentimentFilter    bool    // Skip sentiment checks for entry
	Tier1Pct                 float64 // Tier 1 percentage override
	Tier2Pct                 float64 // Tier 2 percentage override
	Tier3Pct                 float64 // Tier 3 percentage override
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

type LiquidityBucket struct {
	Price  float64 `json:"price"`
	Volume float64 `json:"volume"`
}

type LiquiditySnapshot struct {
	Symbol string            `json:"symbol"`
	Time   int64             `json:"time"`
	Bids   []LiquidityBucket `json:"bids"`
	Asks   []LiquidityBucket `json:"asks"`
}

// RSIMonitorConfig defines settings for automated RSI-based level creation.
type RSIMonitorConfig struct {
	Enabled             bool     `json:"enabled"`
	ScanIntervalMinutes int      `json:"scan_interval_minutes"`
	Timeframes          []string `json:"timeframes"`
	Period              int      `json:"period"`
	Overbought          float64  `json:"overbought"`
	Oversold            float64  `json:"oversold"`
	SizeUSDT            float64  `json:"size_usdt"`
	Leverage            int      `json:"leverage"`
	TakeProfitPct       float64  `json:"take_profit_pct"`
	TrendBreakEnabled   bool     `json:"trend_break_enabled"`
	TrendBreakWindow    int      `json:"trend_break_window"` // window size for pivot optimization
}
