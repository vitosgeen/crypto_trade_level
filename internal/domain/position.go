package domain

import "time"

type Side string

const (
	SideLong  Side = "LONG"
	SideShort Side = "SHORT"
	SideBoth  Side = "BOTH"
)

// Position represents an open position on the exchange.
type Position struct {
	Exchange          string
	Symbol            string
	Side              Side
	Size              float64
	EntryPrice        float64
	MarkPrice         float64
	CurrentPrice      float64 // Legacy support, maps to EvalPrice or MarkPrice
	EvalPrice         float64 // The price used for bot evaluation (Last/Mid)
	RSI               float64
	UnrealizedPnL     float64
	Leverage          int
	MarginType        string
	LevelID           string  // Linked level ID
	LevelPrice        float64 // Linked level price for display
	LiquidityRatio    float64
	LiquidityClusters int // Legacy/Weak side
	BidClusters       int
	AskClusters       int
	LiquidityWallPct  float64
	GLI               float64
	DominantSide      string // "bid" or "ask"
	Volume60s         float64
	DepthBid          float64
	DepthAsk          float64
	PriceChange60s    float64
	OBI               float64
	MACD              float64
	MACDSignal        float64
	MACDHist          float64
	TSI               float64
}

// Order represents a trade executed by the bot.
type Order struct {
	ID           string // Internal ID
	OrderID      string // Exchange Order ID (Bybit order ID)
	Exchange     string
	Symbol       string
	LevelID      string
	Side         Side
	Type         string // "Market" or "Limit"
	Size         float64
	Price        float64
	Status       string // "New", "PartiallyFilled", "Filled", "Cancelled", "Rejected"
	TimeInForce  string // "GoodTillCancel", "ImmediateOrCancel", "FillOrKill", "PostOnly"
	ReduceOnly   bool   // If true, order can only reduce position
	StopLoss     float64
	TakeProfit   float64
	TriggerPrice float64
	RealizedPnL  float64
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// PositionHistory represents a closed position.
type PositionHistory struct {
	ID           int64
	Exchange     string
	Symbol       string
	Side         Side
	Size         float64
	EntryPrice   float64
	ExitPrice    float64
	RealizedPnL  float64
	Leverage     int
	MarginType   string
	LevelID      string
	AnalysisJSON string
	Source       string
	OpenedAt     time.Time
	ClosedAt     time.Time
}

type PositionPnLHistory struct {
	ID               int64     `json:"id"`
	Symbol           string    `json:"symbol"`
	Side             Side      `json:"side"`
	Size             float64   `json:"size"`
	EntryPrice       float64   `json:"entry_price"`
	MarkPrice        float64   `json:"mark_price"`
	UnrealizedPnL    float64   `json:"unrealized_pnl"`
	RSI              float64   `json:"rsi"`
	LiquidityRatio   float64   `json:"liquidity_ratio"`
	BidClusters      int       `json:"bid_clusters"`
	AskClusters      int       `json:"ask_clusters"`
	LiquidityWallPct float64   `json:"liquidity_wall_pct"`
	GLI              float64   `json:"gli"`
	Volume60s        float64   `json:"volume_60s"`
	DepthBid         float64   `json:"depth_bid"`
	DepthAsk         float64   `json:"depth_ask"`
	PriceChange60s   float64   `json:"price_change_60s"`
	OBI              float64   `json:"obi"`
	MACD             float64   `json:"macd"`
	MACDSignal       float64   `json:"macd_signal"`
	MACDHist         float64   `json:"macd_hist"`
	TSI              float64   `json:"tsi"`
	Timestamp        time.Time `json:"timestamp"`
}

type TickData struct {
	Timestamp     int64            `json:"ts"`
	Price         float64          `json:"p"`
	RSI           float64          `json:"rsi"`
	Volume        float64          `json:"v"`
	TradeVelocity float64          `json:"vel"`
	Bids          []OrderBookEntry `json:"bids"`
	Asks          []OrderBookEntry `json:"asks"`
}

type TradeSessionLog struct {
	ID        string     `json:"id"` // symbol_ts_start-ts_finish
	Symbol    string     `json:"symbol"`
	StartTime int64      `json:"start_time"`
	EndTime   int64      `json:"end_time"`
	Ticks     []TickData `json:"ticks"`
}
