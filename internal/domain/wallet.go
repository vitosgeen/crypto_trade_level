package domain

import "time"

type WalletBalance struct {
	ID                int64     `json:"id" db:"id"`
	Coin              string    `json:"coin" db:"coin"`
	Total             float64   `json:"total" db:"total"`
	Free              float64   `json:"free" db:"free"`
	UnrealizedPnL     float64   `json:"unrealized_pnl" db:"unrealized_pnl"`
	Equity            float64   `json:"equity" db:"equity"`
	MarginBalance     float64   `json:"margin_balance" db:"margin_balance"`
	InitialMargin     float64   `json:"initial_margin" db:"initial_margin"`
	MaintenanceMargin float64   `json:"maintenance_margin" db:"maintenance_margin"`
	Timestamp         time.Time `json:"timestamp" db:"timestamp"`
}
