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
	ActionClose         Action = "CLOSE"
)

type LevelState struct {
	Tier1Triggered        bool
	Tier2Triggered        bool
	Tier3Triggered        bool
	LastTriggerTime       time.Time
	ActiveSide            domain.Side
	ConsecutiveWins       int       // Tracks consecutive profitable closes
	ConsecutiveBaseCloses int       // Tracks consecutive closes at base level
	DisabledUntil         time.Time // Timestamp until which the level is disabled
	RangeHigh             float64   // Highest price observed during active period
	RangeLow              float64   // Lowest price observed during active period
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

func (e *SublevelEngine) GetState(levelID string) LevelState {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if s, ok := e.states[levelID]; ok {
		return *s
	}
	return LevelState{}
}

// UpdateState allows external services to update the state (e.g. recording a win/loss)
func (e *SublevelEngine) UpdateState(levelID string, updateFn func(*LevelState)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	s, ok := e.states[levelID]
	if !ok {
		s = &LevelState{}
		e.states[levelID] = s
	}
	updateFn(s)
}

func (e *SublevelEngine) ResetState(levelID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	// We do NOT delete the state entirely, because we need to persist ConsecutiveWins.
	// Instead, we reset the triggers and active side.
	if s, ok := e.states[levelID]; ok {
		s.Tier1Triggered = false
		s.Tier2Triggered = false
		s.Tier3Triggered = false
		s.ActiveSide = ""
		// ConsecutiveWins is preserved
		// ConsecutiveBaseCloses is preserved
		// DisabledUntil is preserved
	} else {
		// If state doesn't exist, no need to do anything (or create empty?)
		delete(e.states, levelID)
	}
}

// Evaluate checks if price movement triggers a tier action.
// boundaries: [Tier1, Tier2, Tier3] prices.
func (e *SublevelEngine) Evaluate(level *domain.Level, boundaries []float64, prevPrice, currPrice float64, side domain.Side) (Action, float64) {
	e.mu.Lock()
	state, ok := e.states[level.ID]
	isNewState := !ok
	if !ok {
		state = &LevelState{}
		e.states[level.ID] = state
	}
	e.mu.Unlock()

	// Check Cooldown
	if !state.LastTriggerTime.IsZero() && time.Since(state.LastTriggerTime) < time.Duration(level.CoolDownMs)*time.Millisecond {
		// log.Printf("DEBUG: Level %s in cooldown. Remaining: %v", level.ID, time.Duration(level.CoolDownMs)*time.Millisecond-time.Since(state.LastTriggerTime))
		return ActionNone, 0
	}

	// Check Base Close Cooldown
	if !state.DisabledUntil.IsZero() && time.Now().Before(state.DisabledUntil) {
		// log.Printf("DEBUG: Level %s disabled until %v", level.ID, state.DisabledUntil)
		return ActionNone, 0
	}

	// Determine Trigger Logic based on Side
	// Triggers are now BIDIRECTIONAL per updated spec.
	// Long: Trigger on Cross Down (Dip) OR Cross Up (Breakout/Trend)
	// Short: Trigger on Cross Up (Rally) OR Cross Down (Breakdown/Trend)

	tier1Price, tier2Price, tier3Price := boundaries[0], boundaries[1], boundaries[2]

	// CRITICAL FIX: On first evaluation, mark already-passed tiers as triggered
	// to avoid false triggers on old price levels.
	// Since triggers are bidirectional, "passed" means "between Level and Tier" vs "outside Tier"?
	// Actually, if we trigger on ANY cross, we just need to know if we are currently "past" the tier relative to the level?
	// No, "Triggered" state prevents re-triggering.
	// Initialization logic: If we start "inside" the position (e.g. between Level and Tier 1), should we mark Tier 1 as triggered?
	// If we start at L+0.2% (Tier 1 is 0.5%), we are "inside".
	// If price moves to L+0.6%, we cross Tier 1 Upwards. Should we trigger? Yes.
	// If price moves to L, we close.

	// If we start at L+0.6% (Outside Tier 1).
	// If price moves to L+0.4%, we cross Tier 1 Downwards. Should we trigger? Yes.

	// So, initialization logic is tricky with bidirectional triggers.
	// Maybe we don't need special initialization if we just rely on "Cross"?
	// But if we restart the bot with an open position, we don't want to double-buy?
	// The bot doesn't know about external positions yet (stateless engine).
	// The `isNewState` block was to prevent immediate triggers on startup if price is *already* past the tier.
	// But if triggers are bidirectional, "past" is ambiguous.

	// Let's assume "Triggered" means "We have executed this tier".
	// If `isNewState` is true, we might want to assume no tiers are triggered unless we are DEEP in profit?
	// Or maybe we just leave it empty and let the first cross trigger?
	// The previous logic assumed "Approaching Level" was the only trigger.
	// Now that "Moving Away" is also a trigger, ANY cross is valid.
	// So, if we start at 10000 (Level), and Tier 1 is 10050.
	// Price is 10020. We are "Inside".
	// If price goes to 10060, we cross Tier 1. Trigger.
	// If price goes to 10000, we hit Stop Loss.

	// If we start at 10060 (Outside).
	// Price goes to 10040. Cross Tier 1. Trigger.

	// So, `isNewState` logic might be unnecessary or should be minimal?
	// The original issue was: Start at 10040. Prev=10040, Curr=10040. No cross.
	// Next tick 10030. Cross? No.
	// Wait, `Evaluate` takes `prevPrice`.
	// On first run, `prevPrice` might be 0 or same as `currPrice`?
	// The caller `LevelService` passes `prevPrice`.
	// If it's the first tick, `prevPrice` might be the same as `currPrice`.

	// Let's keep the initialization simple: Do NOT pre-trigger tiers.
	// Let the market action trigger them.
	// UNLESS we are strictly recovering state?
	// For now, I will COMMENT OUT the aggressive initialization to allow bidirectional triggers to work naturally.
	// If the user wants to "resume" a position, that's a separate state persistence issue.

	if isNewState {
		// Reset/Clear triggers on new state? They are already false by default.
		// We log the start.
		log.Printf("INFO: Level %s engine initialized. Price: %f", level.ID, currPrice)
	}

	triggered := false
	action := ActionNone
	size := 0.0

	e.mu.Lock()
	defer e.mu.Unlock()

	// Helper to check crossing with direction
	// Short (Resistance): Trigger on Rise (Prev < Boundary <= Curr)
	// Long (Support): Trigger on Fall (Prev > Boundary >= Curr)
	crossesUp := func(p1, p2, boundary float64) bool {
		return p1 < boundary && p2 >= boundary
	}
	crossesDown := func(p1, p2, boundary float64) bool {
		return p1 > boundary && p2 <= boundary
	}

	// Calculate Multiplier based on Consecutive Wins
	multiplier := 1.0
	if state.ConsecutiveWins > 0 {
		multiplier = 2.0
		// Cap at 2x for now as per "Profit Doubling" spec (implied single step up)
		// Or should it be 2^wins? Spec says "from 1x to 2x". Let's stick to 2x max for safety.
	}

	if side == domain.SideShort {
		// Short (Resistance): Tiers are BELOW Level
		// EXT Spec: "SHORT side (below level) ... Tier1_below = L * (1 - t1)"
		// YES. Short Zone is Price < Level. Tiers are < Level.
		// We want to Short when Price RISES to the Tier (Resistance).
		// So we check crossesUp.

		// Debug Log
		// log.Printf("DEBUG: Eval Short. Level: %s. Prev: %f, Curr: %f, T1: %f, T2: %f, T3: %f", level.ID, prevPrice, currPrice, tier1Price, tier2Price, tier3Price)

		// Tier 1
		// Trigger if crossed UP OR currently INSIDE the zone (between Tier 1 and Level)
		if !state.Tier1Triggered && (crossesUp(prevPrice, currPrice, tier1Price) || (currPrice >= tier1Price && currPrice < level.LevelPrice)) {
			log.Printf("AUDIT: Tier 1 Triggered (Short). Level %s. Price %f -> %f. Boundary: %f. Wins: %d. Mult: %f", level.ID, prevPrice, currPrice, tier1Price, state.ConsecutiveWins, multiplier)
			state.Tier1Triggered = true
			state.ActiveSide = domain.SideShort
			triggered = true
			action = ActionOpen
			size = level.BaseSize * multiplier
		} else if !state.Tier2Triggered && crossesUp(prevPrice, currPrice, tier2Price) {
			// Tier 2
			log.Printf("AUDIT: Tier 2 Triggered (Short). Level %s. Price %f -> %f. Boundary: %f", level.ID, prevPrice, currPrice, tier2Price)
			state.Tier2Triggered = true
			triggered = true
			action = ActionAddToPosition
			size = level.BaseSize // Additions are usually base size? Or scaled? Spec implies initial entry scaling. Keeping additions flat for now to manage risk.
		} else if !state.Tier3Triggered && crossesUp(prevPrice, currPrice, tier3Price) {
			// Tier 3
			log.Printf("AUDIT: Tier 3 Triggered (Short). Level %s. Price %f -> %f. Boundary: %f", level.ID, prevPrice, currPrice, tier3Price)
			state.Tier3Triggered = true
			triggered = true
			action = ActionAddToPosition
			size = 2 * level.BaseSize
		}
	} else {
		// Long (Support): Tiers are ABOVE Level.
		// EXT Spec: "LONG side (above level) ... Tier1_above = L * (1 + t1)"
		// YES. Long Zone is Price > Level. Tiers are > Level.
		// We want to Long when Price FALLS to the Tier (Support).
		// So we check crossesDown.

		// Debug Log
		// log.Printf("DEBUG: Eval Long. Level: %s. Prev: %f, Curr: %f, T1: %f, T2: %f, T3: %f", level.ID, prevPrice, currPrice, tier1Price, tier2Price, tier3Price)

		// Tier 1
		// Trigger if crossed DOWN OR currently INSIDE the zone (between Tier 1 and Level)
		if !state.Tier1Triggered && (crossesDown(prevPrice, currPrice, tier1Price) || (currPrice <= tier1Price && currPrice > level.LevelPrice)) {
			log.Printf("AUDIT: Tier 1 Triggered (Long). Level %s. Price %f -> %f. Boundary: %f. Wins: %d. Mult: %f", level.ID, prevPrice, currPrice, tier1Price, state.ConsecutiveWins, multiplier)
			state.Tier1Triggered = true
			state.ActiveSide = domain.SideLong
			triggered = true
			action = ActionOpen
			size = level.BaseSize * multiplier
		} else if !state.Tier2Triggered && crossesDown(prevPrice, currPrice, tier2Price) {
			log.Printf("AUDIT: Tier 2 Triggered (Long). Level %s. Price %f -> %f. Boundary: %f", level.ID, prevPrice, currPrice, tier2Price)
			state.Tier2Triggered = true
			triggered = true
			action = ActionAddToPosition
			size = level.BaseSize
		} else if !state.Tier3Triggered && crossesDown(prevPrice, currPrice, tier3Price) {
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
