package usecase

import (
	"log"
	"sync"
	"time"

	"github.com/vitos/crypto_trade_level/internal/domain"
)

type Action string

const (
	ActionNone          Action = "NONE"
	ActionOpen          Action = "OPEN"
	ActionAddToPosition Action = "ADD"
)

type LevelState struct {
	Tier1Triggered  bool
	Tier2Triggered  bool
	Tier3Triggered  bool
	LastTriggerTime time.Time
}

type SublevelEngine struct {
	states map[string]*LevelState
	mu     sync.RWMutex
}

func NewSublevelEngine() *SublevelEngine {
	return &SublevelEngine{
		states: make(map[string]*LevelState),
	}
}

func (e *SublevelEngine) GetState(levelID string) *LevelState {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if s, ok := e.states[levelID]; ok {
		return s
	}
	return &LevelState{}
}

func (e *SublevelEngine) ResetState(levelID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.states, levelID)
}

// Evaluate checks if price movement triggers a tier action.
// boundaries: [Tier1, Tier2, Tier3] prices.
// Evaluate checks if price movement triggers a tier action.
// boundaries: [Tier1, Tier2, Tier3] prices.
func (e *SublevelEngine) Evaluate(level *domain.Level, boundaries []float64, prevPrice, currPrice float64, side domain.Side) (Action, float64) {
	e.mu.Lock()
	state, ok := e.states[level.ID]
	if !ok {
		state = &LevelState{}
		e.states[level.ID] = state
	}
	e.mu.Unlock()

	// Check Cooldown
	if !state.LastTriggerTime.IsZero() && time.Since(state.LastTriggerTime) < time.Duration(level.CoolDownMs)*time.Millisecond {
		// log.Printf("DEBUG: Level %s in cooldown. Remaining: %v", level.ID, time.Duration(level.CoolDownMs)*time.Millisecond - time.Since(state.LastTriggerTime))
		return ActionNone, 0
	}

	// Determine Trigger Logic based on Side
	// Short: Price comes from ABOVE. Trigger if prev > Tier >= curr
	// Long: Price comes from BELOW. Trigger if prev < Tier <= curr

	tier1Price, tier2Price, tier3Price := boundaries[0], boundaries[1], boundaries[2]

	triggered := false
	action := ActionNone
	size := 0.0

	e.mu.Lock()
	defer e.mu.Unlock()

	if side == domain.SideShort {
		// Short (Resistance): Trigger when price rises UP to the tier
		// Tier prices are ABOVE level (calculated as Level * (1+pct))
		// We trigger when we cross UP into the tier

		// Debug Log
		// log.Printf("DEBUG: Eval Short. Prev: %f, Curr: %f, T1: %f, T2: %f, T3: %f", prevPrice, currPrice, tier1Price, tier2Price, tier3Price)

		// Tier 1
		if !state.Tier1Triggered && prevPrice < tier1Price && currPrice >= tier1Price {
			log.Printf("AUDIT: Tier 1 Triggered (Short). Level %s. Price %f -> %f. Boundary: %f", level.ID, prevPrice, currPrice, tier1Price)
			state.Tier1Triggered = true
			triggered = true
			action = ActionOpen
			size = level.BaseSize
		} else if !state.Tier2Triggered && prevPrice < tier2Price && currPrice >= tier2Price {
			// Tier 2
			log.Printf("AUDIT: Tier 2 Triggered (Short). Level %s. Price %f -> %f. Boundary: %f", level.ID, prevPrice, currPrice, tier2Price)
			state.Tier2Triggered = true
			triggered = true
			action = ActionAddToPosition
			size = level.BaseSize
		} else if !state.Tier3Triggered && prevPrice < tier3Price && currPrice >= tier3Price {
			// Tier 3
			log.Printf("AUDIT: Tier 3 Triggered (Short). Level %s. Price %f -> %f. Boundary: %f", level.ID, prevPrice, currPrice, tier3Price)
			state.Tier3Triggered = true
			triggered = true
			action = ActionAddToPosition
			size = 2 * level.BaseSize
		}
	} else {
		// Long (Support): Trigger when price falls DOWN to the tier
		// Tier prices are BELOW level (calculated as Level * (1-pct))
		// We trigger when we cross DOWN into the tier

		// Debug Log
		// log.Printf("DEBUG: Eval Long. Prev: %f, Curr: %f, T1: %f, T2: %f, T3: %f", prevPrice, currPrice, tier1Price, tier2Price, tier3Price)

		if !state.Tier1Triggered && prevPrice > tier1Price && currPrice <= tier1Price {
			log.Printf("AUDIT: Tier 1 Triggered (Long). Level %s. Price %f -> %f. Boundary: %f", level.ID, prevPrice, currPrice, tier1Price)
			state.Tier1Triggered = true
			triggered = true
			action = ActionOpen
			size = level.BaseSize
		} else if !state.Tier2Triggered && prevPrice > tier2Price && currPrice <= tier2Price {
			log.Printf("AUDIT: Tier 2 Triggered (Long). Level %s. Price %f -> %f. Boundary: %f", level.ID, prevPrice, currPrice, tier2Price)
			state.Tier2Triggered = true
			triggered = true
			action = ActionAddToPosition
			size = level.BaseSize
		} else if !state.Tier3Triggered && prevPrice > tier3Price && currPrice <= tier3Price {
			log.Printf("AUDIT: Tier 3 Triggered (Long). Level %s. Price %f -> %f. Boundary: %f", level.ID, prevPrice, currPrice, tier3Price)
			state.Tier3Triggered = true
			triggered = true
			action = ActionAddToPosition
			size = 2 * level.BaseSize
		}
	}

	if triggered {
		state.LastTriggerTime = time.Now()
		return action, size
	}

	return ActionNone, 0
}
