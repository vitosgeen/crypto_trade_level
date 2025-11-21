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
	// Short Side: 10050, 10030, 10015
	boundaries := []float64{10050.0, 10030.0, 10015.0}

	// 1. Price comes from above 10050 to 10040 (Crosses Tier1)
	// Should trigger Tier1
	action, size := engine.Evaluate(level, boundaries, 10060.0, 10040.0, domain.SideLong)
	if action != usecase.ActionOpen || size != 1.0 {
		t.Errorf("Expected Tier1 trigger (Open, 1.0), got (%v, %f)", action, size)
	}

	// 2. Price stays at 10040
	// Should NOT trigger again
	action, _ = engine.Evaluate(level, boundaries, 10040.0, 10040.0, domain.SideLong)
	if action != usecase.ActionNone {
		t.Errorf("Expected No Action, got %v", action)
	}

	// 3. Price moves to 10025 (Crosses Tier2: 10030)
	// Should trigger Tier2 (Add 1.0 to double to 2.0)
	action, size = engine.Evaluate(level, boundaries, 10040.0, 10025.0, domain.SideLong)
	if action != usecase.ActionAddToPosition || size != 1.0 {
		t.Errorf("Expected Tier2 trigger (AddToPosition, 1.0), got (%v, %f)", action, size)
	}

	// 4. Price moves to 10010 (Crosses Tier3: 10015)
	// Should trigger Tier3 (Add 2.0 to double to 4.0)
	action, size = engine.Evaluate(level, boundaries, 10025.0, 10010.0, domain.SideLong)
	if action != usecase.ActionAddToPosition || size != 2.0 {
		t.Errorf("Expected Tier3 trigger (AddToPosition, 2.0), got (%v, %f)", action, size)
	}
}

func TestSublevelEngine_Cooldown(t *testing.T) {
	engine := usecase.NewSublevelEngine()
	level := &domain.Level{ID: "1", BaseSize: 1.0, CoolDownMs: 5000} // 5s cooldown
	boundaries := []float64{10050.0, 10030.0, 10015.0}

	// Trigger Tier1
	engine.Evaluate(level, boundaries, 10060.0, 10040.0, domain.SideLong)

	// Try Trigger Tier2 immediately
	action, _ := engine.Evaluate(level, boundaries, 10040.0, 10025.0, domain.SideLong)
	if action != usecase.ActionNone {
		t.Errorf("Expected Cooldown (None), got %v", action)
	}

	// Wait for cooldown (mocking time would be better, but for simple logic check:)
	// In real impl we inject time provider. For now, let's just check if logic respects LastTriggerTime.
	// We will rely on the implementation to check time.Since(LastTrigger)
}
