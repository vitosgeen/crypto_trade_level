package usecase

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
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

	mu             sync.RWMutex
	lastPrices     map[string]float64   // symbol -> price
	lastPriceTimes map[string]time.Time // symbol -> timestamp

	// Cache
	levelsCache map[string][]*domain.Level     // symbol -> levels
	tiersCache  map[string]*domain.SymbolTiers // symbol -> tiers

	// Position Cache
	positionCache map[string]*domain.Position
	positionTime  map[string]time.Time

	// Symbol Cache
	allSymbolsCache  []string
	symbolsCacheTime time.Time
}

func NewLevelService(
	levelRepo domain.LevelRepository,
	tradeRepo domain.TradeRepository,
	exchange domain.Exchange,
	market *MarketService,
) *LevelService {
	return &LevelService{
		levelRepo:      levelRepo,
		tradeRepo:      tradeRepo,
		exchange:       exchange,
		market:         market,
		evaluator:      NewLevelEvaluator(),
		engine:         NewSublevelEngine(),
		executor:       NewTradeExecutor(exchange),
		lastPrices:     make(map[string]float64),
		lastPriceTimes: make(map[string]time.Time),
		levelsCache:    make(map[string][]*domain.Level),
		tiersCache:     make(map[string]*domain.SymbolTiers),
		positionCache:  make(map[string]*domain.Position),
		positionTime:   make(map[string]time.Time),
	}
}

// GetLatestPrice returns the last known price for a symbol
func (s *LevelService) GetLatestPrice(symbol string) float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastPrices[symbol]
}

// LoadInitialPrices fetches latest prices from exchange and seeds the cache for any missing symbols
func (s *LevelService) LoadInitialPrices(ctx context.Context) error {
	tickers, err := s.exchange.GetTickers(ctx, "linear")
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, t := range tickers {
		// Update if missing, zero, or stale (> 5 seconds)
		if val, ok := s.lastPrices[t.Symbol]; !ok || val == 0 || time.Since(s.lastPriceTimes[t.Symbol]) > 5*time.Second {
			s.lastPrices[t.Symbol] = t.LastPrice
			s.lastPriceTimes[t.Symbol] = time.Now()
		}
	}
	return nil
}

// GetLevelState returns the current runtime state of a level
func (s *LevelService) GetLevelState(levelID string) LevelState {
	return s.engine.GetState(levelID)
}

func (s *LevelService) GetExchange() domain.Exchange {
	return s.exchange
}

// GetPositions fetches active positions for the entire exchange account
func (s *LevelService) GetPositions(ctx context.Context) ([]*domain.Position, error) {
	return s.exchange.GetPositions(ctx)
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
			log.Printf("Warning: Failed to fetch tiers for %s: %v. Using defaults.", symbol, err)
			tiers = &domain.SymbolTiers{
				Exchange:  exchangeName,
				Symbol:    symbol,
				Tier1Pct:  0.005,
				Tier2Pct:  0.003,
				Tier3Pct:  0.0015,
				UpdatedAt: time.Now(),
			}
		}
		newTiersCache[symbol] = tiers
	}

	s.mu.Lock()
	s.levelsCache = newLevelsCache
	s.tiersCache = newTiersCache
	s.mu.Unlock()

	return nil
}

// CreateLevel creates a new level
func (s *LevelService) CreateLevel(ctx context.Context, level *domain.Level) error {

	// 2. Save Level
	if err := s.levelRepo.SaveLevel(ctx, level); err != nil {
		return fmt.Errorf("failed to save level: %w", err)
	}

	// 3. Update Cache
	return s.UpdateCache(ctx)
}

// GetAllSymbols returns all available symbols from the exchange
func (s *LevelService) GetAllSymbols(ctx context.Context) ([]string, error) {
	s.mu.RLock()
	if len(s.allSymbolsCache) > 0 && time.Since(s.symbolsCacheTime) < 1*time.Hour {
		symbols := make([]string, len(s.allSymbolsCache))
		copy(symbols, s.allSymbolsCache)
		s.mu.RUnlock()
		return symbols, nil
	}
	s.mu.RUnlock()

	tickers, err := s.exchange.GetTickers(ctx, "linear")
	if err != nil {
		return nil, err
	}

	var symbols []string
	for _, t := range tickers {
		symbols = append(symbols, t.Symbol)
	}

	s.mu.Lock()
	s.allSymbolsCache = symbols
	s.symbolsCacheTime = time.Now()
	s.mu.Unlock()

	return symbols, nil
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
	s.mu.Lock()
	prevPrice, ok := s.lastPrices[symbol]
	s.lastPrices[symbol] = price
	s.lastPriceTimes[symbol] = time.Now()
	levels := s.levelsCache[symbol]
	tiers := s.tiersCache[symbol]
	s.mu.Unlock()

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

	// 1. MANDATORY: Update Range High/Low for all relevant levels.
	// This must happen even on the first tick to support Auto-Split logic.
	for _, l := range relevantLevels {
		s.engine.UpdateState(l.ID, func(ls *LevelState) {
			if ls.RangeHigh == 0 || price > ls.RangeHigh {
				ls.RangeHigh = price
			}
			if ls.RangeLow == 0 || price < ls.RangeLow {
				ls.RangeLow = price
			}
		})
	}

	// 2. Position Handling (SL at Base and Eval Price updates)
	pos, err := s.getPosition(ctx, symbol)
	var activeLevel *domain.Level
	if err == nil && pos.Size > 0 {
		// Find relevant level (closest to entry)
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
			// Update the evaluation price in the position cache so the UI shows it
			s.mu.Lock()
			if p, ok := s.positionCache[symbol]; ok {
				p.EvalPrice = price
			}
			s.mu.Unlock()

			log.Printf("TICK: [%s] Price: %f, Position: %s %f @ %f (Unrealized PnL: %f), Active Level: %s (Price: %f)",
				symbol, price, pos.Side, pos.Size, pos.EntryPrice, pos.UnrealizedPnL, activeLevel.ID, activeLevel.LevelPrice)

			// Check Stop Loss at Base (Does NOT need prevPrice/crossover)
			if activeLevel.StopLossAtBase {
				shouldSL := false
				if pos.Side == domain.SideLong {
					if price <= activeLevel.LevelPrice {
						log.Printf("STOP LOSS (Base): LONG on %s. Price %f <= Level %f. Closing...", symbol, price, activeLevel.LevelPrice)
						shouldSL = true
					}
				} else if pos.Side == domain.SideShort {
					if price >= activeLevel.LevelPrice {
						log.Printf("STOP LOSS (Base): SHORT on %s. Price %f >= Level %f. Closing...", symbol, price, activeLevel.LevelPrice)
						shouldSL = true
					}
				}

				if shouldSL {
					if _, err := s.finalizePosition(ctx, symbol, "Stop Loss (Base)", activeLevel.ID, price); err != nil {
						log.Printf("Failed to finalize position on SL: %v", err)
					}
					return nil
				}
			}
		}
	}

	// 3. Strategy Evaluation Logic (Needs prevPrice and tiers)
	if !ok {
		// First tick recorded, return nil so we have prevPrice for next tick evaluation
		return nil
	}

	if tiers == nil {
		return nil // No tiers, can't evaluate strategy
	}

	log.Printf("DEBUG: %s price %f (prev %f) - relevant levels: %d", symbol, price, prevPrice, len(relevantLevels))

	// --- SENTIMENT LOGIC ---
	sentiment, err := s.market.GetTradeSentiment(ctx, symbol)
	if err != nil {
		log.Printf("Error getting sentiment for %s: %v", symbol, err)
		sentiment = 0 // Default to neutral
	}

	// Dynamic Threshold Logic
	sentimentThreshold := 0.6

	// Determine if speed close is disabled for this symbol
	speedCloseDisabled := false
	for _, l := range relevantLevels {
		if l.DisableSpeedClose {
			speedCloseDisabled = true
			break
		}
	}

	// TP and Sentiment-based Exits
	if pos != nil && pos.Size > 0 {
		// Check Take Profit
		if activeLevel != nil {
			if activeLevel.TakeProfitPct > 0 || activeLevel.TakeProfitMode == "liquidity" || activeLevel.TakeProfitMode == "sentiment" {
				shouldTP := false
				var tpPrice float64

				if activeLevel.TakeProfitMode == "liquidity" {
					dynamicTP, err := s.CalculateLiquidityTP(ctx, symbol, pos.Side, pos.EntryPrice)
					if err == nil && dynamicTP > 0 {
						tpPrice = dynamicTP
					} else {
						if pos.Side == domain.SideLong {
							tpPrice = pos.EntryPrice * (1 + activeLevel.TakeProfitPct)
						} else {
							tpPrice = pos.EntryPrice * (1 - activeLevel.TakeProfitPct)
						}
					}
				} else if activeLevel.TakeProfitMode == "sentiment" {
					baseTP := activeLevel.TakeProfitPct
					if baseTP <= 0 {
						baseTP = 0.02
					}
					stats, _ := s.market.GetMarketStats(ctx, symbol)
					score := 0.0
					if stats != nil {
						score = stats.ConclusionScore
					}
					factor := 0.5
					multiplier := 1.0
					if pos.Side == domain.SideLong {
						multiplier = 1 + (score * factor)
					} else {
						multiplier = 1 - (score * factor)
					}
					adjustedPct := baseTP * multiplier
					if adjustedPct < 0.001 {
						adjustedPct = 0.001
					}
					if pos.Side == domain.SideLong {
						tpPrice = pos.EntryPrice * (1 + adjustedPct)
					} else {
						tpPrice = pos.EntryPrice * (1 - adjustedPct)
					}
				} else {
					if pos.Side == domain.SideLong {
						tpPrice = pos.EntryPrice * (1 + activeLevel.TakeProfitPct)
					} else {
						tpPrice = pos.EntryPrice * (1 - activeLevel.TakeProfitPct)
					}
				}

				if pos.Side == domain.SideLong {
					if price >= tpPrice {
						log.Printf("TAKE PROFIT: LONG on %s. Price %f >= TP %f. Closing...", symbol, price, tpPrice)
						shouldTP = true
					}
				} else if pos.Side == domain.SideShort {
					if price <= tpPrice {
						log.Printf("TAKE PROFIT: SHORT on %s. Price %f <= TP %f. Closing...", symbol, price, tpPrice)
						shouldTP = true
					}
				}

				if shouldTP {
					if _, err := s.finalizePosition(ctx, symbol, "Take Profit", activeLevel.ID, price); err != nil {
						log.Printf("Failed to finalize position on TP: %v", err)
					}
					return nil
				}
			}
		}

		// Sentiment-Based Speed Exit
		if !speedCloseDisabled {
			// Strict zone calculation (1% from any level)
			inStrictZone := false
			for _, l := range relevantLevels {
				dist := (price - l.LevelPrice) / l.LevelPrice
				if dist < 0 {
					dist = -dist
				}
				if dist < 0.01 {
					inStrictZone = true
					break
				}
			}
			if inStrictZone {
				sentimentThreshold = 0.3
			}

			shouldClose := false
			if pos.Side == domain.SideLong && sentiment < -sentimentThreshold {
				log.Printf("SENTIMENT: Exit LONG on %s. Sentiment %f < -%f", symbol, sentiment, sentimentThreshold)
				shouldClose = true
			} else if pos.Side == domain.SideShort && sentiment > sentimentThreshold {
				log.Printf("SENTIMENT: Exit SHORT on %s. Sentiment %f > %f", symbol, sentiment, sentimentThreshold)
				shouldClose = true
			}

			if shouldClose {
				targetID := "sentiment-exit"
				if activeLevel != nil {
					targetID = activeLevel.ID
				}
				if _, err := s.finalizePosition(ctx, symbol, "Sentiment Exit", targetID, price); err != nil {
					log.Printf("Failed to finalize position on sentiment: %v", err)
				}
				return nil
			}
		}
	}

	// 4. Process new level triggers
	for _, level := range relevantLevels {
		s.processLevel(ctx, level, tiers, pos, prevPrice, price, sentiment, sentimentThreshold)
	}

	return nil
}

func (s *LevelService) processLevel(ctx context.Context, level *domain.Level, tiers *domain.SymbolTiers, pos *domain.Position, prevPrice, currPrice, sentiment, sentimentThreshold float64) {
	// Sync Check: Detect external closes
	// If the state thinks we are triggered, but the exchange says we have no position (Size 0),
	// we should reset the state so we can trade again.
	state := s.engine.GetState(level.ID)
	if state.Tier1Triggered {
		// Only sync if we successfully fetched position data (pos != nil)
		// AND that data says we have no size (pos.Size == 0).
		// We add a 10s buffer to avoid race conditions with just-opened positions where API might lag.
		if pos != nil && pos.Size == 0 {
			if time.Since(state.LastTriggerTime) > 10*time.Second {
				// Prevent spawning multiple routines
				if !state.CleanupInProgress {
					log.Printf("STATE SYNC: Position for %s (Level %s) is closed (Size 0). Starting async cleanup.", level.Symbol, level.ID)

					// Mark cleanup in progress
					s.engine.UpdateState(level.ID, func(s *LevelState) {
						s.CleanupInProgress = true
					})

					// Launch async cleanup
					go s.handleExternalCloseAsync(ctx, level)
				}
			}
		}
	}
	// 1. Determine Side
	// If the level has a fixed side, use it. Otherwise, determine based on price relative to level.
	side := level.Side
	if side == "" || side == domain.SideBoth {
		side = s.evaluator.DetermineSide(level.LevelPrice, currPrice)
	}

	if side == "" {
		return
	}

	// 1b. Respect Side Filter (Redundant if we just used it, but keeps logic safe for 'Both' case)
	// If Side was Both, we calculated 'side' dynamically.
	// If Side was Fixed, 'side' is Fixed.
	// So we don't need the filter check anymore, just proceed with 'side'.
	// But wait, if Side=Both, and DetermineSide says Short, we process as Short.
	// That's correct.

	// 2. Calculate Boundaries
	usedTiers := tiers
	if level.Tier1Pct > 0 {
		usedTiers = &domain.SymbolTiers{
			Exchange:  level.Exchange,
			Symbol:    level.Symbol,
			Tier1Pct:  level.Tier1Pct,
			Tier2Pct:  level.Tier2Pct,
			Tier3Pct:  level.Tier3Pct,
			UpdatedAt: level.CreatedAt,
		}
	} else if usedTiers == nil {
		// Final fallback for safety
		usedTiers = &domain.SymbolTiers{
			Exchange: level.Exchange,
			Symbol:   level.Symbol,
			Tier1Pct: 0.005,
			Tier2Pct: 0.003,
			Tier3Pct: 0.0015,
		}
	}

	boundaries := s.evaluator.CalculateBoundaries(level, usedTiers, side)

	// 3. Evaluate Trigger
	action, size := s.engine.Evaluate(level, boundaries, prevPrice, currPrice, side)

	if action != ActionNone {
		// --- ENTRY FILTER ---
		if action == ActionOpen || action == ActionAddToPosition {
			if !level.IgnoreSentimentFilter {
				if side == domain.SideLong && sentiment < -sentimentThreshold {
					log.Printf("SENTIMENT: Skipping LONG on %s. Sentiment is Bearish (%f).", level.Symbol, sentiment)
					return
				}
				if side == domain.SideShort && sentiment > sentimentThreshold {
					log.Printf("SENTIMENT: Skipping SHORT on %s. Sentiment is Bullish (%f).", level.Symbol, sentiment)
					return
				}
			}

			// --- STATE SYNC / DOUBLE ENTRY PROTECTION ---
			if pos != nil && pos.Size > 0 {
				// We already have a position. Check if we should skip this action.
				// If ActionOpen, we should definitely skip and just ensure state is synced.
				if action == ActionOpen {
					log.Printf("STATE SYNC: Detected existing position for %s while attempting OPEN. Syncing state only.", level.Symbol)
					s.engine.UpdateState(level.ID, func(ls *LevelState) {
						ls.Tier1Triggered = true
						ls.ActiveSide = side
						ls.LastTriggerTime = time.Now()
					})
					return
				}
				// If ActionAddToPosition, we might process it if it's a higher tier?
				// But Evaluate() already checks !Tier2Triggered.
				// However, if we restarted, Tier2Triggered is false.
				// If we have a large position, we probably already added.
				// Simple heuristic: If position size >= calculated target size, skip.
				// Target Size for Tier 2 = Base + Base = 2x Base.
				// But leverage/size management is complex.
				// SAFE BET: If we have ANY position, assume state recovery is needed for OPEN, but maybe allow Adds if significantly larger?
				// For now, let's just Sync OPEN. If the user wants to add, they can manually or let the next tier trigger.
			}
		}

		log.Printf("AUDIT: Action Triggered: %s. Level: %s, Symbol: %s, Side: %s, Size: %f", action, level.ID, level.Symbol, side, size)
		log.Printf("Triggered: %s on %s %s (Side: %s, Size: %f)", action, level.Exchange, level.Symbol, side, size)

		if action == ActionClose {
			// Close Position
			_, err := s.finalizePosition(ctx, level.Symbol, "Level Cross", level.ID, currPrice)
			if err != nil {
				log.Printf("WARNING: Failed to finalize position for %s: %v", level.Symbol, err)
			}
			return
		}

		// 4. Execute Trade
		stopLoss := 0.0
		if level.StopLossAtBase && level.StopLossMode == "exchange" {
			stopLoss = level.LevelPrice
		}

		// Mirror stop loss for Anomaly Monitor: SL = TP
		if level.Source == "anomaly-monitor" && level.TakeProfitMode == "exchange" && level.TakeProfitPct > 0 {
			if side == domain.SideLong {
				stopLoss = currPrice * (1 - level.TakeProfitPct)
			} else {
				stopLoss = currPrice * (1 + level.TakeProfitPct)
			}
			log.Printf("ANOMALY MONITOR: Setting mirror Stop Loss at %f (TP: %f%%)", stopLoss, level.TakeProfitPct*100)
		}

		takeProfit := 0.0
		if level.TakeProfitMode == "exchange" && level.TakeProfitPct > 0 {
			if side == domain.SideLong {
				takeProfit = currPrice * (1 + level.TakeProfitPct)
			} else {
				takeProfit = currPrice * (1 - level.TakeProfitPct)
			}
		}

		err := s.executor.Execute(ctx, level.Symbol, side, size, level.Leverage, level.MarginType, stopLoss, takeProfit)
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
			targetID := relevantLevel.ID
			if _, err := s.finalizePosition(ctx, symbol, "Safety Exit", targetID, price); err != nil {
				log.Printf("SAFETY: Failed to finalize position for %s: %v", symbol, err)
			} else {
				log.Printf("SAFETY: Closed position for %s", symbol)
			}
		}
	}
}

// ClosePosition manually closes a position for a symbol
func (s *LevelService) ClosePosition(ctx context.Context, symbol string) error {
	// Try to identify which level was likely active for this position
	levelID := "manual-close"
	s.mu.RLock()
	levels := s.levelsCache[symbol]
	s.mu.RUnlock()

	for _, l := range levels {
		state := s.engine.GetState(l.ID)
		// If ANY tier was triggered, this level is "active"
		if state.Tier1Triggered || state.Tier2Triggered || state.Tier3Triggered {
			levelID = l.ID
			break
		}
	}

	log.Printf("MANUAL CLOSE: Identified level %s for symbol %s", levelID, symbol)

	_, err := s.finalizePosition(ctx, symbol, "Manual Close", levelID, s.GetLatestPrice(symbol))
	return err
}

// CloseAllPositions closes all active positions on the exchange
func (s *LevelService) CloseAllPositions(ctx context.Context) error {
	positions, err := s.exchange.GetPositions(ctx)
	if err != nil {
		return err
	}
	for _, p := range positions {
		if p.Size > 0 {
			if err := s.ClosePosition(ctx, p.Symbol); err != nil {
				log.Printf("Failed to close position for %s: %v", p.Symbol, err)
			}
		}
	}
	return nil
}

// DeleteAllLevels removes all levels and updates cache
func (s *LevelService) DeleteAllLevels(ctx context.Context) error {
	if err := s.levelRepo.DeleteAllLevels(ctx); err != nil {
		return err
	}
	return s.UpdateCache(ctx)
}

// finalizePosition handles the common logic for closing a position, calculating PnL, and saving history.
func (s *LevelService) finalizePosition(ctx context.Context, symbol, reason, levelID string, price float64) (float64, error) {
	// 0. Invalidate Cache IMMEDIATELY to prevent race conditions from other ticks
	// We also set a "Size: 0" position in cache to block further processing while we are waiting for API.
	s.mu.Lock()
	s.positionCache[symbol] = &domain.Position{Symbol: symbol, Size: 0}
	s.positionTime[symbol] = time.Now()
	s.mu.Unlock()

	// 1. Fetch position details (should be from cache or most recent before we wiped it)
	// Actually, we need the position details BEFORE we wiped it for PnL calculation.
	// We'll pass them in or fetch once and store.
	pos, err := s.exchange.GetPosition(ctx, symbol)
	if err != nil || pos == nil || pos.Size == 0 {
		log.Printf("FINALIZE: Warning: No active position found for %s when closing (%s). Proceeding to ensure close.", symbol, reason)
	}

	// 2. Close on Exchange
	if err := s.exchange.ClosePosition(ctx, symbol); err != nil {
		log.Printf("FINALIZE: Failed to close position for %s: %v. Proceeding with state reset.", symbol, err)
		// We proceed to reset state to avoid getting stuck, assuming the position might be closed manually or liquidated.
	}

	// 3. Invalidate Cache again to be sure
	s.invalidatePositionCache(symbol)

	// 4. Reset State for all levels of this symbol
	s.mu.RLock()
	levels := s.levelsCache[symbol]
	s.mu.RUnlock()

	for _, l := range levels {
		// ResetState clears triggers and active side but PRESERVES ConsecutiveWins.
		// This is safe to call here as we want to reset the level for a fresh start after a position close.
		s.engine.ResetState(l.ID)

		// MANDATORY: Update LastTriggerTime to now to enforce cooldown after close
		// This prevents immediate re-entry if price is still in the trigger zone.
		s.engine.UpdateState(l.ID, func(ls *LevelState) {
			ls.LastTriggerTime = time.Now()
		})
	}

	// 5. Calculate PnL and Save History
	var realizedPnL float64
	var side domain.Side = "UNKNOWN"
	var size float64
	var entryPrice float64

	if pos != nil && pos.Size > 0 {
		side = pos.Side
		size = pos.Size
		entryPrice = pos.EntryPrice
		leverage := pos.Leverage
		marginType := pos.MarginType

		if side == domain.SideLong {
			realizedPnL = (price - entryPrice) * size
		} else {
			realizedPnL = (entryPrice - price) * size
		}

		log.Printf("FINALIZE: Symbol: %s, Side: %s, Size: %f, Entry: %f, Exit: %f, Calculated Realized PnL: %f",
			symbol, side, size, entryPrice, price, realizedPnL)

		// Find analysis data for history
		var analysisJSON string
		var source string
		s.mu.RLock()
		if levels, ok := s.levelsCache[symbol]; ok {
			for _, l := range levels {
				if l.ID == levelID {
					analysisJSON = l.AnalysisJSON
					source = l.Source
					break
				}
			}
		}
		s.mu.RUnlock()

		// Fetch OpenedAt from engine state
		state := s.engine.GetState(levelID)
		openedAt := state.OpenedAt
		if openedAt.IsZero() {
			openedAt = time.Now().Add(-1 * time.Minute) // Fallback
		}

		// Save Position History (Initial estimation)
		history := &domain.PositionHistory{
			Exchange:     pos.Exchange,
			Symbol:       pos.Symbol,
			Side:         side,
			Size:         size,
			EntryPrice:   entryPrice,
			ExitPrice:    price,
			RealizedPnL:  realizedPnL,
			Leverage:     leverage,
			MarginType:   marginType,
			LevelID:      levelID,
			AnalysisJSON: analysisJSON,
			Source:       source,
			OpenedAt:     openedAt,
			ClosedAt:     time.Now(),
		}

		historyID, err := s.tradeRepo.SavePositionHistory(ctx, history)
		if err != nil {
			log.Printf("Failed to save position history: %v", err)
		} else {
			// SYNC: Start a goroutine to fetch actual exchange data and update this record
			go func(hID int64, sym string, sde domain.Side) {
				time.Sleep(2 * time.Second) // Wait for exchange to record PnL
				// Use a fresh context for background sync
				bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				actualHistory, err := s.exchange.GetClosedPnL(bgCtx, sym, 5)
				if err == nil {
					for _, ah := range actualHistory {
						// Match by side and recent time
						if ah.Side == sde && time.Since(ah.ClosedAt) < 30*time.Second {
							ah.ID = hID // Set the ID for update
							if err := s.tradeRepo.UpdatePositionHistory(bgCtx, ah); err != nil {
								log.Printf("SYNC: Failed to update position history %d: %v", hID, err)
							} else {
								log.Printf("SYNC: Updated history %d with real exchange data (PnL: %f)", hID, ah.RealizedPnL)
							}
							break
						}
					}
				}
			}(historyID, symbol, side)
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

	// 7. Update Level State (Centralized Logic)
	// We only update state if we have a valid levelID
	if levelID != "" && levelID != "unknown" {
		// Find the activeLevel before entering the callback to avoid nested locking
		var activeLevel *domain.Level
		s.mu.RLock()
		if levels, ok := s.levelsCache[symbol]; ok {
			for _, l := range levels {
				if l.ID == levelID {
					activeLevel = l
					break
				}
			}
		}
		s.mu.RUnlock()

		s.engine.UpdateState(levelID, func(ls *LevelState) {
			// 1. Check for Base Close (Priority)
			isBaseClose := false
			if activeLevel != nil {
				// Use 0.2% tolerance to account for slippage/spread
				const epsilon = 0.002
				dist := (price - activeLevel.LevelPrice) / activeLevel.LevelPrice
				if dist < 0 {
					dist = -dist
				}
				if dist <= epsilon {
					isBaseClose = true
				}
			}

			if isBaseClose && (reason == "Stop Loss (Base)" || reason == "Level Cross" || reason == "Safety Exit") {
				ls.ConsecutiveBaseCloses++
				if realizedPnL > 0 {
					ls.ConsecutiveWins++
					log.Printf("AUDIT: Base Close (Win) recorded for Level %s. Consecutive Wins: %d", levelID, ls.ConsecutiveWins)
				} else {
					ls.ConsecutiveWins = 0 // Reset wins on loss
				}
				log.Printf("AUDIT: Base Close recorded for Level %s. Count: %d (PnL: %f)", levelID, ls.ConsecutiveBaseCloses, realizedPnL)

				if activeLevel != nil && activeLevel.MaxConsecutiveBaseCloses > 0 && ls.ConsecutiveBaseCloses >= activeLevel.MaxConsecutiveBaseCloses {
					ls.DisabledUntil = time.Now().Add(time.Duration(activeLevel.BaseCloseCooldownMs) * time.Millisecond)
					ls.ConsecutiveBaseCloses = 0
					log.Printf("AUDIT: Level %s disabled until %v due to max base closes.", activeLevel.ID, ls.DisabledUntil)

					// --- AUTO-LEVEL SPLIT LOGIC ---
					if activeLevel.AutoModeEnabled && ls.RangeHigh > 0 && ls.RangeLow > 0 {
						go func(oldLevel *domain.Level, high, low float64) {
							// Ensure high > low to avoid errors, though logic implies it
							if high > low {
								if err := s.SplitLevel(context.Background(), oldLevel, high, low); err != nil {
									log.Printf("AUTO-LEVEL: Failed to split level %s: %v", oldLevel.ID, err)
								}
							}
						}(activeLevel, ls.RangeHigh, ls.RangeLow)
					}
				}
			} else {
				// Not a Base Close
				ls.ConsecutiveBaseCloses = 0 // Reset base close streak
				ls.RangeHigh = 0
				ls.RangeLow = 0

				if realizedPnL > 0 {
					ls.ConsecutiveWins++
					log.Printf("AUDIT: Win recorded for Level %s. Consecutive Wins: %d", levelID, ls.ConsecutiveWins)
				} else {
					ls.ConsecutiveWins = 0
					log.Printf("AUDIT: Loss recorded for Level %s. Streak reset.", levelID)
				}
			}
		})
	}

	// 8. Close level if profit was achieved
	if realizedPnL > 0 && levelID != "" && levelID != "unknown" &&
		levelID != "sentiment-exit" && levelID != "safety-exit" {
		log.Printf("FINALIZE: Level %s achieved profit (%f). Deleting level.", levelID, realizedPnL)
		if err := s.levelRepo.DeleteLevel(ctx, levelID); err != nil {
			log.Printf("FINALIZE: Failed to delete level %s after profit: %v", levelID, err)
		} else {
			// Update cache to reflect deletion immediately
			s.UpdateCache(ctx)
		}
	}

	return realizedPnL, nil
}

// AutoCreateNextLevel attempts to find a better level based on liquidity and create it.
// It creates levels for the best Bid (Support) and/or best Ask (Resistance).
// Replaces all existing auto-levels for the symbol with up to 2 new levels based on best bid/ask liquidity.
// Handles overlap by prioritizing volume.
func (s *LevelService) AutoCreateNextLevel(ctx context.Context, oldLevelID string) error {
	// 1. Get Old Level to identify Symbol and Exchange
	s.mu.RLock()
	var oldLevel *domain.Level
	for _, levels := range s.levelsCache {
		for _, l := range levels {
			if l.ID == oldLevelID {
				oldLevel = l
				break
			}
		}
		if oldLevel != nil {
			break
		}
	}
	s.mu.RUnlock()

	if oldLevel == nil {
		return fmt.Errorf("level %s not found", oldLevelID)
	}

	// 2. Fetch Liquidity
	clusters, err := s.market.GetLiquidityClusters(ctx, oldLevel.Symbol)
	if err != nil {
		return fmt.Errorf("failed to fetch liquidity: %w", err)
	}

	// 3. Fetch Tiers (Moved up for distance calculation)
	tiers, err := s.levelRepo.GetSymbolTiers(ctx, oldLevel.Exchange, oldLevel.Symbol)
	if err != nil || tiers == nil {
		// Default tiers if not found
		tiers = &domain.SymbolTiers{Tier3Pct: 0.003} // Conservative default
	}

	// 4. Find Best Clusters (Bid and Ask)
	var bidCandidates []LiquidityCluster
	var askCandidates []LiquidityCluster
	minDistancePct := tiers.Tier3Pct

	for _, c := range clusters {
		dist := (c.Price - oldLevel.LevelPrice) / oldLevel.LevelPrice
		if dist < 0 {
			dist = -dist
		}
		if dist >= minDistancePct {
			if c.Type == "bid" {
				bidCandidates = append(bidCandidates, c)
			} else if c.Type == "ask" {
				askCandidates = append(askCandidates, c)
			}
		}
	}

	sort.Slice(bidCandidates, func(i, j int) bool {
		return bidCandidates[i].Volume > bidCandidates[j].Volume
	})
	sort.Slice(askCandidates, func(i, j int) bool {
		return askCandidates[i].Volume > askCandidates[j].Volume
	})

	// 5. Select Candidates
	var selected []LiquidityCluster

	var bestBid *LiquidityCluster
	if len(bidCandidates) > 0 {
		bestBid = &bidCandidates[0]
	}

	var bestAsk *LiquidityCluster
	if len(askCandidates) > 0 {
		bestAsk = &askCandidates[0]
	}

	// 6. Check Overlap & Apply Offset

	// Apply Offset Logic
	// Bid Level = Cluster + Tier3 (Buffer above support)
	// Ask Level = Cluster - Tier3 (Buffer below resistance)
	if bestBid != nil {
		originalPrice := bestBid.Price
		bestBid.Price = originalPrice * (1 + tiers.Tier3Pct)
		log.Printf("AUTO-LEVEL: Offset Bid Level: %f -> %f (Tier3: %f)", originalPrice, bestBid.Price, tiers.Tier3Pct)
	}
	if bestAsk != nil {
		originalPrice := bestAsk.Price
		bestAsk.Price = originalPrice * (1 - tiers.Tier3Pct)
		log.Printf("AUTO-LEVEL: Offset Ask Level: %f -> %f (Tier3: %f)", originalPrice, bestAsk.Price, tiers.Tier3Pct)
	}

	if bestBid != nil && bestAsk != nil {
		// Check if they are too close
		// Range < (Bid * Tier3 + Ask * Tier3) ?
		// Or simply: Ask - Bid < (Ask * Tier3) ?
		// Let's use the sum of their Tier 3 distances as the "forbidden zone".
		// Actually, if they are closer than 2x Tier3, the zones might overlap.
		// Let's be safe: if Ask < Bid * (1 + 2*tiers.Tier3Pct), they overlap.
		minAsk := bestBid.Price * (1 + 2*tiers.Tier3Pct)
		if bestAsk.Price < minAsk {
			log.Printf("AUTO-LEVEL: Overlap detected. Bid: %f, Ask: %f. Min Ask: %f. Prioritizing Volume.", bestBid.Price, bestAsk.Price, minAsk)
			if bestBid.Volume >= bestAsk.Volume {
				selected = append(selected, *bestBid)
			} else {
				selected = append(selected, *bestAsk)
			}
		} else {
			selected = append(selected, *bestBid, *bestAsk)
		}
	} else {
		if bestBid != nil {
			selected = append(selected, *bestBid)
		}
		if bestAsk != nil {
			selected = append(selected, *bestAsk)
		}
	}

	if len(selected) == 0 {
		return fmt.Errorf("no suitable candidates found")
	}

	// 6. Delete ALL Old Auto-Levels for this Symbol
	// We do this BEFORE creating new ones to ensure we stay within limits (though ID collision is unlikely).
	// Actually, we should find them first.
	s.mu.RLock()
	var levelsToDelete []string
	if levels, ok := s.levelsCache[oldLevel.Symbol]; ok {
		for _, l := range levels {
			if l.IsAuto {
				levelsToDelete = append(levelsToDelete, l.ID)
			}
		}
	}
	s.mu.RUnlock()

	for _, id := range levelsToDelete {
		if err := s.levelRepo.DeleteLevel(ctx, id); err != nil {
			log.Printf("AUTO-LEVEL: Failed to delete old auto level %s: %v", id, err)
		} else {
			log.Printf("AUTO-LEVEL: Deleted old auto level %s", id)
		}
	}

	// 7. Create New Levels
	for _, c := range selected {
		newLevel := &domain.Level{
			ID:                       fmt.Sprintf("%d", time.Now().UnixNano()),
			Exchange:                 oldLevel.Exchange,
			Symbol:                   oldLevel.Symbol,
			LevelPrice:               c.Price,
			BaseSize:                 oldLevel.BaseSize,
			Leverage:                 oldLevel.Leverage,
			MarginType:               oldLevel.MarginType,
			CoolDownMs:               oldLevel.CoolDownMs,
			StopLossAtBase:           oldLevel.StopLossAtBase,
			StopLossMode:             oldLevel.StopLossMode,
			DisableSpeedClose:        oldLevel.DisableSpeedClose,
			MaxConsecutiveBaseCloses: oldLevel.MaxConsecutiveBaseCloses,
			BaseCloseCooldownMs:      oldLevel.BaseCloseCooldownMs,
			TakeProfitPct:            oldLevel.TakeProfitPct,
			TakeProfitMode:           oldLevel.TakeProfitMode,
			Tier1Pct:                 oldLevel.Tier1Pct,
			Tier2Pct:                 oldLevel.Tier2Pct,
			Tier3Pct:                 oldLevel.Tier3Pct,
			IsAuto:                   true,
			AutoModeEnabled:          true,
			Source:                   "auto-next-" + c.Type,
			CreatedAt:                time.Now(),
		}
		// Ensure unique ID
		// Ensure unique ID using atomic counter or just high precision
		// Using a simple suffix to ensure uniqueness if called rapidly
		newLevel.ID = fmt.Sprintf("%d-%d", time.Now().UnixNano(), time.Now().UnixMicro()%1000)

		if err := s.levelRepo.SaveLevel(ctx, newLevel); err != nil {
			log.Printf("AUTO-LEVEL: Failed to save new %s level: %v", c.Type, err)
		} else {
			log.Printf("AUTO-LEVEL: Created new %s level %s at %f (Vol: %f)", c.Type, newLevel.ID, newLevel.LevelPrice, c.Volume)
		}
	}

	// Refresh cache
	s.UpdateCache(ctx)

	return nil
}

// CalculateLiquidityTP finds the best liquidity cluster to use as a Take Profit target.
// For Longs: Finds the biggest Ask cluster above entryPrice.
// For Shorts: Finds the biggest Bid cluster below entryPrice.
func (s *LevelService) CalculateLiquidityTP(ctx context.Context, symbol string, side domain.Side, entryPrice float64) (float64, error) {
	clusters, err := s.market.GetLiquidityClusters(ctx, symbol)
	if err != nil {
		return 0, err
	}

	var bestCluster *LiquidityCluster
	// We want to find a cluster that is at least some distance away to cover fees/profit.
	// Let's say min 0.5% profit.
	const minProfitPct = 0.005

	for _, c := range clusters {
		if side == domain.SideLong {
			// Look for Asks above entry
			if c.Type == "ask" && c.Price > entryPrice*(1+minProfitPct) {
				if bestCluster == nil || c.Volume > bestCluster.Volume {
					// We want the biggest wall
					// But maybe we also want the closest big wall?

					// Or the first big one?
					// Let's stick to "Highest Volume" as per spec.
					// But we should probably limit the range, e.g. within 5%
					if c.Price <= entryPrice*1.05 {
						current := c // copy loop var
						bestCluster = &current
					}
				}
			}
		} else {
			// Look for Bids below entry
			if c.Type == "bid" && c.Price < entryPrice*(1-minProfitPct) {
				if bestCluster == nil || c.Volume > bestCluster.Volume {
					if c.Price >= entryPrice*0.95 {
						current := c
						bestCluster = &current
					}
				}
			}
		}
	}

	if bestCluster == nil {
		return 0, fmt.Errorf("no suitable liquidity cluster found for TP")
	}

	// Apply small offset to exit BEFORE the wall
	// 0.1% offset
	const offsetPct = 0.001
	tpPrice := bestCluster.Price
	if side == domain.SideLong {
		tpPrice = tpPrice * (1 - offsetPct)
	} else {
		tpPrice = tpPrice * (1 + offsetPct)
	}

	return tpPrice, nil
}

// SplitLevel splits a level into two new levels based on the range, deleting the original and other auto levels.
func (s *LevelService) SplitLevel(ctx context.Context, originalLevel *domain.Level, high, low float64) error {
	// 1. Cleanup Old Auto Levels
	// We want to remove ALL existing auto levels for this symbol to ensure we only have the new ones.
	// We also remove the originalLevel (whether manual or auto) because it is being split.

	existingLevels, err := s.levelRepo.GetLevelsBySymbol(ctx, originalLevel.Symbol)
	if err != nil {
		return fmt.Errorf("failed to get levels for symbol %s: %w", originalLevel.Symbol, err)
	}

	for _, l := range existingLevels {
		// Delete if it's the original level OR if it's an auto level
		if l.ID == originalLevel.ID || l.IsAuto {
			if err := s.levelRepo.DeleteLevel(ctx, l.ID); err != nil {
				log.Printf("Warning: Failed to delete level %s during split cleanup: %v", l.ID, err)
				// Continue trying to delete others
			} else {
				log.Printf("SPLIT: Deleted level %s (IsAuto: %v)", l.ID, l.IsAuto)
			}
		}
	}

	// Create High Level
	highLevel := &domain.Level{
		ID:                       fmt.Sprintf("auto-split-%d-high", time.Now().UnixNano()),
		Exchange:                 originalLevel.Exchange,
		Symbol:                   originalLevel.Symbol,
		LevelPrice:               high,
		BaseSize:                 originalLevel.BaseSize,
		Leverage:                 originalLevel.Leverage,
		MarginType:               originalLevel.MarginType,
		CoolDownMs:               originalLevel.CoolDownMs,
		StopLossAtBase:           originalLevel.StopLossAtBase,
		StopLossMode:             originalLevel.StopLossMode,
		DisableSpeedClose:        originalLevel.DisableSpeedClose,
		MaxConsecutiveBaseCloses: originalLevel.MaxConsecutiveBaseCloses,
		BaseCloseCooldownMs:      originalLevel.BaseCloseCooldownMs,
		TakeProfitPct:            originalLevel.TakeProfitPct,
		TakeProfitMode:           originalLevel.TakeProfitMode,
		IsAuto:                   true,
		AutoModeEnabled:          true,
		Source:                   "auto-split",
		CreatedAt:                time.Now(),
	}

	// Create Low Level
	lowLevel := &domain.Level{
		ID:                       fmt.Sprintf("auto-split-%d-low", time.Now().UnixNano()),
		Exchange:                 originalLevel.Exchange,
		Symbol:                   originalLevel.Symbol,
		LevelPrice:               low,
		BaseSize:                 originalLevel.BaseSize,
		Leverage:                 originalLevel.Leverage,
		MarginType:               originalLevel.MarginType,
		CoolDownMs:               originalLevel.CoolDownMs,
		StopLossAtBase:           originalLevel.StopLossAtBase,
		StopLossMode:             originalLevel.StopLossMode,
		DisableSpeedClose:        originalLevel.DisableSpeedClose,
		MaxConsecutiveBaseCloses: originalLevel.MaxConsecutiveBaseCloses,
		BaseCloseCooldownMs:      originalLevel.BaseCloseCooldownMs,
		TakeProfitPct:            originalLevel.TakeProfitPct,
		TakeProfitMode:           originalLevel.TakeProfitMode,
		IsAuto:                   true,
		AutoModeEnabled:          true,
		Source:                   "auto-split",
		CreatedAt:                time.Now(),
	}

	// Save both levels
	if err := s.levelRepo.SaveLevel(ctx, highLevel); err != nil {
		return fmt.Errorf("failed to save high split level: %w", err)
	}
	if err := s.levelRepo.SaveLevel(ctx, lowLevel); err != nil {
		return fmt.Errorf("failed to save low split level: %w", err)
	}

	// Update cache
	return s.UpdateCache(ctx)
}

// IncrementBaseCloses manually increments the base close counter for a level
// and triggers the split logic if the max is reached.
func (s *LevelService) IncrementBaseCloses(ctx context.Context, levelID string) error {
	s.mu.RLock()
	// Find the level
	var targetLevel *domain.Level
	for _, levels := range s.levelsCache {
		for _, l := range levels {
			if l.ID == levelID {
				targetLevel = l
				break
			}
		}
		if targetLevel != nil {
			break
		}
	}
	s.mu.RUnlock()

	if targetLevel == nil {
		return fmt.Errorf("level not found: %s", levelID)
	}

	// Update State
	var currentCloses int
	var rangeHigh, rangeLow float64

	s.engine.UpdateState(levelID, func(ls *LevelState) {
		ls.ConsecutiveBaseCloses++
		currentCloses = ls.ConsecutiveBaseCloses
		rangeHigh = ls.RangeHigh
		rangeLow = ls.RangeLow

		// If range is empty (e.g. manually added level without ticks), set a default range around the level price
		if rangeHigh == 0 || rangeLow == 0 {
			rangeHigh = targetLevel.LevelPrice * 1.005 // +0.5%
			rangeLow = targetLevel.LevelPrice * 0.995  // -0.5%
			ls.RangeHigh = rangeHigh
			ls.RangeLow = rangeLow
		}
	})

	log.Printf("MANUAL: Incremented Base Closes for %s. Count: %d", levelID, currentCloses)

	// Check for Max Closes Trigger
	if targetLevel.MaxConsecutiveBaseCloses > 0 && currentCloses >= targetLevel.MaxConsecutiveBaseCloses {
		// Trigger Split Logic
		// We reuse the logic from finalizePosition, but here we call SplitLevel directly.

		// Disable the old level
		s.engine.UpdateState(levelID, func(ls *LevelState) {
			ls.DisabledUntil = time.Now().Add(time.Duration(targetLevel.BaseCloseCooldownMs) * time.Millisecond)
		})
		log.Printf("AUDIT: Level %s disabled until %v due to manual max base closes.", targetLevel.ID, time.Now().Add(time.Duration(targetLevel.BaseCloseCooldownMs)*time.Millisecond))

		// Split Logic
		if targetLevel.AutoModeEnabled { // Check AutoModeEnabled (which we enabled for manual levels too)
			go func(oldLevel *domain.Level, high, low float64) {
				if err := s.SplitLevel(context.Background(), oldLevel, high, low); err != nil {
					log.Printf("ERROR: Failed to split level %s: %v", oldLevel.ID, err)
				}
			}(targetLevel, rangeHigh, rangeLow)
		}
	}

	return nil
}

// RecordResearchMetrics logs current market stats for a symbol to the research file
func (s *LevelService) RecordResearchMetrics(ctx context.Context, symbol string, optStats *MarketStats, optAnalysis *LiquidityAnalysis) {
	stats := optStats
	if stats == nil {
		var err error
		stats, err = s.market.GetMarketStats(ctx, symbol)
		if err != nil {
			return
		}
	}

	analysis := optAnalysis
	if analysis == nil {
		analysis, _ = s.market.AnalyzeLiquidityImbalance(ctx, symbol, stats.LastPrice, 2.0, 5, 0.25)
	}

	history := &domain.PositionPnLHistory{
		Symbol:    symbol,
		Side:      "RESEARCH", // Marker side
		MarkPrice: stats.LastPrice,
		RSI:       stats.RSI,
		Timestamp: time.Now(),
	}

	if stats != nil {
		history.Volume60s = stats.SpeedBuy + stats.SpeedSell
		history.DepthBid = stats.DepthBid
		history.DepthAsk = stats.DepthAsk
		history.PriceChange60s = stats.PriceChange60s
		history.OBI = stats.OBI
		history.MACD = stats.MACD
		history.MACDSignal = stats.MACDSignal
		history.MACDHist = stats.MACDHist
		history.TSI = stats.TSI
	}

	if analysis != nil {
		history.LiquidityRatio = analysis.Ratio
		history.BidClusters = analysis.BidClusters
		history.AskClusters = analysis.AskClusters
		history.LiquidityWallPct = analysis.WallPct
		history.GLI = analysis.GLI
	}

	// Log to file for research
	s.logMetricsToFile(history)
}

// RecordActivePositionsPnL fetches all active positions and records their unrealized PnL to history.
func (s *LevelService) RecordActivePositionsPnL(ctx context.Context) {
	positions, err := s.exchange.GetPositions(ctx)
	if err != nil {
		log.Printf("ERROR: Failed to fetch positions for PnL history: %v", err)
		return
	}

	for _, pos := range positions {
		if pos.Size == 0 {
			continue
		}

		stats, _ := s.market.GetMarketStats(ctx, pos.Symbol)
		rsi := 0.0
		if stats != nil {
			rsi = stats.RSI
		} else {
			rsi, _ = s.market.GetRSI(ctx, pos.Symbol, "1", 14)
		}

		// Fetch Liquidity Analysis
		analysis, err := s.market.AnalyzeLiquidityImbalance(ctx, pos.Symbol, pos.MarkPrice, 2.0, 5, 0.25)

		s.RecordResearchMetrics(ctx, pos.Symbol, stats, analysis)

		history := &domain.PositionPnLHistory{
			Symbol:        pos.Symbol,
			Side:          pos.Side,
			Size:          pos.Size,
			EntryPrice:    pos.EntryPrice,
			MarkPrice:     pos.MarkPrice,
			UnrealizedPnL: pos.UnrealizedPnL,
			RSI:           rsi,
			Timestamp:     time.Now(),
		}

		if stats != nil {
			history.Volume60s = stats.SpeedBuy + stats.SpeedSell
			history.DepthBid = stats.DepthBid
			history.DepthAsk = stats.DepthAsk
			history.PriceChange60s = stats.PriceChange60s
			history.OBI = stats.OBI
			history.MACD = stats.MACD
			history.MACDSignal = stats.MACDSignal
			history.MACDHist = stats.MACDHist
			history.TSI = stats.TSI
		}

		if err == nil && analysis != nil {
			history.LiquidityRatio = analysis.Ratio
			history.BidClusters = analysis.BidClusters
			history.AskClusters = analysis.AskClusters
			history.LiquidityWallPct = analysis.WallPct
			history.GLI = analysis.GLI
		}

		if err := s.tradeRepo.SavePositionPnLHistory(ctx, history); err != nil {
			log.Printf("ERROR: Failed to save position PnL history for %s: %v", pos.Symbol, err)
		}
	}
}

func (s *LevelService) logMetricsToFile(h *domain.PositionPnLHistory) {
	// dir: logs/research/SYMBOL/YYYYMMDD-HH.log
	now := time.Now()
	dir := filepath.Join("logs", "research", h.Symbol)
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Printf("ERROR: Failed to create log directory %s: %v", dir, err)
		return
	}

	fileName := fmt.Sprintf("%s.log", now.Format("20060102-15"))
	path := filepath.Join(dir, fileName)

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("ERROR: Failed to open metric log file %s: %v", path, err)
		return
	}
	defer f.Close()

	// Format: Time | Symbol | Side | Price | RSI | Ratio | Clusters | Wall% | PnL | GLI | Vol60 | Depth B/A | Chg60 | OBI | MACD | TSI
	line := fmt.Sprintf("%s | %s | %s | %.6f | %.2f | %.2f | %dB:%dA | %.1f%% | %.4f | %.2f | %.1f | %.0f/%.0f | %.2f%% | %.3f | %.6f(%.6f) | %.2f\n",
		now.Format("15:04:05"),
		h.Symbol,
		h.Side,
		h.MarkPrice,
		h.RSI,
		h.LiquidityRatio,
		h.BidClusters,
		h.AskClusters,
		h.LiquidityWallPct,
		h.UnrealizedPnL,
		h.GLI,
		h.Volume60s,
		h.DepthBid,
		h.DepthAsk,
		h.PriceChange60s,
		h.OBI,
		h.MACD,
		h.MACDHist,
		h.TSI,
	)

	if _, err := f.WriteString(line); err != nil {
		log.Printf("ERROR: Failed to write to metric log file %s: %v", path, err)
	}
}

// GetExchangeHistory fetches the closed PnL history from the exchange, saves new entries to DB, and returns consolidated history.
func (s *LevelService) GetExchangeHistory(ctx context.Context, limit int) ([]*domain.PositionHistory, error) {
	// 1. Fetch from exchange (get a bit more than limit to ensure we sync enough)
	exchangeHistory, err := s.exchange.GetClosedPnL(ctx, "", 50)
	if err == nil {
		// 2. Save new entries to DB
		for _, h := range exchangeHistory {
			if err := s.tradeRepo.SaveExchangePositionHistory(ctx, h); err != nil {
				// We don't want to fail the whole request if one save fails (e.g. unique constraint)
				// But our SaveExchangePositionHistory uses ON CONFLICT DO NOTHING, so it should be fine.
				log.Printf("Warning: Failed to save exchange history item: %v", err)
			}
		}
	} else {
		log.Printf("Error: Failed to fetch exchange history: %v", err)
	}

	// 3. Return from DB
	return s.tradeRepo.ListExchangePositionHistory(ctx, limit)
}

func (s *LevelService) handleExternalCloseAsync(ctx context.Context, level *domain.Level) {
	// Retry loop to catch PnL record which might lag
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	timeout := time.After(45 * time.Second) // Try for 45 seconds

	for {
		select {
		case <-timeout:
			log.Printf("Async Cleanup Timeout for %s (Level %s). Providing state reset.", level.Symbol, level.ID)
			s.engine.UpdateState(level.ID, func(s *LevelState) {
				s.CleanupInProgress = false
			})
			s.engine.ResetState(level.ID)
			return
		case <-ticker.C:
			// Try to record
			if s.recordExternalClose(context.Background(), level) {
				// Success! Level is deleted inside recordExternalClose.
				// We don't need to reset state as level is gone.
				return
			}
		}
	}
}

func (s *LevelService) recordExternalClose(ctx context.Context, level *domain.Level) bool {
	// Try to fetch the very latest closed PnL item
	history, err := s.exchange.GetClosedPnL(ctx, level.Symbol, 1)
	if err != nil {
		// log.Printf("Failed to fetch closed PnL for %s during sync: %v", level.Symbol, err)
		return false
	}
	if len(history) == 0 {
		return false // No history found
	}

	latest := history[0]
	// Check recency: if closed more than 2 minutes ago, assume it's old news.
	if time.Since(latest.ClosedAt) > 2*time.Minute {
		// Found history but it's old. This means the new close hasn't appeared yet.
		return false
	}

	// Enrich with Level Info since Exchange doesn't know it
	latest.LevelID = level.ID
	latest.Source = "Exchange Trigger" // e.g. TP/SL on exchange
	latest.AnalysisJSON = level.AnalysisJSON

	// Save to Local History
	if _, err := s.tradeRepo.SavePositionHistory(ctx, latest); err != nil {
		// Log but don't fail, it might be a duplicate
		// log.Printf("Failed to save external history for %s: %v", level.Symbol, err)
	} else {
		log.Printf("Captured external close for %s (Level %s): PnL %f", level.Symbol, level.ID, latest.RealizedPnL)

		// DELETE LEVEL (External Close = Used)
		if err := s.levelRepo.DeleteLevel(ctx, level.ID); err != nil {
			log.Printf("Sync: Failed to delete level %s after external close: %v", level.ID, err)
		} else {
			log.Printf("AUTO-DELETE: Deleted level %s after external close", level.ID)
			s.UpdateCache(ctx)
		}
		return true // SUCCESS
	}
	return true // Saved or duplicate, accept as success
}
