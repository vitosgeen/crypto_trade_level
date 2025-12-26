package usecase

import (
	"context"
	"fmt"
	"log"
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

// CreateLevel creates a new level
func (s *LevelService) CreateLevel(ctx context.Context, level *domain.Level) error {

	// 2. Save Level
	if err := s.levelRepo.SaveLevel(ctx, level); err != nil {
		return fmt.Errorf("failed to save level: %w", err)
	}

	// 3. Update Cache
	return s.UpdateCache(ctx)
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
		// Update Range High/Low
		s.engine.UpdateState(l.ID, func(ls *LevelState) {
			if ls.RangeHigh == 0 || price > ls.RangeHigh {
				ls.RangeHigh = price
			}
			if ls.RangeLow == 0 || price < ls.RangeLow {
				ls.RangeLow = price
			}
		})
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
			// Check TP
			if activeLevel.TakeProfitPct > 0 || activeLevel.TakeProfitMode == "liquidity" || activeLevel.TakeProfitMode == "sentiment" {
				shouldTP := false
				var tpPrice float64

				if activeLevel.TakeProfitMode == "liquidity" {
					// Dynamic TP based on liquidity
					dynamicTP, err := s.CalculateLiquidityTP(ctx, symbol, pos.Side, pos.EntryPrice)
					if err == nil && dynamicTP > 0 {
						tpPrice = dynamicTP
					} else {
						// Fallback to fixed % if liquidity TP fails
						if pos.Side == domain.SideLong {
							tpPrice = pos.EntryPrice * (1 + activeLevel.TakeProfitPct)
						} else {
							tpPrice = pos.EntryPrice * (1 - activeLevel.TakeProfitPct)
						}
					}
				} else if activeLevel.TakeProfitMode == "sentiment" {
					// Sentiment-Adjusted TP
					// TargetTP = BaseTP * (1 + (ConclusionScore * Factor))
					// Factor = 0.5 (Adjustable? Hardcoded for now per plan)
					baseTP := activeLevel.TakeProfitPct
					if baseTP <= 0 {
						baseTP = 0.02 // Default 2% if not set
					}

					stats, err := s.market.GetMarketStats(ctx, symbol)
					score := 0.0
					if err == nil && stats != nil {
						score = stats.ConclusionScore
					}

					// Adjust TP
					// If Long: Positive Score (Bullish) -> Increase TP. Negative Score (Bearish) -> Decrease TP.
					// If Short: Negative Score (Bearish) -> Increase TP. Positive Score (Bullish) -> Decrease TP.
					// Wait, Score is -1 (Bear) to 1 (Bull).
					// For Long: Multiplier = 1 + (Score * 0.5)
					//   Score 0.8 -> 1 + 0.4 = 1.4x TP.
					//   Score -0.5 -> 1 - 0.25 = 0.75x TP.
					// For Short: We want to INCREASE TP if Bearish (Score < 0).
					//   Score -0.8 -> We want larger TP.
					//   Multiplier = 1 - (Score * 0.5)
					//   Score -0.8 -> 1 - (-0.4) = 1.4x TP.
					//   Score 0.5 -> 1 - 0.25 = 0.75x TP.

					factor := 0.5
					multiplier := 1.0
					if pos.Side == domain.SideLong {
						multiplier = 1 + (score * factor)
					} else {
						multiplier = 1 - (score * factor)
					}

					// Clamp multiplier to avoid negative or too small TP?
					// If score is extreme, e.g. -1. Multiplier = 0.5. TP becomes half.
					// Seems safe.

					adjustedPct := baseTP * multiplier
					if adjustedPct < 0.001 {
						adjustedPct = 0.001 // Minimum 0.1% TP
					}

					if pos.Side == domain.SideLong {
						tpPrice = pos.EntryPrice * (1 + adjustedPct)
					} else {
						tpPrice = pos.EntryPrice * (1 - adjustedPct)
					}

				} else {
					// Fixed TP
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
					// State update is now handled in finalizePosition
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
					if _, err := s.finalizePosition(ctx, symbol, "Stop Loss (Base)", activeLevel.ID, price); err != nil {
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
		// ResetState clears triggers and active side but PRESERVES ConsecutiveWins.
		// This is safe to call here as we want to reset the level for a fresh start after a position close.
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
