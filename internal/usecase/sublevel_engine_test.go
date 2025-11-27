package usecase_test

import (
	"testing"

	"github.com/vitos/crypto_trade_level/internal/domain"
	"github.com/vitos/crypto_trade_level/internal/usecase"
)

func TestSublevelEngine_Evaluate(t *testing.T) {
	engine := usecase.NewSublevelEngine()

	level := &domain.Level{
		ID:         "1",
		LevelPrice: 10000.0,
		BaseSize:   1.0,
		CoolDownMs: 0, // No cooldown for logic test
	}

	// Tiers: 0.5%, 0.3%, 0.15%
	// LONG zone (Price > Level): Tiers ABOVE level
	// Level at 10000, boundaries: 10050, 10030, 10015
	boundaries := []float64{10050.0, 10030.0, 10015.0}

	// 0. Initialize state with price OUTSIDE tiers (10100)
	engine.Evaluate(level, boundaries, 10200.0, 10100.0, domain.SideLong)

	// 1. Price falls from 10100 to 10040, crossing Tier1 (10050) downward (Defense Trigger)
	// This triggers Tier1 (ActionOpen)
	action, size := engine.Evaluate(level, boundaries, 10100.0, 10040.0, domain.SideLong)
	if action != usecase.ActionOpen || size != 1.0 {
		t.Errorf("Expected Tier1 trigger (Open, 1.0), got (%v, %f)", action, size)
	}

	// 2. Price continues at 10040
	// No trigger (not crossing any tier)
	action, _ = engine.Evaluate(level, boundaries, 10040.0, 10040.0, domain.SideLong)
	if action != usecase.ActionNone {
		t.Errorf("Expected No Action, got %v", action)
	}

	// 3. Price falls from 10040 to 10020, crossing Tier2 (10030) downward
	// This triggers Tier2 (ActionAddToPosition, size 1.0)
	action, size = engine.Evaluate(level, boundaries, 10040.0, 10020.0, domain.SideLong)
	if action != usecase.ActionAddToPosition || size != 1.0 {
		t.Errorf("Expected Tier2 trigger (AddToPosition, 1.0), got (%v, %f)", action, size)
	}

	// 4. Reset Engine to test Bidirectional Trigger (Trend/Breakout)
	engine.ResetState(level.ID)

	// Start INSIDE (between Level and Tier 1)
	// Level 10000. Tier 1 10050. Price 10020.
	engine.Evaluate(level, boundaries, 10010.0, 10020.0, domain.SideLong)

	// 5. Price rises from 10020 to 10060, crossing Tier 1 (10050) UPWARD (Trend Trigger)
	// NOTE: Bidirectional triggers disabled in favor of Defensive Only (Approaching Level).
	// So this should NOT trigger.
	action, size = engine.Evaluate(level, boundaries, 10020.0, 10060.0, domain.SideLong)
	if action != usecase.ActionNone {
		t.Errorf("Expected No Action (Defensive Only), got (%v, %f)", action, size)
	}
}

func TestSublevelEngine_Cooldown(t *testing.T) {
	engine := usecase.NewSublevelEngine()
	level := &domain.Level{ID: "1", BaseSize: 1.0, CoolDownMs: 5000} // 5s cooldown
	boundaries := []float64{10050.0, 10030.0, 10015.0}

	// 0. Init outside
	engine.Evaluate(level, boundaries, 10200.0, 10100.0, domain.SideLong)

	// Trigger Tier1 (price falls from 10100 to 10040)
	engine.Evaluate(level, boundaries, 10100.0, 10040.0, domain.SideLong)

	// Try to trigger Tier2 immediately (price falls from 10040 to 10020, crossing Tier2)
	// Should be blocked by cooldown
	action, _ := engine.Evaluate(level, boundaries, 10040.0, 10020.0, domain.SideLong)
	if action != usecase.ActionNone {
		t.Errorf("Expected Cooldown (None), got %v", action)
	}
}

// TestSublevelEngine_StopLossAtBase removed as logic moved to LevelService
// TestSublevelEngine_StopLossAtBase_Stateless removed as logic moved to LevelService
