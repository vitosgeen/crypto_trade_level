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
	exchange      domain.Exchange
	marketService *MarketService
	bots          map[string]*FundingBot
	logger        *zap.Logger
	mu            sync.Mutex
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

func NewFundingBotService(exchange domain.Exchange, marketService *MarketService, logger *zap.Logger) *FundingBotService {
	return &FundingBotService{
		exchange:      exchange,
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
	countdown := ticker.NextFundingTime - now

	// Check if we are approaching funding time
	thresholdSeconds := int64(b.config.CountdownThreshold.Seconds())
	if countdown <= thresholdSeconds {
		b.logger.Info("Funding countdown active (scanning)",
			zap.String("symbol", b.config.Symbol),
			zap.Int64("countdown_seconds", countdown),
			zap.Int64("threshold_seconds", thresholdSeconds),
			zap.Float64("current_rate_pct", ticker.FundingRate*100),
			zap.Float64("min_rate_pct", b.config.MinFundingRate*100))
	}

	// Check funding rate
	if ticker.FundingRate <= 0 || ticker.FundingRate < b.config.MinFundingRate {
		// Not profitable or negative funding

		// If we are in the countdown window but rate is low, log it explicitly
		if countdown <= thresholdSeconds {
			b.logger.Info("Skipping funding entry: Rate below threshold",
				zap.String("symbol", b.config.Symbol),
				zap.Float64("current_rate_pct", ticker.FundingRate*100),
				zap.Float64("min_rate_pct", b.config.MinFundingRate*100))
		}

		// Check for rollover if we were tracking a previous funding time
		if b.lastNextFundingTime > 0 && ticker.NextFundingTime > b.lastNextFundingTime {
			b.logger.Info("Funding event passed without action",
				zap.String("symbol", b.config.Symbol),
				zap.Int64("previous_funding_time", b.lastNextFundingTime),
				zap.Int64("new_funding_time", ticker.NextFundingTime),
				zap.Float64("funding_rate_pct", ticker.FundingRate*100),
				zap.Float64("min_funding_rate", b.config.MinFundingRate),
				zap.String("reason", "rate_below_threshold"))
			b.lastNextFundingTime = ticker.NextFundingTime
		}
		// Also update if not initialized
		if b.lastNextFundingTime == 0 {
			b.lastNextFundingTime = ticker.NextFundingTime
		}
		return nil
	}

	// Check for funding rollover (event happened)
	if b.lastNextFundingTime > 0 && ticker.NextFundingTime > b.lastNextFundingTime {
		b.mu.Lock()
		hasOrder := b.currentOrder != nil
		b.mu.Unlock()

		if !hasOrder {
			b.logger.Info("Funding event passed without action",
				zap.String("symbol", b.config.Symbol),
				zap.Int64("prefix_funding_time", b.lastNextFundingTime),
				zap.Int64("new_funding_time", ticker.NextFundingTime),
				zap.Float64("funding_rate_pct", ticker.FundingRate*100),
				zap.Float64("countdown_threshold_seconds", b.config.CountdownThreshold.Seconds()),
				zap.Bool("wall_check_enabled", b.config.WallCheckEnabled),
				zap.String("reason", "no_active_order"))
		}
		b.lastNextFundingTime = ticker.NextFundingTime
	}

	// Initialize tracking if needed
	if b.lastNextFundingTime == 0 {
		b.lastNextFundingTime = ticker.NextFundingTime
	}

	if countdown > thresholdSeconds {
		// Too early
		return nil
	}

	// Check if we already have an order
	b.mu.Lock()
	if b.currentOrder != nil {
		b.mu.Unlock()
		// Order already placed, wait for funding event
		return b.monitorFundingEvent(ctx, ticker.NextFundingTime)
	}
	b.mu.Unlock()

	// Get order book for wall detection
	if b.config.WallCheckEnabled {
		orderBook, err := b.exchange.GetOrderBook(ctx, b.config.Symbol, "linear")
		if err != nil {
			return fmt.Errorf("failed to get order book: %w", err)
		}

		limitPrice := b.calculateLimitPrice(ticker.LastPrice, ticker.FundingRate)
		hasWall := b.hasBigAskWall(orderBook, limitPrice)

		b.logger.Info("Performing wall check",
			zap.String("symbol", b.config.Symbol),
			zap.Float64("limit_price", limitPrice),
			zap.Bool("has_wall", hasWall),
			zap.Float64("multiplier", b.config.WallThresholdMultiplier))

		if hasWall {
			b.logger.Info("Big ask wall detected, skipping entry",
				zap.String("symbol", b.config.Symbol),
				zap.Float64("wall_price", limitPrice))
			return nil
		}
	}

	b.logger.Info("Conditions met, placing short order",
		zap.String("symbol", b.config.Symbol),
		zap.Float64("funding_rate_pct", ticker.FundingRate*100),
		zap.Int64("countdown", countdown))

	// Place limit short order
	return b.placeShortOrder(ctx, ticker)
}

func (b *FundingBot) calculateLimitPrice(currentPrice, fundingRate float64) float64 {
	// Place order slightly above current price to ensure fill
	// but below the funding rate profit margin
	// Example: If funding is 0.01%, we can afford to buy at +0.005% premium
	premium := fundingRate * 0.5
	return currentPrice * (1 + premium)
}

func (b *FundingBot) hasBigAskWall(orderBook *domain.OrderBook, limitPrice float64) bool {
	// Check if there's a large ask order (wall) at or below our limit price
	// A "wall" is defined as an order significantly larger than average

	if len(orderBook.Asks) == 0 {
		return false
	}

	avgVolume := 0.0
	count := 0

	for _, ask := range orderBook.Asks {
		avgVolume += ask.Size
		count++
	}

	if count == 0 {
		return false
	}

	avgVolume /= float64(count)
	multiplier := b.config.WallThresholdMultiplier
	if multiplier <= 0 {
		multiplier = 2.0 // Default
	}
	wallThreshold := avgVolume * multiplier

	for _, ask := range orderBook.Asks {
		if ask.Price <= limitPrice && ask.Size > wallThreshold {
			return true
		}
	}

	return false
}

func (b *FundingBot) placeShortOrder(ctx context.Context, ticker domain.Ticker) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Check if we already have an order
	if b.currentOrder != nil {
		return nil
	}

	limitPrice := b.calculateLimitPrice(ticker.LastPrice, ticker.FundingRate)

	// Calculate SL/TP prices
	var stopLossPrice, takeProfitPrice float64

	if b.config.StopLossPercentage > 0 {
		// Short position: SL is ABOVE entry price
		stopLossPrice = limitPrice * (1 + b.config.StopLossPercentage)
		// Round to 4 decimals for now (should utilize tick size in improved version)
		stopLossPrice = math.Round(stopLossPrice*10000) / 10000
	}

	if b.config.TakeProfitPercentage > 0 {
		// Short position: TP is BELOW entry price
		// User requested: TP = UserTP + FundingRate
		// If funding rate is positive (we are paid), we want to capture that + our target.
		// Example: User wants 0.5% profit. Funding is 0.1%. Total target 0.6%.
		// So we set TP price at Entry * (1 - 0.006).

		totalTargetPct := b.config.TakeProfitPercentage + ticker.FundingRate
		takeProfitPrice = limitPrice * (1 - totalTargetPct)
		// Round
		takeProfitPrice = math.Round(takeProfitPrice*10000) / 10000
	}

	order := &domain.Order{
		Symbol:      b.config.Symbol,
		Side:        domain.SideShort,
		Type:        "Limit",
		Size:        b.config.PositionSize,
		Price:       limitPrice,
		TimeInForce: "GoodTillCancel",
		ReduceOnly:  false,
		StopLoss:    stopLossPrice,
		TakeProfit:  takeProfitPrice,
	}

	placedOrder, err := b.exchange.PlaceOrder(ctx, order)
	if err != nil {
		return fmt.Errorf("failed to place order: %w", err)
	}

	b.currentOrder = placedOrder
	b.logger.Info("Placed funding arbitrage order",
		zap.String("symbol", b.config.Symbol),
		zap.String("order_id", placedOrder.OrderID),
		zap.Float64("price", limitPrice),
		zap.Float64("stop_loss", stopLossPrice),
		zap.Float64("take_profit", takeProfitPrice),
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
	return b.handleFundingEvent(ctx)
}

func (b *FundingBot) handleFundingEvent(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.currentOrder == nil {
		return nil
	}

	// Check if order was filled
	order, err := b.exchange.GetOrder(ctx, b.config.Symbol, b.currentOrder.OrderID)
	if err != nil {
		b.logger.Error("Failed to get order status", zap.Error(err))
		return err
	}

	if order.Status == "Filled" {
		b.logger.Info("Order filled, funding collected!",
			zap.String("symbol", b.config.Symbol),
			zap.String("order_id", order.OrderID))

		// Close position immediately (market order)
		if err := b.closePosition(ctx); err != nil {
			b.logger.Error("Failed to close position", zap.Error(err))
			return err
		}
	} else {
		// Order not filled, cancel it
		b.logger.Info("Order not filled, cancelling",
			zap.String("symbol", b.config.Symbol),
			zap.String("order_id", order.OrderID))

		if err := b.cancelOrder(ctx); err != nil {
			b.logger.Error("Failed to cancel order", zap.Error(err))
			return err
		}
	}

	b.currentOrder = nil
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
