package usecase

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/vitos/crypto_trade_level/internal/domain"
	"go.uber.org/zap"
)

type SpeedBotConfig struct {
	Symbol       string        `json:"symbol"`
	PositionSize float64       `json:"position_size"`
	Leverage     int           `json:"leverage"`
	MarginType   string        `json:"margin_type"`
	Cooldown     time.Duration `json:"cooldown"`
}

type SpeedBotService struct {
	exchange      domain.Exchange
	marketService *MarketService
	bots          map[string]*SpeedBot
	logger        *zap.Logger
	mu            sync.Mutex
}

type SpeedBot struct {
	config        SpeedBotConfig
	exchange      domain.Exchange
	marketService *MarketService
	logger        *zap.Logger
	lastCloseTime time.Time
	running       bool
	stopChan      chan struct{}
	cancel        context.CancelFunc
	mu            sync.Mutex
}

type BotStatus struct {
	Running    bool             `json:"running"`
	Position   *domain.Position `json:"position,omitempty"`
	Signal     string           `json:"signal"`
	InCooldown bool             `json:"in_cooldown"`
	LastSignal time.Time        `json:"last_signal"`
}

func NewSpeedBotService(exchange domain.Exchange, marketService *MarketService, logger *zap.Logger) *SpeedBotService {
	return &SpeedBotService{
		exchange:      exchange,
		marketService: marketService,
		bots:          make(map[string]*SpeedBot),
		logger:        logger,
	}
}

func (s *SpeedBotService) StartBot(ctx context.Context, config SpeedBotConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if bot already running for this symbol
	if bot, exists := s.bots[config.Symbol]; exists && bot.running {
		return fmt.Errorf("bot already running for %s", config.Symbol)
	}

	// Create new bot
	bot := &SpeedBot{
		config:        config,
		exchange:      s.exchange,
		marketService: s.marketService,
		logger:        s.logger,
		running:       true,
		stopChan:      make(chan struct{}),
	}

	s.bots[config.Symbol] = bot

	// Start bot loop with background context (not request context!)
	// The request context expires after the HTTP response is sent
	// Use a cancellable context derived from background
	botCtx, cancel := context.WithCancel(context.Background())
	bot.cancel = cancel
	go bot.run(botCtx)

	s.logger.Info("Speed bot started", zap.String("symbol", config.Symbol))
	return nil
}

func (s *SpeedBotService) StopBot(symbol string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	bot, exists := s.bots[symbol]
	if !exists || !bot.running {
		return fmt.Errorf("no running bot found for %s", symbol)
	}

	bot.stop()
	delete(s.bots, symbol)

	s.logger.Info("Speed bot stopped", zap.String("symbol", symbol))
	return nil
}

func (s *SpeedBotService) GetBotStatus(ctx context.Context, symbol string) (*BotStatus, error) {
	s.mu.Lock()
	bot, exists := s.bots[symbol]
	s.mu.Unlock()

	if !exists {
		return &BotStatus{Running: false}, nil
	}

	return bot.getStatus(ctx)
}

func (b *SpeedBot) run(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	b.logger.Info("Bot evaluation loop started", zap.String("symbol", b.config.Symbol))

	for {
		select {
		case <-ticker.C:
			if err := b.evaluate(ctx); err != nil {
				b.logger.Error("Bot evaluation error", zap.Error(err), zap.String("symbol", b.config.Symbol))
			}
		case <-b.stopChan:
			b.logger.Info("Bot evaluation loop stopped", zap.String("symbol", b.config.Symbol))
			return
		case <-ctx.Done():
			b.logger.Info("Bot evaluation loop cancelled", zap.String("symbol", b.config.Symbol))
			return
		}
	}
}

func (b *SpeedBot) stop() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.running {
		b.running = false
		if b.cancel != nil {
			b.cancel()
		}
		close(b.stopChan)
	}
}

func (b *SpeedBot) evaluate(ctx context.Context) error {

	// No lock needed for reading config as it is immutable after start
	// b.mu.Lock()
	// defer b.mu.Unlock()

	// Get market stats
	stats, err := b.marketService.GetMarketStats(ctx, b.config.Symbol)
	if err != nil {
		return fmt.Errorf("failed to get market stats: %w", err)
	}

	// Get current position
	position, err := b.exchange.GetPosition(ctx, b.config.Symbol)
	if err != nil {
		return fmt.Errorf("failed to get position: %w", err)
	}

	// Calculate divergence
	totalSpeed := stats.SpeedBuy + stats.SpeedSell
	totalDepth := stats.DepthBid + stats.DepthAsk

	speedRatio := 0.5
	if totalSpeed > 0 {
		speedRatio = stats.SpeedBuy / totalSpeed
	}

	depthRatio := 0.5
	if totalDepth > 0 {
		depthRatio = stats.DepthBid / totalDepth
	}

	divergence := speedRatio - depthRatio

	// If we have a position, check for close signal
	if position.Size > 0 {
		if b.shouldClose(stats, position.Side) {
			b.logger.Info("Closing position",
				zap.String("symbol", b.config.Symbol),
				zap.String("side", string(position.Side)),
				zap.Float64("size", position.Size))

			if err := b.exchange.ClosePosition(ctx, b.config.Symbol); err != nil {
				return fmt.Errorf("failed to close position: %w", err)
			}

			b.lastCloseTime = time.Now()
			return nil
		}
		return nil // Position open, no close signal
	}

	// Check cooldown
	if time.Since(b.lastCloseTime) < b.config.Cooldown {
		return nil // Still in cooldown
	}

	// Check for open signals (3 gauges: Trade Volume, Price Change, Divergence)
	longSignal := stats.SpeedBuy > stats.SpeedSell &&
		stats.PriceChange60s > 0 &&
		divergence > 0

	shortSignal := stats.SpeedSell > stats.SpeedBuy &&
		stats.PriceChange60s < 0 &&
		divergence < 0

	// Log signal values
	b.logger.Info("Signal check",
		zap.String("symbol", b.config.Symbol),
		zap.Float64("speedBuy", stats.SpeedBuy),
		zap.Float64("speedSell", stats.SpeedSell),
		zap.Float64("priceChange", stats.PriceChange60s),
		zap.Float64("divergence", divergence),
		zap.Bool("longSignal", longSignal),
		zap.Bool("shortSignal", shortSignal))

	if longSignal {
		b.logger.Info("Opening LONG position",
			zap.String("symbol", b.config.Symbol),
			zap.Float64("size", b.config.PositionSize))

		return b.exchange.MarketBuy(ctx, b.config.Symbol, b.config.PositionSize,
			b.config.Leverage, b.config.MarginType, 0)
	}

	if shortSignal {
		b.logger.Info("Opening SHORT position",
			zap.String("symbol", b.config.Symbol),
			zap.Float64("size", b.config.PositionSize))

		return b.exchange.MarketSell(ctx, b.config.Symbol, b.config.PositionSize,
			b.config.Leverage, b.config.MarginType, 0)
	}

	return nil
}

func (b *SpeedBot) shouldClose(stats *MarketStats, currentSide domain.Side) bool {
	// Close LONG if selling pressure
	if currentSide == domain.SideLong && stats.SpeedSell > stats.SpeedBuy {
		return true
	}

	// Close SHORT if buying pressure
	if currentSide == domain.SideShort && stats.SpeedBuy > stats.SpeedSell {
		return true
	}

	return false
}

func (b *SpeedBot) getStatus(ctx context.Context) (*BotStatus, error) {
	b.mu.Lock()
	running := b.running
	lastCloseTime := b.lastCloseTime
	cooldownDuration := b.config.Cooldown
	b.mu.Unlock()

	inCooldown := time.Since(lastCloseTime) < cooldownDuration

	status := &BotStatus{
		Running:    running,
		InCooldown: inCooldown,
	}

	// Get current position
	position, err := b.exchange.GetPosition(ctx, b.config.Symbol)
	if err == nil && position.Size > 0 {
		status.Position = position
		status.Signal = fmt.Sprintf("Position: %s %.4f @ $%.2f",
			position.Side, position.Size, position.EntryPrice)
	} else {
		// Get market stats to show current signal
		stats, err := b.marketService.GetMarketStats(ctx, b.config.Symbol)
		if err == nil {
			totalSpeed := stats.SpeedBuy + stats.SpeedSell
			totalDepth := stats.DepthBid + stats.DepthAsk

			speedRatio := 0.5
			if totalSpeed > 0 {
				speedRatio = stats.SpeedBuy / totalSpeed
			}

			depthRatio := 0.5
			if totalDepth > 0 {
				depthRatio = stats.DepthBid / totalDepth
			}

			divergence := speedRatio - depthRatio

			longSignal := stats.SpeedBuy > stats.SpeedSell &&
				stats.PriceChange60s > 0 &&
				divergence > 0

			shortSignal := stats.SpeedSell > stats.SpeedBuy &&
				stats.PriceChange60s < 0 &&
				divergence < 0

			if status.InCooldown {
				status.Signal = fmt.Sprintf("Cooldown: %ds remaining",
					int(cooldownDuration.Seconds()-time.Since(lastCloseTime).Seconds()))
			} else if longSignal {
				status.Signal = "🟢 LONG signal detected - 3 gauges bullish"
			} else if shortSignal {
				status.Signal = "🔴 SHORT signal detected - 3 gauges bearish"
			} else {
				status.Signal = "⚪ Waiting for confluence - Gauges not aligned"
			}
		}
	}

	return status, nil
}
