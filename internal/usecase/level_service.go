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

	// Position Cache
	positionCache map[string]*domain.Position
	positionTime  map[string]time.Time
}

func NewLevelService(
	levelRepo domain.LevelRepository,
	tradeRepo domain.TradeRepository,
	exchange domain.Exchange,
	market *MarketService,
) *LevelService {
	return &LevelService{
		levelRepo:     levelRepo,
		tradeRepo:     tradeRepo,
		exchange:      exchange,
		market:        market,
		evaluator:     NewLevelEvaluator(),
		engine:        NewSublevelEngine(),
		executor:      NewTradeExecutor(exchange),
		lastPrices:    make(map[string]float64),
		levelsCache:   make(map[string][]*domain.Level),
		tiersCache:    make(map[string]*domain.SymbolTiers),
		positionCache: make(map[string]*domain.Position),
		positionTime:  make(map[string]time.Time),
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
		pos, err := s.getPosition(ctx, symbol)
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

func (s *LevelService) getPosition(ctx context.Context, symbol string) (*domain.Position, error) {
	s.mu.RLock()
	cached, ok := s.positionCache[symbol]
	ts, timeOk := s.positionTime[symbol]
	s.mu.RUnlock()

	// Cache TTL: 1 second
	if ok && timeOk && time.Since(ts) < 1*time.Second {
		// Return a copy to prevent external mutation affecting the cache
		cachedCopy := *cached
		return &cachedCopy, nil
	}

	// Fetch from exchange
	pos, err := s.exchange.GetPosition(ctx, symbol)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.positionCache[symbol] = pos
	s.positionTime[symbol] = time.Now()
	s.mu.Unlock()

	// Return a copy
	posCopy := *pos
	return &posCopy, nil
}

func (s *LevelService) invalidatePositionCache(symbol string) {
	s.mu.Lock()
	delete(s.positionCache, symbol)
	delete(s.positionTime, symbol)
	s.mu.Unlock()
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

	// if !ok {
	// 	return nil
	// }

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

	// --- SENTIMENT-BASED EXIT LOGIC ---
	// Check if any level for this symbol has DisableSpeedClose enabled
	speedCloseDisabled := false
	for _, l := range relevantLevels {
		if l.DisableSpeedClose {
			speedCloseDisabled = true
			break
		}
	}

	// Check Position for Exit Logic (TP and Sentiment)
	pos, err := s.getPosition(ctx, symbol)
	if err == nil && pos.Size > 0 {
		// --- STOP LOSS AT BASE LOGIC ---
		// Find relevant level (closest to entry)
		// We already found tpLevel, let's reuse or find again if needed.
		// Actually, we need to check ALL relevant levels or just the closest?
		// Usually just the one we are trading against.
		// Let's reuse tpLevel logic but rename to relevantLevel for clarity.
		var activeLevel *domain.Level
		minDiff := 1e9
		for _, l := range relevantLevels {
			diff := pos.EntryPrice - l.LevelPrice
			if diff < 0 {
				diff = -diff
			}
			if diff < minDiff {
				minDiff = diff
				activeLevel = l
			}
		}

		if activeLevel != nil {
			// Check TP
			if activeLevel.TakeProfitPct > 0 {
				shouldTP := false
				if pos.Side == domain.SideLong {
					tpPrice := pos.EntryPrice * (1 + activeLevel.TakeProfitPct)
					if price >= tpPrice {
						log.Printf("TAKE PROFIT: LONG on %s. Price %f >= TP %f. Closing...", symbol, price, tpPrice)
						shouldTP = true
					}
				} else if pos.Side == domain.SideShort {
					tpPrice := pos.EntryPrice * (1 - activeLevel.TakeProfitPct)
					if price <= tpPrice {
						log.Printf("TAKE PROFIT: SHORT on %s. Price %f <= TP %f. Closing...", symbol, price, tpPrice)
						shouldTP = true
					}
				}

				if shouldTP {
					if _, err := s.finalizePosition(ctx, symbol, "Take Profit", "take-profit", price); err != nil {
						log.Printf("Failed to finalize position on TP: %v", err)
					}
					return nil
				}
			}

			// Check Stop Loss at Base
			if activeLevel.StopLossAtBase {
				shouldSL := false
				if pos.Side == domain.SideLong {
					// Long: Close if Price <= LevelPrice
					if price <= activeLevel.LevelPrice {
						log.Printf("STOP LOSS (Base): LONG on %s. Price %f <= Level %f. Closing...", symbol, price, activeLevel.LevelPrice)
						shouldSL = true
					}
				} else if pos.Side == domain.SideShort {
					// Short: Close if Price >= LevelPrice
					if price >= activeLevel.LevelPrice {
						log.Printf("STOP LOSS (Base): SHORT on %s. Price %f >= Level %f. Closing...", symbol, price, activeLevel.LevelPrice)
						shouldSL = true
					}
				}

				if shouldSL {
					if _, err := s.finalizePosition(ctx, symbol, "Stop Loss (Base)", "stop-loss-base", price); err != nil {
						log.Printf("Failed to finalize position on SL: %v", err)
					}
					return nil
				}
			}
		}

		// --- SENTIMENT-BASED EXIT LOGIC ---
		if !speedCloseDisabled {
			// Determine if in strict zone (within 1% of any level)
			inStrictZone := false
			for _, l := range relevantLevels {
				distance := (price - l.LevelPrice) / l.LevelPrice
				if distance < 0 {
					distance = -distance
				}
				const strictZoneThreshold = 0.01 // 1%
				if distance < strictZoneThreshold {
					inStrictZone = true
					break
				}
			}

			if inStrictZone {
				sentimentThreshold = 0.3
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
				if _, err := s.finalizePosition(ctx, symbol, "Sentiment Exit", "sentiment-exit", price); err != nil {
					log.Printf("Failed to finalize position on sentiment: %v", err)
				}
				return nil
			}
		}
	}

	if !ok {
		return nil
	}

	for _, level := range relevantLevels {
		s.processLevel(ctx, level, tiers, prevPrice, price, sentiment, sentimentThreshold)
	}

	return nil
}

func (s *LevelService) processLevel(ctx context.Context, level *domain.Level, tiers *domain.SymbolTiers, prevPrice, currPrice, sentiment, sentimentThreshold float64) {
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
			realizedPnL, err := s.finalizePosition(ctx, level.Symbol, "Level Cross", level.ID, currPrice)
			if err != nil {
				log.Printf("WARNING: Failed to finalize position for %s: %v", level.Symbol, err)
			}

			// Update State with Win/Loss
			// Note: finalizePosition resets state for ALL levels.
			// But we want to update ConsecutiveWins for THIS level.
			// Since we just reset it, we need to re-apply the win/loss to the fresh state?
			// Or we should have updated it BEFORE reset.
			// But finalizePosition resets it.
			// This is a problem. finalizePosition resets state indiscriminately.

			// Solution: Update state AFTER finalizePosition (which resets it).
			// If we update it after reset, it will be clean state + wins.
			s.engine.UpdateState(level.ID, func(ls *LevelState) {
				if realizedPnL > 0 {
					ls.ConsecutiveWins++
					log.Printf("AUDIT: Win recorded for Level %s. Consecutive Wins: %d", level.ID, ls.ConsecutiveWins)
				} else {
					ls.ConsecutiveWins = 0
					log.Printf("AUDIT: Loss recorded for Level %s. Streak reset.", level.ID)
				}
			})
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
		s.invalidatePositionCache(level.Symbol)

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

// CheckSafety iterates over all active levels and checks if the current position is safe.
// Safety Condition:
// - Long Position: Price must be >= LevelPrice
// - Short Position: Price must be <= LevelPrice
// If unsafe, it closes the position immediately.
func (s *LevelService) CheckSafety(ctx context.Context) {
	s.mu.RLock()
	// Copy cache to avoid holding lock during IO
	levelsMap := make(map[string][]*domain.Level)
	for k, v := range s.levelsCache {
		levelsMap[k] = v
	}
	s.mu.RUnlock()

	for symbol, levels := range levelsMap {
		if len(levels) == 0 {
			continue
		}

		pos, err := s.getPosition(ctx, symbol)
		if err != nil {
			log.Printf("SAFETY: Failed to get position for %s: %v", symbol, err)
			continue
		}

		if pos.Size == 0 {
			continue
		}

		// Find the level closest to the Entry Price
		var relevantLevel *domain.Level
		minDiff := 1e9 // Infinity
		for _, l := range levels {
			diff := pos.EntryPrice - l.LevelPrice
			if diff < 0 {
				diff = -diff
			}
			if diff < minDiff {
				minDiff = diff
				relevantLevel = l
			}
		}

		if relevantLevel == nil {
			continue
		}

		// Check Safety against this relevant level
		price := s.GetLatestPrice(symbol)
		if price == 0 {
			continue
		}

		shouldClose := false
		if pos.Side == domain.SideLong {
			// Long: Price should be > Level
			// If Price < Level, we are losing and below base.
			if price < relevantLevel.LevelPrice {
				log.Printf("SAFETY: UNSAFE LONG on %s. Price %f < Level %f. Closing...", symbol, price, relevantLevel.LevelPrice)
				shouldClose = true
			}
		} else if pos.Side == domain.SideShort {
			// Short: Price should be < Level
			// If Price > Level, we are losing and above base.
			if price > relevantLevel.LevelPrice {
				log.Printf("SAFETY: UNSAFE SHORT on %s. Price %f > Level %f. Closing...", symbol, price, relevantLevel.LevelPrice)
				shouldClose = true
			}
		}

		if shouldClose {
			if _, err := s.finalizePosition(ctx, symbol, "Safety Exit", "safety-exit", price); err != nil {
				log.Printf("SAFETY: Failed to finalize position for %s: %v", symbol, err)
			} else {
				log.Printf("SAFETY: Closed position for %s", symbol)
			}
		}
	}
}

// ClosePosition manually closes a position for a symbol
func (s *LevelService) ClosePosition(ctx context.Context, symbol string) error {
	_, err := s.finalizePosition(ctx, symbol, "Manual Close", "manual-close", s.GetLatestPrice(symbol))
	return err
}

// finalizePosition handles the common logic for closing a position, calculating PnL, and saving history.
func (s *LevelService) finalizePosition(ctx context.Context, symbol, reason, levelID string, price float64) (float64, error) {
	// 1. Fetch position details
	pos, err := s.getPosition(ctx, symbol)
	if err != nil || pos == nil || pos.Size == 0 {
		log.Printf("FINALIZE: Warning: No active position found for %s when closing (%s). Proceeding to ensure close.", symbol, reason)
		// We still try to close on exchange to be safe
	}

	// 2. Close on Exchange
	if err := s.exchange.ClosePosition(ctx, symbol); err != nil {
		log.Printf("FINALIZE: Failed to close position for %s: %v. Proceeding with state reset.", symbol, err)
		// We proceed to reset state to avoid getting stuck, assuming the position might be closed manually or liquidated.
	}

	// 3. Invalidate Cache
	s.invalidatePositionCache(symbol)

	// 4. Reset State for all levels of this symbol
	s.mu.RLock()
	levels := s.levelsCache[symbol]
	s.mu.RUnlock()

	for _, l := range levels {
		// We reset state here. Specific logic (like updating wins) should be handled by caller if needed,
		// but since we reset ALL, it's tricky.
		// For now, we just reset.
		s.engine.ResetState(l.ID)
	}

	// 5. Calculate PnL and Save History
	var realizedPnL float64
	var side domain.Side = "UNKNOWN"
	var size float64
	var entryPrice float64
	var leverage int
	var marginType string

	if pos != nil && pos.Size > 0 {
		side = pos.Side
		size = pos.Size
		entryPrice = pos.EntryPrice
		leverage = pos.Leverage
		marginType = pos.MarginType

		if side == domain.SideLong {
			realizedPnL = (price - entryPrice) * size
		} else {
			realizedPnL = (entryPrice - price) * size
		}

		// Save Position History
		history := &domain.PositionHistory{
			Exchange:    pos.Exchange,
			Symbol:      pos.Symbol,
			Side:        side,
			Size:        size,
			EntryPrice:  entryPrice,
			ExitPrice:   price,
			RealizedPnL: realizedPnL,
			Leverage:    leverage,
			MarginType:  marginType,
			ClosedAt:    time.Now(),
		}
		if err := s.tradeRepo.SavePositionHistory(ctx, history); err != nil {
			log.Printf("Failed to save position history: %v", err)
		}
	}

	// 6. Log Trade (Close)
	exchangeName := "unknown"
	if pos != nil {
		exchangeName = pos.Exchange
	} else if len(levels) > 0 {
		exchangeName = levels[0].Exchange
	}

	s.tradeRepo.SaveTrade(ctx, &domain.Order{
		Exchange:    exchangeName,
		Symbol:      symbol,
		LevelID:     levelID,
		Side:        side,
		Size:        0, // Close marker
		Price:       price,
		RealizedPnL: realizedPnL,
		CreatedAt:   time.Now(),
	})

	log.Printf("FINALIZE: Closed %s on %s. Reason: %s. PnL: %f", side, symbol, reason, realizedPnL)
	return realizedPnL, nil
}
