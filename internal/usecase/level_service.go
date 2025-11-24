package usecase

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/vitos/crypto_trade_level/internal/domain"
)

// LevelService orchestrates the trading logic.
type LevelService struct {
	levelRepo domain.LevelRepository
	tradeRepo domain.TradeRepository
	exchange  domain.Exchange
	market    *MarketService // Injected dependency
	evaluator *LevelEvaluator
	engine    *SublevelEngine
	executor  *TradeExecutor

	mu         sync.RWMutex
	lastPrices map[string]float64 // symbol -> price

	// Cache
	levelsCache map[string][]*domain.Level     // symbol -> levels
	tiersCache  map[string]*domain.SymbolTiers // symbol -> tiers
}

func NewLevelService(
	levelRepo domain.LevelRepository,
	tradeRepo domain.TradeRepository,
	exchange domain.Exchange,
	market *MarketService,
) *LevelService {
	return &LevelService{
		levelRepo:   levelRepo,
		tradeRepo:   tradeRepo,
		exchange:    exchange,
		market:      market,
		evaluator:   NewLevelEvaluator(),
		engine:      NewSublevelEngine(),
		executor:    NewTradeExecutor(exchange),
		lastPrices:  make(map[string]float64),
		levelsCache: make(map[string][]*domain.Level),
		tiersCache:  make(map[string]*domain.SymbolTiers),
	}
}

// GetLatestPrice returns the last known price for a symbol
func (s *LevelService) GetLatestPrice(symbol string) float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastPrices[symbol]
}

func (s *LevelService) GetExchange() domain.Exchange {
	return s.exchange
}

// GetPositions fetches active positions for all symbols with levels
func (s *LevelService) GetPositions(ctx context.Context) ([]*domain.Position, error) {
	levels, err := s.levelRepo.ListLevels(ctx)
	if err != nil {
		return nil, err
	}

	uniqueSymbols := make(map[string]bool)
	for _, l := range levels {
		uniqueSymbols[l.Symbol] = true
	}

	var positions []*domain.Position
	for symbol := range uniqueSymbols {
		pos, err := s.exchange.GetPosition(ctx, symbol)
		if err != nil {
			// Assuming a logger exists, otherwise log directly
			log.Printf("Failed to get position for symbol %s: %v", symbol, err)
			continue
		}
		if pos.Size > 0 { // Only add positions with actual size
			positions = append(positions, pos)
		}
	}
	return positions, nil
}

// UpdateCache refreshes the in-memory cache of levels and tiers
func (s *LevelService) UpdateCache(ctx context.Context) error {
	levels, err := s.levelRepo.ListLevels(ctx)
	if err != nil {
		return err
	}

	newLevelsCache := make(map[string][]*domain.Level)
	uniqueSymbols := make(map[string]bool)

	for _, l := range levels {
		newLevelsCache[l.Symbol] = append(newLevelsCache[l.Symbol], l)
		uniqueSymbols[l.Symbol] = true
	}

	newTiersCache := make(map[string]*domain.SymbolTiers)
	for symbol := range uniqueSymbols {
		// Assuming exchange name is consistent or we need to handle multiple exchanges per symbol?
		// For now, let's pick the exchange from the first level of that symbol or just iterate.
		// The current architecture seems to assume one exchange per symbol or handled by the caller.
		// Let's look at how ProcessTick worked: it filtered by exchangeName.
		// Here we just cache by symbol. The tiers are per (exchange, symbol).
		// We might need a composite key or just cache by symbol if unique enough.
		// Let's use "bybit" as default or extract from levels.
		// Ideally, we should iterate unique (exchange, symbol) pairs.

		// Find exchange for this symbol
		var exchangeName string
		if len(newLevelsCache[symbol]) > 0 {
			exchangeName = newLevelsCache[symbol][0].Exchange
		}

		tiers, err := s.levelRepo.GetSymbolTiers(ctx, exchangeName, symbol)
		if err != nil {
			log.Printf("Warning: Failed to fetch tiers for %s: %v", symbol, err)
			continue
		}
		newTiersCache[symbol] = tiers
	}

	s.mu.Lock()
	s.levelsCache = newLevelsCache
	s.tiersCache = newTiersCache
	s.mu.Unlock()

	return nil
}

// ProcessTick should be called when a new price arrives (e.g. from WebSocket).
func (s *LevelService) ProcessTick(ctx context.Context, exchangeName, symbol string, price float64) error {
	// fmt.Printf("Tick: %s %f\n", symbol, price) // Too noisy
	s.mu.Lock()
	prevPrice, ok := s.lastPrices[symbol]
	s.lastPrices[symbol] = price

	// Read from cache while locked
	levels := s.levelsCache[symbol]
	tiers := s.tiersCache[symbol]
	s.mu.Unlock()

	if !ok {
		return nil
	}

	if len(levels) == 0 {
		return nil
	}

	// Filter for this exchange
	var relevantLevels []*domain.Level
	for _, l := range levels {
		if l.Exchange == exchangeName {
			relevantLevels = append(relevantLevels, l)
		}
	}

	if len(relevantLevels) == 0 {
		return nil
	}

	if tiers == nil {
		return nil // No tiers, can't trade
	}

	// --- SENTIMENT LOGIC ---
	sentiment, err := s.market.GetTradeSentiment(ctx, symbol)
	if err != nil {
		log.Printf("Error getting sentiment for %s: %v", symbol, err)
		sentiment = 0 // Default to neutral
	}

	// Dynamic Threshold Logic
	// Default Loose Threshold
	sentimentThreshold := 0.6

	// Check if we have an open position
	pos, err := s.exchange.GetPosition(ctx, symbol)
	if err == nil && pos.Size > 0 {
		// Determine if we are in a "Strict Zone"
		// Strict Zone = Between Base Level and Edge Tier (Max Tier)
		inStrictZone := false

		for _, l := range relevantLevels {
			// Calculate Max Tier Percentage
			maxTierPct := tiers.Tier1Pct
			if tiers.Tier2Pct > maxTierPct {
				maxTierPct = tiers.Tier2Pct
			}
			if tiers.Tier3Pct > maxTierPct {
				maxTierPct = tiers.Tier3Pct
			}

			if pos.Side == domain.SideLong {
				// Long Zone: [BaseLevel, BaseLevel * (1 + MaxTierPct)]
				upperBound := l.LevelPrice * (1 + maxTierPct)
				if price >= l.LevelPrice && price <= upperBound {
					inStrictZone = true
					break
				}
			} else if pos.Side == domain.SideShort {
				// Short Zone: [BaseLevel * (1 - MaxTierPct), BaseLevel]
				lowerBound := l.LevelPrice * (1 - maxTierPct)
				if price >= lowerBound && price <= l.LevelPrice {
					inStrictZone = true
					break
				}
			}
		}

		if inStrictZone {
			sentimentThreshold = 0.3
			// log.Printf("SENTIMENT: Strict Zone for %s. Threshold: %f", symbol, sentimentThreshold)
		}

		// Check Exit Trigger
		shouldClose := false
		if pos.Side == domain.SideLong && sentiment < -sentimentThreshold {
			log.Printf("SENTIMENT: Strong Sell Speed (%f < -%f). Closing LONG on %s.", sentiment, sentimentThreshold, symbol)
			shouldClose = true
		} else if pos.Side == domain.SideShort && sentiment > sentimentThreshold {
			log.Printf("SENTIMENT: Strong Buy Speed (%f > %f). Closing SHORT on %s.", sentiment, sentimentThreshold, symbol)
			shouldClose = true
		}

		if shouldClose {
			if err := s.exchange.ClosePosition(ctx, symbol); err != nil {
				log.Printf("Failed to close position on sentiment: %v", err)
			} else {
				// Reset State for all levels of this symbol
				for _, l := range relevantLevels {
					s.engine.ResetState(l.ID)
				}
				// Log Trade
				s.tradeRepo.SaveTrade(ctx, &domain.Order{
					Exchange:  exchangeName,
					Symbol:    symbol,
					LevelID:   "sentiment-exit",
					Side:      pos.Side,
					Size:      0, // Marker
					Price:     price,
					CreatedAt: time.Now(),
				})
				return nil // Stop processing this tick
			}
		}
	}

	for _, level := range relevantLevels {
		s.processLevel(ctx, level, tiers, prevPrice, price, sentiment)
	}

	return nil
}

func (s *LevelService) processLevel(ctx context.Context, level *domain.Level, tiers *domain.SymbolTiers, prevPrice, currPrice, sentiment float64) {
	// 1. Determine Side
	side := s.evaluator.DetermineSide(level.LevelPrice, currPrice)
	if side == "" {
		return
	}

	// 2. Calculate Boundaries
	boundaries := s.evaluator.CalculateBoundaries(level, tiers, side)

	// 3. Evaluate Trigger
	action, size := s.engine.Evaluate(level, boundaries, prevPrice, currPrice, side)

	if action != ActionNone {
		// --- ENTRY FILTER ---
		const sentimentThreshold = 0.6
		if action == ActionOpen || action == ActionAddToPosition {
			if side == domain.SideLong && sentiment < -sentimentThreshold {
				log.Printf("SENTIMENT: Skipping LONG on %s. Sentiment is Bearish (%f).", level.Symbol, sentiment)
				return
			}
			if side == domain.SideShort && sentiment > sentimentThreshold {
				log.Printf("SENTIMENT: Skipping SHORT on %s. Sentiment is Bullish (%f).", level.Symbol, sentiment)
				return
			}
		}

		log.Printf("AUDIT: Action Triggered: %s. Level: %s, Symbol: %s, Side: %s, Size: %f", action, level.ID, level.Symbol, side, size)
		log.Printf("Triggered: %s on %s %s (Side: %s, Size: %f)", action, level.Exchange, level.Symbol, side, size)

		if action == ActionClose {
			// Close Position
			// We need to fetch the position BEFORE closing to calculate PnL
			// Or we can calculate it roughly: (ExitPrice - EntryPrice) * Size * Side
			// But we don't track EntryPrice in LevelState.
			// Let's fetch the position from Exchange.
			pos, err := s.exchange.GetPosition(ctx, level.Symbol)
			var realizedPnL float64
			if err == nil && pos.Size > 0 {
				// Calculate PnL
				// Long: (Exit - Entry) * Size
				// Short: (Entry - Exit) * Size
				if pos.Side == domain.SideLong {
					realizedPnL = (currPrice - pos.EntryPrice) * pos.Size
				} else {
					realizedPnL = (pos.EntryPrice - currPrice) * pos.Size
				}
				log.Printf("AUDIT: Closing Position. Symbol: %s. Entry: %f. Exit: %f. Size: %f. PnL: %f", level.Symbol, pos.EntryPrice, currPrice, pos.Size, realizedPnL)
			} else {
				log.Printf("WARNING: Could not fetch position for PnL calc before closing: %v", err)
			}

			err = s.exchange.ClosePosition(ctx, level.Symbol)
			if err != nil {
				log.Printf("WARNING: Failed to close position for %s (might be already closed): %v", level.Symbol, err)
			}

			// Use the ActiveSide from state for the Close record, as 'side' might be flipped (e.g. crossing level)
			state := s.engine.GetState(level.ID)
			closingSide := state.ActiveSide
			if closingSide == "" {
				closingSide = side // Fallback
			}

			// Update State with Win/Loss
			s.engine.UpdateState(level.ID, func(ls *LevelState) {
				if realizedPnL > 0 {
					ls.ConsecutiveWins++
					log.Printf("AUDIT: Win recorded for Level %s. Consecutive Wins: %d", level.ID, ls.ConsecutiveWins)
				} else {
					ls.ConsecutiveWins = 0
					log.Printf("AUDIT: Loss recorded for Level %s. Streak reset.", level.ID)
				}
			})

			// Reset State (Triggers and ActiveSide)
			s.engine.ResetState(level.ID)

			// Save Trade (Close)
			order := &domain.Order{
				Exchange: level.Exchange,
				Symbol:   level.Symbol,
				LevelID:  level.ID,
				Side:     closingSide,
				// Actually, for trade history, it's better to show "CLOSE" or negative size?
				// For now, let's just log it as a trade with 0 size or specific marker?
				// The user wants to see it in trades.
				Size:      0, // Size 0 indicates full close in this MVP context? Or maybe we should fetch position size before closing.
				Price:     currPrice,
				CreatedAt: time.Now(),
			}
			// We might want to mark it as a close.
			// But domain.Order doesn't have a "Type".
			// Let's just save it.
			if err := s.tradeRepo.SaveTrade(ctx, order); err != nil {
				log.Printf("Failed to save close trade: %v", err)
			}
			return
		}

		// 4. Execute Trade
		stopLoss := 0.0
		if level.StopLossAtBase && level.StopLossMode == "exchange" {
			stopLoss = level.LevelPrice
		}

		err := s.executor.Execute(ctx, level.Symbol, side, size, level.Leverage, level.MarginType, stopLoss)
		if err != nil {
			log.Printf("Failed to execute trade: %v", err)
			return
		}

		// 5. Save Trade
		order := &domain.Order{
			Exchange:  level.Exchange,
			Symbol:    level.Symbol,
			LevelID:   level.ID,
			Side:      side,
			Size:      size,
			Price:     currPrice,
			CreatedAt: time.Now(),
		}
		if err := s.tradeRepo.SaveTrade(ctx, order); err != nil {
			log.Printf("Failed to save trade: %v", err)
		}
	}
}
