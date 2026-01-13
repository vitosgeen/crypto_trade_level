package usecase

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/vitos/crypto_trade_level/internal/domain"
	"go.uber.org/zap"
)

type FundingBotConfig struct {
	Symbol                  string        `json:"symbol"`
	PositionSize            float64       `json:"position_size"`
	Leverage                int           `json:"leverage"`
	MarginType              string        `json:"margin_type"`
	CountdownThreshold      time.Duration `json:"countdown_threshold"`       // Seconds before funding event
	MinFundingRate          float64       `json:"min_funding_rate"`          // Minimum funding rate to trade
	WallCheckEnabled        bool          `json:"wall_check_enabled"`        // Enable wall detection
	WallThresholdMultiplier float64       `json:"wall_threshold_multiplier"` // Multiplier for average volume to define a wall (default 2.0)
	StopLossPercentage      float64       `json:"stop_loss_percentage"`      // SL as percentage (e.g. 0.01 for 1%)
	TakeProfitPercentage    float64       `json:"take_profit_percentage"`    // TP as percentage (e.g. 0.01 for 1%)
}

type FundingBotService struct {
	exchange          domain.Exchange
	tradeRepo         domain.TradeRepository
	marketService     *MarketService
	bots              map[string]*FundingBot
	logger            *zap.Logger
	mu                sync.Mutex
	autoScannerCtx    context.Context
	autoScannerCancel context.CancelFunc
}

type FundingBot struct {
	config              FundingBotConfig
	exchange            domain.Exchange
	marketService       *MarketService
	logger              *zap.Logger
	running             bool
	stopChan            chan struct{}
	cancel              context.CancelFunc
	currentOrder        *domain.Order
	lastNextFundingTime int64
	expectedFundingRate float64
	fundingEventTime    time.Time
	tpOrder             *domain.Order
	needsNextCandleLog  bool
	initialLogMinute    int
	monitoringActive    bool
	sessionTicks        []domain.TickData
	sessionStartTime    int64
	tradeRepo           domain.TradeRepository
	mu                  sync.Mutex
}

type FundingBotStatus struct {
	Running         bool             `json:"running"`
	Position        *domain.Position `json:"position,omitempty"`
	Signal          string           `json:"signal"`
	CurrentOrder    *domain.Order    `json:"current_order,omitempty"`
	NextFundingTime int64            `json:"next_funding_time"`
	Countdown       int64            `json:"countdown"`    // Seconds until funding
	FundingRate     float64          `json:"funding_rate"` // Current funding rate
}

func NewFundingBotService(exchange domain.Exchange, tradeRepo domain.TradeRepository, marketService *MarketService, logger *zap.Logger) *FundingBotService {
	return &FundingBotService{
		exchange:      exchange,
		tradeRepo:     tradeRepo,
		marketService: marketService,
		bots:          make(map[string]*FundingBot),
		logger:        logger,
	}
}

func (s *FundingBotService) StartBot(ctx context.Context, config FundingBotConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if bot already running for this symbol
	if bot, exists := s.bots[config.Symbol]; exists && bot.running {
		return fmt.Errorf("funding bot already running for %s", config.Symbol)
	}

	// Create new bot
	bot := &FundingBot{
		config:        config,
		exchange:      s.exchange,
		marketService: s.marketService,
		tradeRepo:     s.tradeRepo,
		logger:        s.logger,
		running:       true,
		stopChan:      make(chan struct{}),
	}

	s.bots[config.Symbol] = bot

	// Start bot loop with background context
	botCtx, cancel := context.WithCancel(context.Background())
	bot.cancel = cancel
	go bot.run(botCtx)

	s.logger.Info("Funding bot started", zap.String("symbol", config.Symbol))
	return nil
}

func (s *FundingBotService) StopBot(symbol string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	bot, exists := s.bots[symbol]
	if !exists || !bot.running {
		return fmt.Errorf("no running funding bot found for %s", symbol)
	}

	bot.stop()
	delete(s.bots, symbol)

	s.logger.Info("Funding bot stopped", zap.String("symbol", symbol))
	return nil
}

func (s *FundingBotService) StartAutoScanner(ctx context.Context) {
	s.mu.Lock()
	if s.autoScannerCancel != nil {
		s.mu.Unlock()
		return
	}
	autoCtx, cancel := context.WithCancel(ctx)
	s.autoScannerCtx = autoCtx
	s.autoScannerCancel = cancel
	s.mu.Unlock()

	s.logger.Info("Starting Funding Bot Auto-Scanner")
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	// Initial check
	if err := s.checkAutoBots(autoCtx); err != nil {
		s.logger.Error("Initial auto-scanner check failed", zap.Error(err))
	}

	for {
		select {
		case <-autoCtx.Done():
			return
		case <-ticker.C:
			if err := s.checkAutoBots(autoCtx); err != nil {
				s.logger.Error("Auto-scanner check failed", zap.Error(err))
			}
		}
	}
}

func (s *FundingBotService) IsBotRunning(symbol string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	bot, exists := s.bots[symbol]
	return exists && bot.running
}

func (s *FundingBotService) IsAutoScannerRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.autoScannerCancel != nil
}

func (s *FundingBotService) StopAutoScanner() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.autoScannerCancel != nil {
		s.autoScannerCancel()
		s.autoScannerCancel = nil
		s.logger.Info("Funding Bot Auto-Scanner stopped")
	}
}

func (s *FundingBotService) checkAutoBots(ctx context.Context) error {
	tickers, err := s.exchange.GetTickers(ctx, "linear")
	if err != nil {
		return err
	}

	for _, t := range tickers {
		// Only funding > 0.8% or < -0.8%
		if math.Abs(t.FundingRate) >= 0.008 {
			s.mu.Lock()
			bot, exists := s.bots[t.Symbol]
			s.mu.Unlock()

			if !exists || !bot.running {
				s.logger.Info("Auto-starting funding bot",
					zap.String("symbol", t.Symbol),
					zap.Float64("rate_pct", t.FundingRate*100))

				// Calculate position size to be ~$11 USD (min 10$ equivalent)
				posSize := 11.0 / t.LastPrice
				// Round to 4 decimal places to support expensive assets like BTC
				posSize = math.Round(posSize*10000) / 10000
				if posSize <= 0 {
					posSize = 0.0001 // Fallback to minimum possible
				}

				config := FundingBotConfig{
					Symbol:                  t.Symbol,
					PositionSize:            posSize,
					Leverage:                10,
					MarginType:              "isolated",
					CountdownThreshold:      5 * time.Second,
					MinFundingRate:          0.008,
					WallCheckEnabled:        true,
					WallThresholdMultiplier: 2.0,
					StopLossPercentage:      0.5,
					TakeProfitPercentage:    0.5,
				}

				// Start bot in background
				go func(cfg FundingBotConfig) {
					if err := s.StartBot(context.Background(), cfg); err != nil {
						s.logger.Error("Auto-start failed", zap.String("symbol", cfg.Symbol), zap.Error(err))
					}
				}(config)
			}
		} else if t.FundingRate < 0.007 {
			s.mu.Lock()
			bot, exists := s.bots[t.Symbol]
			s.mu.Unlock()

			if exists && bot.running {
				// We don't stop the bot automatically if it's already running,
				// but maybe we should if there's no position.
				// However, StartBot might have been manual.
				// Let's only stop it if it was started by auto-scanner?
				// We don't track who started it.
				// For now, let's NOT automatically stop bots to avoid surprising the user.
				// If they want to stop it, they can do it from the UI.
			}
		}
	}
	return nil
}

func (s *FundingBotService) GetBotStatus(ctx context.Context, symbol string) (*FundingBotStatus, error) {
	s.mu.Lock()
	bot, exists := s.bots[symbol]
	s.mu.Unlock()

	var status *FundingBotStatus
	if exists {
		var err error
		status, err = bot.getStatus(ctx)
		if err != nil {
			return nil, err
		}
	} else {
		status = &FundingBotStatus{Running: false}
	}

	// Helper to fetch funding info if not populated (e.g. bot not running)
	// or update it if we want latest info guaranteed
	if !exists {
		// Fetch tickers to get funding rate and time
		tickers, err := s.exchange.GetTickers(ctx, "linear")
		if err == nil {
			for _, t := range tickers {
				if t.Symbol == symbol {
					// Convert NextFundingTime from milliseconds to seconds if needed
					nextFundingTimeSec := t.NextFundingTime
					if nextFundingTimeSec > 1000000000000 { // Heuristic check: > year 2001 in ms
						nextFundingTimeSec = nextFundingTimeSec / 1000
					}
					status.NextFundingTime = nextFundingTimeSec

					status.FundingRate = t.FundingRate
					now := time.Now().Unix()
					status.Countdown = nextFundingTimeSec - now
					break
				}
			}
		}

		// Round countdown
		if status.Countdown < 0 {
			status.Countdown = 0
		}
	}

	return status, nil
}

func (b *FundingBot) run(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	b.logger.Info("Funding bot evaluation loop started", zap.String("symbol", b.config.Symbol))

	for {
		select {
		case <-ticker.C:
			if err := b.evaluate(ctx); err != nil {
				b.logger.Error("Funding bot evaluation error",
					zap.Error(err),
					zap.String("symbol", b.config.Symbol))
			}
		case <-b.stopChan:
			b.logger.Info("Funding bot evaluation loop stopped", zap.String("symbol", b.config.Symbol))
			return
		case <-ctx.Done():
			b.logger.Info("Funding bot evaluation loop cancelled", zap.String("symbol", b.config.Symbol))
			return
		}
	}
}

func (b *FundingBot) stop() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.running {
		b.running = false
		if b.cancel != nil {
			b.cancel()
		}
		close(b.stopChan)

		// Cancel any pending orders
		if b.currentOrder != nil {
			ctx := context.Background()
			if err := b.exchange.CancelOrder(ctx, b.config.Symbol, b.currentOrder.OrderID); err != nil {
				b.logger.Error("Failed to cancel order on stop",
					zap.Error(err),
					zap.String("order_id", b.currentOrder.OrderID))
			}
			b.currentOrder = nil
		}
	}
}

func (b *FundingBot) evaluate(ctx context.Context) error {
	b.mu.Lock()
	if !b.running {
		b.mu.Unlock()
		return nil
	}
	b.mu.Unlock()

	// Get funding data
	tickers, err := b.exchange.GetTickers(ctx, "linear")
	if err != nil {
		return fmt.Errorf("failed to get tickers: %w", err)
	}

	var ticker domain.Ticker
	found := false
	for _, t := range tickers {
		if t.Symbol == b.config.Symbol {
			ticker = t
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("symbol %s not found in tickers", b.config.Symbol)
	}

	// Calculate countdown
	now := time.Now().Unix()
	// Convert NextFundingTime from milliseconds to seconds if needed
	if ticker.NextFundingTime > 1000000000000 { // Heuristic check: > year 2001 in ms
		ticker.NextFundingTime = ticker.NextFundingTime / 1000
	}
	countdown := ticker.NextFundingTime - now

	// 1. ROLLOVER DETECTION
	if b.lastNextFundingTime > 0 && ticker.NextFundingTime > b.lastNextFundingTime {
		b.mu.Lock()
		b.fundingEventTime = time.Now()
		b.mu.Unlock()

		b.logger.Info("Funding rollover detected! Scheduling position closure in 10s.",
			zap.String("symbol", b.config.Symbol),
			zap.Int64("previous_funding_time", b.lastNextFundingTime),
			zap.Int64("new_funding_time", ticker.NextFundingTime),
			zap.Time("event_time", b.fundingEventTime))

		b.lastNextFundingTime = ticker.NextFundingTime
	}

	// Initialize tracking if needed
	if b.lastNextFundingTime == 0 {
		b.lastNextFundingTime = ticker.NextFundingTime
	}

	// 2. CHECK FOR DELAYED CLOSURE
	b.mu.Lock()
	hasEventTime := !b.fundingEventTime.IsZero()
	eventTime := b.fundingEventTime
	b.mu.Unlock()

	if hasEventTime {
		elapsed := time.Since(eventTime)
		if elapsed >= 10*time.Second {
			b.logger.Info("10 seconds passed since funding event. Closing position if exists.",
				zap.String("symbol", b.config.Symbol),
				zap.Duration("elapsed", elapsed))

			// Reset event time first to avoid re-triggering if closure takes time
			b.mu.Lock()
			b.fundingEventTime = time.Time{}
			tpOrder := b.tpOrder
			b.tpOrder = nil
			b.mu.Unlock()

			// Close position (checks if size > 0 internally)
			if err := b.closePosition(ctx); err != nil {
				b.logger.Error("Failed to close position after funding", zap.Error(err))
			}

			// Cancel TP order if it exists
			if tpOrder != nil {
				if err := b.exchange.CancelOrder(ctx, b.config.Symbol, tpOrder.OrderID); err != nil {
					b.logger.Warn("Failed to cancel TP order after closure", zap.Error(err), zap.String("order_id", tpOrder.OrderID))
				}
			}
		} else {
			// Still waiting for 10s
			b.logger.Debug("Waiting for 10s closure...",
				zap.String("symbol", b.config.Symbol),
				zap.Duration("remaining", 10*time.Second-elapsed))
		}
	}

	// 3. LOGGING NEXT CANDLE
	if b.needsNextCandleLog {
		currentMinute := time.Now().Minute()
		if currentMinute != b.initialLogMinute {
			b.logOrderBook(ctx, "Next Candle")
			b.needsNextCandleLog = false
		}
	}

	// 4. ACTIVE MONITORING LOGS
	if b.monitoringActive {
		b.logTradeTick(ctx, ticker)
	}

	// 5. ENTRY LOGIC CHECK
	b.logger.Info("Funding countdown  ",
		zap.Int64("countdown", countdown),
		zap.Float64("funding_rate_pct", ticker.FundingRate*100),
	)

	// Check if we are approaching funding time (must be in the future)
	thresholdSeconds := int64(b.config.CountdownThreshold.Seconds())
	if countdown > 0 && countdown <= thresholdSeconds {
		b.logger.Info("Funding countdown active (scanning)",
			zap.String("symbol", b.config.Symbol),
			zap.Int64("countdown_seconds", countdown),
			zap.Float64("current_rate_pct", ticker.FundingRate*100),
			zap.Float64("min_rate_pct", b.config.MinFundingRate*100))

		// Check funding rate
		fundingRateAbs := math.Abs(ticker.FundingRate)
		if fundingRateAbs < b.config.MinFundingRate {
			b.logger.Info("Skipping funding entry: Rate below threshold",
				zap.String("symbol", b.config.Symbol),
				zap.Float64("current_rate_pct", ticker.FundingRate*100),
				zap.Float64("min_rate_pct", b.config.MinFundingRate*100))
			return nil
		}

		// Trigger entry logic
		return b.handleFundingEvent(ctx)
	}

	if countdown < 0 {
		b.logger.Debug("Funding time passed, waiting for rollover",
			zap.String("symbol", b.config.Symbol),
			zap.Int64("countdown", countdown))
	}

	return nil
}

func (b *FundingBot) logOrderBook(ctx context.Context, label string) {
	orderBook, err := b.exchange.GetOrderBook(ctx, b.config.Symbol, "linear")
	if err != nil {
		b.logger.Error("Failed to get order book for logging", zap.Error(err), zap.String("label", label))
		return
	}

	if orderBook == nil {
		b.logger.Warn("Order book is nil for logging", zap.String("label", label))
		return
	}

	// Log top 10 bids and asks for better visibility
	topBids := orderBook.Bids
	if len(topBids) > 10 {
		topBids = topBids[:10]
	}
	topAsks := orderBook.Asks
	if len(topAsks) > 10 {
		topAsks = topAsks[:10]
	}

	b.logger.Info("Order Book Snapshot",
		zap.String("label", label),
		zap.String("symbol", b.config.Symbol),
		zap.Any("top_bids", topBids),
		zap.Any("top_asks", topAsks),
	)

	// Persist to liquidity_snapshots table for heatmap visibility
	if b.marketService != nil {
		go b.marketService.ForceRecordLiquiditySnapshot(context.Background(), b.config.Symbol)
	}
}

func (b *FundingBot) calculateLimitPrice(currentPrice, fundingRate float64) float64 {
	// Place order slightly above current price to ensure fill
	// but below the funding rate profit margin
	// Example: If funding is 0.01%, we can afford to buy at +0.005% premium
	premium := fundingRate * 0.5
	return currentPrice * (1 + premium)
}

func (b *FundingBot) IsWallStable(ctx context.Context, price float64, side string) bool {
	// Calculate current threshold
	orderBook, err := b.exchange.GetOrderBook(ctx, b.config.Symbol, "linear")
	if err != nil {
		return false
	}

	avgVolume := 0.0
	count := 0
	entries := orderBook.Asks
	if side == "bid" {
		entries = orderBook.Bids
	}

	for _, e := range entries {
		avgVolume += e.Size
		count++
	}

	if count == 0 {
		return false
	}

	avgVolume /= float64(count)
	multiplier := b.config.WallThresholdMultiplier
	if multiplier <= 0 {
		multiplier = 2.0
	}
	threshold := avgVolume * multiplier

	// Use 30s as stability duration
	return b.marketService.IsWallStable(b.config.Symbol, price, side, threshold, 30*time.Second)
}

func (b *FundingBot) placeShortOrder(ctx context.Context, ticker domain.Ticker) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Check if we already have an order
	if b.currentOrder != nil {
		return nil
	}

	limitPrice := b.calculateLimitPrice(ticker.LastPrice, ticker.FundingRate)
	b.expectedFundingRate = ticker.FundingRate

	order := &domain.Order{
		Symbol:      b.config.Symbol,
		Side:        domain.SideShort,
		Type:        "Limit",
		Size:        b.config.PositionSize,
		Price:       limitPrice,
		TimeInForce: "GoodTillCancel",
		ReduceOnly:  false,
		// SL/TP will be placed as separate orders after fill
	}

	placedOrder, err := b.exchange.PlaceOrder(ctx, order)
	if err != nil {
		return fmt.Errorf("failed to place order: %w", err)
	}

	b.currentOrder = placedOrder
	b.logger.Info("Placed funding arbitrage order (awaiting fill)",
		zap.String("symbol", b.config.Symbol),
		zap.String("order_id", placedOrder.OrderID),
		zap.Float64("price", limitPrice),
		zap.Float64("funding_rate_pct", ticker.FundingRate*100))

	return nil
}

func (b *FundingBot) monitorFundingEvent(ctx context.Context, fundingTime int64) error {
	now := time.Now().Unix()
	timeUntilFunding := fundingTime - now

	// If funding event hasn't occurred yet, wait
	if timeUntilFunding > 0 {
		return nil
	}

	// Funding event has occurred, check order status
	// 2. Persist to liquidity_snapshots table for heatmap visibility
	if b.marketService != nil {
		go b.marketService.ForceRecordLiquiditySnapshot(context.Background(), b.config.Symbol)
	}
	return b.handleFundingEvent(ctx)
}

func (b *FundingBot) handleFundingEvent(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Get latest position
	position, err := b.exchange.GetPosition(ctx, b.config.Symbol)
	if err != nil {
		return err
	}

	if position.Size > 0 {
		b.logger.Info("Position already exists, skipping funding entry",
			zap.String("symbol", b.config.Symbol),
			zap.Float64("size", position.Size))
		return nil
	}

	// Force a liquidity snapshot on entry attempt
	if b.marketService != nil {
		go b.marketService.ForceRecordLiquiditySnapshot(context.Background(), b.config.Symbol)
	}

	// Get latest ticker and funding rate
	tickers, err := b.exchange.GetTickers(ctx, "linear")
	if err != nil {
		return fmt.Errorf("failed to get tickers: %w", err)
	}

	var ticker domain.Ticker
	found := false
	for _, t := range tickers {
		if t.Symbol == b.config.Symbol {
			ticker = t
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("symbol %s not found in tickers", b.config.Symbol)
	}

	// Determine direction
	entrySide := domain.SideShort
	tpSide := domain.SideLong
	if ticker.FundingRate < 0 {
		entrySide = domain.SideLong
		tpSide = domain.SideShort
	}

	// 2. Check big wall (if exist nothing to do, just write to logs)
	// For Short entry, we check for Ask walls. For Long entry, we check for Bid walls.
	entryPrice := b.calculateLimitPrice(ticker.LastPrice, ticker.FundingRate)

	if b.config.WallCheckEnabled {
		orderBook, err := b.exchange.GetOrderBook(ctx, b.config.Symbol, "linear")
		if err != nil {
			return fmt.Errorf("failed to get order book: %w", err)
		}

		avgVolume := 0.0
		for _, a := range orderBook.Asks {
			avgVolume += a.Size
		}
		if len(orderBook.Asks) > 0 {
			avgVolume /= float64(len(orderBook.Asks))
		}

		multiplier := b.config.WallThresholdMultiplier
		if multiplier <= 0 {
			multiplier = 2.0
		}
		threshold := avgVolume * multiplier

		hasWall := false
		wallPrice := 0.0

		if entrySide == domain.SideShort {
			// Identify if any wall exists above current price but below or near our entry
			for _, ask := range orderBook.Asks {
				if ask.Price > ticker.LastPrice && ask.Price <= entryPrice*1.001 {
					if ask.Size > threshold {
						// Check stability
						if b.marketService.IsWallStable(b.config.Symbol, ask.Price, "ask", threshold, 30*time.Second) {
							hasWall = true
							wallPrice = ask.Price
							break
						}
					}
				}
			}
		} else {
			// Identify if any wall exists below current price but above or near our entry
			for _, bid := range orderBook.Bids {
				if bid.Price < ticker.LastPrice && bid.Price >= entryPrice*0.999 {
					if bid.Size > threshold {
						// Check stability
						if b.marketService.IsWallStable(b.config.Symbol, bid.Price, "bid", threshold, 30*time.Second) {
							hasWall = true
							wallPrice = bid.Price
							break
						}
					}
				}
			}
		}

		if hasWall {
			b.logger.Info("Stable big wall detected, skipping entry",
				zap.String("symbol", b.config.Symbol),
				zap.String("side", string(entrySide)),
				zap.Float64("wall_price", wallPrice),
				zap.Float64("entry_price", entryPrice),
				zap.Float64("current_price", ticker.LastPrice))
			return nil
		}
	}

	// 3. Open postion and limit order(funding rate + (0.5%)) at the same time
	b.logger.Info("Opening simultaneous funding arbitrage positions",
		zap.String("symbol", b.config.Symbol),
		zap.String("side", string(entrySide)),
		zap.Float64("entry_price", entryPrice),
		zap.Float64("current_price", ticker.LastPrice),
		zap.Float64("funding_rate_pct", ticker.FundingRate*100))

	// Entry Order (Limit order for "sniper" entry)
	entryOrder := &domain.Order{
		Symbol:      b.config.Symbol,
		Side:        entrySide,
		Type:        "Limit",
		Size:        b.config.PositionSize,
		Price:       entryPrice,
		TimeInForce: "GoodTillCancel",
		ReduceOnly:  false,
	}

	placedEntry, err := b.exchange.PlaceOrder(ctx, entryOrder)
	if err != nil {
		return fmt.Errorf("failed to place entry order: %w", err)
	}
	b.currentOrder = placedEntry
	b.logger.Info("Placed entry sniper limit order", zap.String("order_id", placedEntry.OrderID), zap.String("side", string(entrySide)))

	// Take Profit Order
	// Formula: entryPrice * (1 - (math.Abs(fundingRate) + 0.5%))
	// We always place the TP limit order BELOW the entry price
	// For SHORT, this is a profit on price. For LONG, this is a partial give-back of funding.
	tpDistance := math.Abs(ticker.FundingRate) + 0.005
	tpPrice := entryPrice * (1 - tpDistance)
	tpPrice = math.Round(tpPrice*10000) / 10000

	tpOrder := &domain.Order{
		Symbol:      b.config.Symbol,
		Side:        tpSide,
		Type:        "Limit",
		Size:        b.config.PositionSize,
		Price:       tpPrice,
		TimeInForce: "GoodTillCancel",
		ReduceOnly:  true,
	}

	placedTP, err := b.exchange.PlaceOrder(ctx, tpOrder)
	if err != nil {
		b.logger.Error("Failed to place limit order (TP)", zap.Error(err))
		return err
	}
	b.tpOrder = placedTP
	b.logger.Info("Placed limit order (TP)",
		zap.String("order_id", placedTP.OrderID),
		zap.String("side", string(tpSide)),
		zap.Float64("tp_price", tpPrice),
		zap.Float64("tp_distance_pct", tpDistance*100))

	// LOG ORDER BOOK Snapshot
	b.logOrderBook(ctx, "⏱️ Countdown Threshold")
	b.needsNextCandleLog = true
	b.initialLogMinute = time.Now().Minute()
	b.monitoringActive = true
	b.sessionStartTime = time.Now().Unix()
	b.sessionTicks = nil

	return nil
}

func (b *FundingBot) closePosition(ctx context.Context) error {
	// Get current position
	position, err := b.exchange.GetPosition(ctx, b.config.Symbol)
	if err != nil {
		return err
	}

	if position.Size == 0 {
		return nil // No position to close
	}

	// Close position using exchange's ClosePosition method
	if err := b.exchange.ClosePosition(ctx, b.config.Symbol); err != nil {
		return err
	}

	b.logger.Info("Position closed successfully",
		zap.String("symbol", b.config.Symbol))

	// Save session log
	if b.monitoringActive && len(b.sessionTicks) > 0 {
		endTime := time.Now().Unix()
		logID := fmt.Sprintf("%s_%d-%d", b.config.Symbol, b.sessionStartTime, endTime)
		sessionLog := &domain.TradeSessionLog{
			ID:        logID,
			Symbol:    b.config.Symbol,
			StartTime: b.sessionStartTime,
			EndTime:   endTime,
			Ticks:     b.sessionTicks,
		}

		go func(sl *domain.TradeSessionLog) {
			if b.tradeRepo != nil {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := b.tradeRepo.SaveTradeSessionLog(ctx, sl); err != nil {
					b.logger.Error("Failed to save trade session log", zap.Error(err))
				} else {
					b.logger.Info("Trade session log saved", zap.String("id", sl.ID))
				}
			}
		}(sessionLog)
	}

	b.monitoringActive = false
	b.sessionTicks = nil
	return nil
}

func (b *FundingBot) cancelOrder(ctx context.Context) error {
	if b.currentOrder == nil {
		return nil
	}

	if err := b.exchange.CancelOrder(ctx, b.config.Symbol, b.currentOrder.OrderID); err != nil {
		return err
	}

	b.logger.Info("Order cancelled successfully",
		zap.String("symbol", b.config.Symbol),
		zap.String("order_id", b.currentOrder.OrderID))

	return nil
}

func (b *FundingBot) getStatus(ctx context.Context) (*FundingBotStatus, error) {
	b.mu.Lock()
	running := b.running
	currentOrder := b.currentOrder
	b.mu.Unlock()

	status := &FundingBotStatus{
		Running: running,
	}

	// Get current position
	position, err := b.exchange.GetPosition(ctx, b.config.Symbol)
	if err == nil && position.Size > 0 {
		status.Position = position
	}

	// Get funding info
	tickers, err := b.exchange.GetTickers(ctx, "linear")
	if err == nil {
		for _, t := range tickers {
			if t.Symbol == b.config.Symbol {
				// Convert NextFundingTime from milliseconds to seconds if needed
				// Bybit returns ms, but we want seconds for countdown
				nextFundingTimeSec := t.NextFundingTime
				if nextFundingTimeSec > 1000000000000 { // Heuristic check: > year 2001 in ms
					nextFundingTimeSec = nextFundingTimeSec / 1000
				}
				status.NextFundingTime = nextFundingTimeSec

				now := time.Now().Unix()
				status.Countdown = nextFundingTimeSec - now

				// Generate signal message
				if currentOrder != nil {
					status.CurrentOrder = currentOrder
					status.Signal = fmt.Sprintf("⏳ Order placed, waiting for funding event (%.2fs)", float64(status.Countdown))
				} else if status.Countdown <= int64(b.config.CountdownThreshold.Seconds()) {
					status.Signal = fmt.Sprintf("🎯 Within threshold! Placing order... (%.2fs)", float64(status.Countdown))
				} else {
					status.Signal = fmt.Sprintf("⏰ Waiting for threshold (%.2fs remaining, threshold: %.0fs)",
						float64(status.Countdown),
						b.config.CountdownThreshold.Seconds())
				}

				// Add funding rate info
				status.FundingRate = t.FundingRate // Set funding rate
				if t.FundingRate > 0 {
					status.Signal += fmt.Sprintf(" | Funding: %.4f%%", t.FundingRate*100)
				}

				break
			}
		}
	}

	// Round countdown to avoid negative values
	if status.Countdown < 0 {
		status.Countdown = 0
	}

	return status, nil
}

func (s *FundingBotService) TriggerTestEvent(ctx context.Context, symbol string) error {
	s.mu.Lock()
	bot, exists := s.bots[symbol]
	s.mu.Unlock()

	if !exists || !bot.running {
		return fmt.Errorf("no running funding bot found for %s", symbol)
	}

	return bot.TriggerTestEvent(ctx)
}

func (b *FundingBot) TriggerTestEvent(ctx context.Context) error {
	b.mu.Lock()
	if !b.running {
		b.mu.Unlock()
		return fmt.Errorf("bot is not running")
	}
	// Don't lock for the whole duration, just check running state
	b.mu.Unlock()

	b.logger.Info("⚡ TRIGGERING TEST FUNDING EVENT ⚡", zap.String("symbol", b.config.Symbol))

	// Get funding data
	tickers, err := b.exchange.GetTickers(ctx, "linear")
	if err != nil {
		return fmt.Errorf("failed to get tickers: %w", err)
	}

	var ticker domain.Ticker
	found := false
	for _, t := range tickers {
		if t.Symbol == b.config.Symbol {
			ticker = t
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("symbol %s not found in tickers", b.config.Symbol)
	}

	// BYPASS: Min Funding Rate check
	// BYPASS: Countdown check

	// Check if active order exists
	b.mu.Lock()
	if b.currentOrder != nil {
		b.mu.Unlock()
		return fmt.Errorf("active order already exists")
	}
	b.mu.Unlock()

	// Wall Check (keep this logic as it's part of the strategy we want to test)
	if b.config.WallCheckEnabled {
		orderBook, err := b.exchange.GetOrderBook(ctx, b.config.Symbol, "linear")
		if err != nil {
			return fmt.Errorf("failed to get order book: %w", err)
		}

		entryPrice := b.calculateLimitPrice(ticker.LastPrice, ticker.FundingRate)

		avgVolume := 0.0
		for _, a := range orderBook.Asks {
			avgVolume += a.Size
		}
		if len(orderBook.Asks) > 0 {
			avgVolume /= float64(len(orderBook.Asks))
		}
		multiplier := b.config.WallThresholdMultiplier
		if multiplier <= 0 {
			multiplier = 2.0
		}
		threshold := avgVolume * multiplier
		hasWall := false
		wallPrice := 0.0

		if ticker.FundingRate >= 0 {
			// Check Ask walls for Short entry
			for _, ask := range orderBook.Asks {
				if ask.Price > ticker.LastPrice && ask.Price <= entryPrice*1.001 {
					if ask.Size > threshold {
						if b.marketService.IsWallStable(b.config.Symbol, ask.Price, "ask", threshold, 30*time.Second) {
							hasWall = true
							wallPrice = ask.Price
							break
						}
					}
				}
			}
		} else {
			// Check Bid walls for Long entry
			for _, bid := range orderBook.Bids {
				if bid.Price < ticker.LastPrice && bid.Price >= entryPrice*0.999 {
					if bid.Size > threshold {
						if b.marketService.IsWallStable(b.config.Symbol, bid.Price, "bid", threshold, 30*time.Second) {
							hasWall = true
							wallPrice = bid.Price
							break
						}
					}
				}
			}
		}

		b.logger.Info("Test Event: Performing actual wall check",
			zap.String("symbol", b.config.Symbol),
			zap.Bool("has_wall", hasWall),
			zap.Float64("wall_price", wallPrice))

		if hasWall {
			b.logger.Info("Test Event: Stable wall detected! Order would be skipped in real scenario.",
				zap.String("symbol", b.config.Symbol))
			return fmt.Errorf("stable wall detected, test skipped order placement")
		}
	}

	b.logger.Info("Test Event: Conditions OK. Triggering handleFundingEvent logic.",
		zap.String("symbol", b.config.Symbol),
		zap.Float64("current_funding_rate", ticker.FundingRate*100))

	// Directly call handleFundingEvent to simulate the funding event logic
	return b.handleFundingEvent(ctx)
}

func (b *FundingBot) logTradeTick(ctx context.Context, ticker domain.Ticker) {
	// 1. Get RSI
	rsi, err := b.getRSI(ctx)
	if err != nil {
		// Log warning but don't spam if API fails occasionally
		// b.logger.Warn("Failed to calc RSI", zap.Error(err))
	}

	// 2. Get OrderBook (Top levels)
	orderBook, err := b.exchange.GetOrderBook(ctx, b.config.Symbol, "linear")
	if err != nil {
		return
	}

	// Get Market Stats for Velocity
	var tradeVel float64
	if b.marketService != nil {
		stats, err := b.marketService.GetMarketStats(ctx, b.config.Symbol)
		if err == nil {
			tradeVel = stats.TradeVelocity
		}
	}

	// Format Order Book (Top 5 Bids/Asks)
	type info struct {
		P float64 `json:"p"`
		S float64 `json:"s"`
	}
	var bids, asks []info
	for i, e := range orderBook.Bids {
		if i >= 5 {
			break
		}
		bids = append(bids, info{P: e.Price, S: e.Size})
	}
	for i, e := range orderBook.Asks {
		if i >= 5 {
			break
		}
		asks = append(asks, info{P: e.Price, S: e.Size})
	}

	// 3. Log
	b.logger.Info("📊 Trade Tick Log",
		zap.String("symbol", b.config.Symbol),
		zap.Float64("price", ticker.LastPrice),
		zap.Float64("rsi_14", rsi),
		zap.Float64("volume_24h", ticker.Volume24h),
		zap.Float64("trade_velocity", tradeVel),
		zap.Any("top_bids", bids),
		zap.Any("top_asks", asks),
	)

	// 4. Collect Tick Data
	b.mu.Lock()
	b.sessionTicks = append(b.sessionTicks, domain.TickData{
		Timestamp:     time.Now().Unix(),
		Price:         ticker.LastPrice,
		RSI:           rsi,
		Volume:        ticker.Volume24h,
		TradeVelocity: tradeVel,
		Bids:          orderBook.Bids,
		Asks:          orderBook.Asks,
	})
	b.mu.Unlock()
}

func (b *FundingBot) getRSI(ctx context.Context) (float64, error) {
	// Fetch last 15 candles (1m)
	candles, err := b.exchange.GetCandles(ctx, b.config.Symbol, "1", 20)
	if err != nil {
		return 0, err
	}
	if len(candles) < 15 {
		return 0, fmt.Errorf("not enough candles for RSI")
	}

	// Use last 14 changes
	startIdx := len(candles) - 15
	subset := candles[startIdx:]

	var gains, losses float64
	for i := 1; i < len(subset); i++ {
		change := subset[i].Close - subset[i-1].Close
		if change > 0 {
			gains += change
		} else {
			losses += -change
		}
	}

	avgGain := gains / 14.0
	avgLoss := losses / 14.0

	if avgLoss == 0 {
		if avgGain == 0 {
			return 50, nil // Flat
		}
		return 100, nil
	}

	rs := avgGain / avgLoss
	rsi := 100.0 - (100.0 / (1.0 + rs))
	return rsi, nil
}
