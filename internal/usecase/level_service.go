package usecase

import (
	"context"
	"fmt"
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
	evaluator *LevelEvaluator
	engine    *SublevelEngine
	executor  *TradeExecutor

	mu         sync.RWMutex
	lastPrices map[string]float64 // symbol -> price
}

func NewLevelService(
	levelRepo domain.LevelRepository,
	tradeRepo domain.TradeRepository,
	exchange domain.Exchange,
) *LevelService {
	return &LevelService{
		levelRepo:  levelRepo,
		tradeRepo:  tradeRepo,
		exchange:   exchange,
		evaluator:  NewLevelEvaluator(),
		engine:     NewSublevelEngine(),
		executor:   NewTradeExecutor(exchange),
		lastPrices: make(map[string]float64),
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

// ProcessTick should be called when a new price arrives (e.g. from WebSocket).
func (s *LevelService) ProcessTick(ctx context.Context, exchangeName, symbol string, price float64) error {
	fmt.Printf("Tick: %s %f\n", symbol, price) // Simple print for debugging
	s.mu.Lock()
	prevPrice, ok := s.lastPrices[symbol]
	s.lastPrices[symbol] = price
	s.mu.Unlock()

	if !ok {
		// First tick, can't detect crossing
		return nil
	}

	// Fetch levels for this symbol
	// In a real app, we might cache these in memory and refresh periodically
	levels, err := s.levelRepo.ListLevels(ctx)
	if err != nil {
		return err
	}

	// Filter for this symbol/exchange
	// Optimization: Cache levels by symbol
	var relevantLevels []*domain.Level
	for _, l := range levels {
		if l.Symbol == symbol && l.Exchange == exchangeName {
			relevantLevels = append(relevantLevels, l)
		}
	}

	if len(relevantLevels) == 0 {
		return nil
	}

	tiers, err := s.levelRepo.GetSymbolTiers(ctx, exchangeName, symbol)
	if err != nil {
		return nil // No tiers, can't trade
	}

	for _, level := range relevantLevels {
		s.processLevel(ctx, level, tiers, prevPrice, price)
	}

	return nil
}

func (s *LevelService) processLevel(ctx context.Context, level *domain.Level, tiers *domain.SymbolTiers, prevPrice, currPrice float64) {
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
		log.Printf("AUDIT: Action Triggered: %s. Level: %s, Symbol: %s, Side: %s, Size: %f", action, level.ID, level.Symbol, side, size)
		log.Printf("Triggered: %s on %s %s (Side: %s, Size: %f)", action, level.Exchange, level.Symbol, side, size)

		if action == ActionClose {
			// Close Position
			err := s.exchange.ClosePosition(ctx, level.Symbol)
			if err != nil {
				log.Printf("WARNING: Failed to close position for %s (might be already closed): %v", level.Symbol, err)
			}

			// Use the ActiveSide from state for the Close record, as 'side' might be flipped (e.g. crossing level)
			state := s.engine.GetState(level.ID)
			closingSide := state.ActiveSide
			if closingSide == "" {
				closingSide = side // Fallback
			}

			// Reset State
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
		if level.StopLossAtBase {
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
