package usecase

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/vitos/crypto_trade_level/internal/domain"
	"go.uber.org/zap"
)

type RSIMonitorService struct {
	marketService *MarketService
	levelService  *LevelService
	logger        *zap.Logger
	config        domain.RSIMonitorConfig
	stopChan      chan struct{}
	mu            sync.Mutex
	running       bool

	// Trigger states to prevent double alerts
	// key: symbol:interval
	lastRSI map[string]float64
	signals []RSISignal
}

type RSISignal struct {
	Symbol    string    `json:"symbol"`
	Interval  string    `json:"interval"`
	RSI       float64   `json:"rsi"`
	Type      string    `json:"type"` // "OB", "OS", "BullishBreak", "BearishBreak"
	Timestamp time.Time `json:"timestamp"`
}

func NewRSIMonitorService(ms *MarketService, ls *LevelService, logger *zap.Logger, config domain.RSIMonitorConfig) *RSIMonitorService {
	return &RSIMonitorService{
		marketService: ms,
		levelService:  ls,
		logger:        logger,
		config:        config,
		stopChan:      make(chan struct{}),
		lastRSI:       make(map[string]float64),
	}
}

func (s *RSIMonitorService) Start(ctx context.Context) {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.mu.Unlock()

	s.logger.Info("RSI Monitor Service started",
		zap.Int("scan_interval_minutes", s.config.ScanIntervalMinutes),
		zap.Strings("timeframes", s.config.Timeframes))

	// Use a ticker for periodic scanning
	scanInterval := time.Duration(s.config.ScanIntervalMinutes) * time.Minute
	if scanInterval <= 0 {
		scanInterval = 1 * time.Minute
	}
	ticker := time.NewTicker(scanInterval)
	defer ticker.Stop()

	// Initial scan
	if s.config.Enabled {
		s.scanAll(ctx)
	}

	for {
		select {
		case <-ticker.C:
			if s.config.Enabled {
				s.scanAll(ctx)
			}
		case <-s.stopChan:
			return
		case <-ctx.Done():
			return
		}
	}
}

func (s *RSIMonitorService) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return
	}
	close(s.stopChan)
	s.running = false
}

func (s *RSIMonitorService) UpdateConfig(config domain.RSIMonitorConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config = config
}

func (s *RSIMonitorService) GetConfig() domain.RSIMonitorConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.config
}

func (s *RSIMonitorService) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

func (s *RSIMonitorService) scanAll(ctx context.Context) {
	// 1. Get all symbols from level service (which fetches from exchange)
	symbols, err := s.levelService.GetAllSymbols(ctx)
	if err != nil {
		s.logger.Error("Failed to get symbols for RSI scan", zap.Error(err))
		return
	}

	s.logger.Info("Starting RSI scan", zap.Int("symbols", len(symbols)))

	for _, symbol := range symbols {
		for _, tf := range s.config.Timeframes {
			s.checkSymbol(ctx, symbol, tf)
		}
	}
}

func (s *RSIMonitorService) checkSymbol(ctx context.Context, symbol, interval string) {
	period := s.config.Period
	if period <= 0 {
		period = 14
	}

	rsi, err := s.marketService.GetRSI(ctx, symbol, interval, period)
	if err != nil {
		// Log error occasionally but maybe not every time to avoid spam
		return
	}

	key := fmt.Sprintf("%s:%s", symbol, interval)
	s.mu.Lock()
	prevRSI, ok := s.lastRSI[key]
	s.lastRSI[key] = rsi
	s.mu.Unlock()

	if !ok {
		// First point recorded, need one more to detect cross
		return
	}

	// RSI Crosses Below Oversold Threshold (Oversold -> Potential Long)
	if prevRSI >= s.config.Oversold && rsi < s.config.Oversold {
		s.logger.Info("🚨 RSI Oversold Trigger",
			zap.String("symbol", symbol),
			zap.String("tf", interval),
			zap.Float64("rsi", rsi),
			zap.Float64("prev_rsi", prevRSI))
		s.createLevel(ctx, symbol, domain.SideLong, fmt.Sprintf("RSI Oversold (%s)", interval))
		s.recordSignal(symbol, interval, rsi, "OS")
	}

	// RSI Crosses Above Overbought Threshold (Overbought -> Potential Short)
	if prevRSI <= s.config.Overbought && rsi > s.config.Overbought {
		s.logger.Info("🚨 RSI Overbought Trigger",
			zap.String("symbol", symbol),
			zap.String("tf", interval),
			zap.Float64("rsi", rsi),
			zap.Float64("prev_rsi", prevRSI))
		s.createLevel(ctx, symbol, domain.SideShort, fmt.Sprintf("RSI Overbought (%s)", interval))
		s.recordSignal(symbol, interval, rsi, "OB")
	}

	// Trend Break Detection
	if s.config.TrendBreakEnabled {
		s.checkTrendBreak(ctx, symbol, interval, period)
	}
}

type RSIPoint struct {
	Index int
	Value float64
}

func (s *RSIMonitorService) findPivots(rsi []float64, window int) (highs []RSIPoint, lows []RSIPoint) {
	if len(rsi) < 2*window+1 {
		return
	}

	for i := window; i < len(rsi)-window; i++ {
		isHigh := true
		isLow := true
		for w := 1; w <= window; w++ {
			if rsi[i] < rsi[i-w] || rsi[i] < rsi[i+w] {
				isHigh = false
			}
			if rsi[i] > rsi[i-w] || rsi[i] > rsi[i+w] {
				isLow = false
			}
		}
		if isHigh {
			highs = append(highs, RSIPoint{Index: i, Value: rsi[i]})
		}
		if isLow {
			lows = append(lows, RSIPoint{Index: i, Value: rsi[i]})
		}
	}
	return
}

func (s *RSIMonitorService) checkTrendBreak(ctx context.Context, symbol, interval string, period int) {
	window := s.config.TrendBreakWindow
	if window < 2 {
		window = 5
	}
	// Fetch enough history for pivots. To get 2 pivots with window 5, we might need ~50 candles.
	rsiHistory, err := s.marketService.GetRSIHistory(ctx, symbol, interval, period, 100)
	if err != nil || len(rsiHistory) < 20 {
		return
	}

	highs, lows := s.findPivots(rsiHistory, window)
	currentRSI := rsiHistory[len(rsiHistory)-1]

	// Check Bullish Break (Breaking a Downward Trendline of Highs)
	if len(highs) >= 2 {
		p1 := highs[len(highs)-2]
		p2 := highs[len(highs)-1]

		// Downward trendline: p2.Value <= p1.Value
		if p2.Value <= p1.Value {
			slope := (p2.Value - p1.Value) / float64(p2.Index-p1.Index)
			// Expected value at current index
			expected := p2.Value + slope*float64((len(rsiHistory)-1)-p2.Index)

			// Break: currentRSI > expected
			if currentRSI > expected && currentRSI < 70 { // Don't buy if already overbought
				// Also check if we just crossed (prev rsi was below)
				prevExpected := p2.Value + slope*float64((len(rsiHistory)-2)-p2.Index)
				prevRSI := rsiHistory[len(rsiHistory)-2]

				if prevRSI <= prevExpected && currentRSI > expected {
					s.logger.Info("🔥 RSI Trend Break (Bullish)",
						zap.String("symbol", symbol),
						zap.Float64("rsi", currentRSI),
						zap.Float64("expected", expected))
					s.createLevel(ctx, symbol, domain.SideLong, fmt.Sprintf("Trend Break Bullish (%s)", interval))
					s.recordSignal(symbol, interval, currentRSI, "BullishBreak")
				}
			}
		}
	}

	// Check Bearish Break (Breaking an Upward Trendline of Lows)
	if len(lows) >= 2 {
		p1 := lows[len(lows)-2]
		p2 := lows[len(lows)-1]

		// Upward trendline: p2.Value >= p1.Value
		if p2.Value >= p1.Value {
			slope := (p2.Value - p1.Value) / float64(p2.Index-p1.Index)
			expected := p2.Value + slope*float64((len(rsiHistory)-1)-p2.Index)

			if currentRSI < expected && currentRSI > 30 { // Don't sell if already oversold
				prevExpected := p2.Value + slope*float64((len(rsiHistory)-2)-p2.Index)
				prevRSI := rsiHistory[len(rsiHistory)-2]

				if prevRSI >= prevExpected && currentRSI < expected {
					s.logger.Info("📉 RSI Trend Break (Bearish)",
						zap.String("symbol", symbol),
						zap.Float64("rsi", currentRSI),
						zap.Float64("expected", expected))
					s.createLevel(ctx, symbol, domain.SideShort, fmt.Sprintf("Trend Break Bearish (%s)", interval))
					s.recordSignal(symbol, interval, currentRSI, "BearishBreak")
				}
			}
		}
	}
}

func (s *RSIMonitorService) recordSignal(symbol, interval string, rsi float64, sigType string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.signals = append(s.signals, RSISignal{
		Symbol:    symbol,
		Interval:  interval,
		RSI:       rsi,
		Type:      sigType,
		Timestamp: time.Now(),
	})

	// Keep last 100 signals
	if len(s.signals) > 100 {
		s.signals = s.signals[len(s.signals)-100:]
	}
}

func (s *RSIMonitorService) GetSignals() []RSISignal {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Return a copy
	res := make([]RSISignal, len(s.signals))
	copy(res, s.signals)
	return res
}

func (s *RSIMonitorService) createLevel(ctx context.Context, symbol string, side domain.Side, source string) {
	// Get latest price
	price := s.levelService.GetLatestPrice(symbol)
	if price == 0 {
		s.logger.Warn("Skipping level creation: Price not available", zap.String("symbol", symbol))
		return
	}

	// Check if we already have an active level for this symbol to avoid over-trading
	activeCount, err := s.levelService.levelRepo.CountActiveLevels(ctx, symbol)
	if err == nil && activeCount > 0 {
		s.logger.Info("Skipping RSI level creation: Level already exists", zap.String("symbol", symbol))
		return
	}

	// Fetch current tiers for this symbol to use as basis
	tiers, _ := s.levelService.levelRepo.GetSymbolTiers(ctx, "bybit", symbol)
	t1, t2, t3 := 0.005, 0.003, 0.0015 // Defaults
	if tiers != nil {
		t1, t2, t3 = tiers.Tier1Pct, tiers.Tier2Pct, tiers.Tier3Pct
	}

	// Parameters for auto-created rsi levels
	level := &domain.Level{
		ID:              fmt.Sprintf("rsi_%d", time.Now().UnixNano()),
		Exchange:        "bybit",
		Symbol:          symbol,
		LevelPrice:      price,
		Side:            side,
		BaseSize:        s.config.SizeUSDT / price,
		Leverage:        s.config.Leverage,
		MarginType:      "cross",
		CoolDownMs:      600 * 1000, // 10 min
		IsAuto:          true,
		AutoModeEnabled: true,
		Source:          source,
		CreatedAt:       time.Now(),
		TakeProfitPct:   s.config.TakeProfitPct,
		TakeProfitMode:  "fixed",
		StopLossAtBase:  true,
		StopLossMode:    "exchange",
		Tier1Pct:        t1,
		Tier2Pct:        t2,
		Tier3Pct:        t3,
	}

	// Round base size to something reasonable?
	// For BTC 100/50000 = 0.002.
	// We should probably use the same rounding logic as in handles.go or just keep it raw if exchange supports it.
	// But let's keep it simple for now.

	err = s.levelService.CreateLevel(ctx, level)
	if err != nil {
		s.logger.Error("Failed to create RSI level", zap.Error(err))
	} else {
		s.logger.Info("Successfully created RSI level",
			zap.String("symbol", symbol),
			zap.Float64("price", price),
			zap.String("side", string(side)),
			zap.String("source", source))
	}
}
