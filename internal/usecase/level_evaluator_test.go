package usecase_test

import (
	"testing"

	"github.com/vitos/crypto_trade_level/internal/domain"
	"github.com/vitos/crypto_trade_level/internal/usecase"
)

func TestDetermineSide(t *testing.T) {
	evaluator := usecase.NewLevelEvaluator()

	tests := []struct {
		name       string
		levelPrice float64
		price      float64
		wantSide   domain.Side
	}{
		{"Price Above Level -> Long", 100.0, 101.0, domain.SideLong},
		{"Price Below Level -> Short", 100.0, 99.0, domain.SideShort},
		{"Price Equal Level -> None/Touch", 100.0, 100.0, domain.SideLong}, // Code treats equality as Long (>=)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := evaluator.DetermineSide(tt.levelPrice, tt.price)
			if got != tt.wantSide {
				t.Errorf("DetermineSide() = %v, want %v", got, tt.wantSide)
			}
		})
	}
}

const epsilon = 0.000001

func floatEquals(a, b float64) bool {
	return (a-b) < epsilon && (b-a) < epsilon
}

func TestCalculateTierBoundaries(t *testing.T) {
	evaluator := usecase.NewLevelEvaluator()
	level := &domain.Level{LevelPrice: 10000.0}
	tiers := &domain.SymbolTiers{
		Tier1Pct: 0.005,  // 0.5%
		Tier2Pct: 0.003,  // 0.3%
		Tier3Pct: 0.0015, // 0.15%
	}

	// Test Short Zone (Price < Level)
	// Price is below level, in SHORT zone
	// Tiers are BELOW level at L * (1 - Pct)
	// Tier1 = 10000 * 0.995 = 9950
	// Tier2 = 10000 * 0.997 = 9970
	// Tier3 = 10000 * 0.9985 = 9985
	boundariesShort := evaluator.CalculateBoundaries(level, tiers, domain.SideShort)

	if !floatEquals(boundariesShort[0], 9950.0) {
		t.Errorf("Tier1 Short wrong: %f", boundariesShort[0])
	}
	if !floatEquals(boundariesShort[1], 9970.0) {
		t.Errorf("Tier2 Short wrong: %f", boundariesShort[1])
	}
	if !floatEquals(boundariesShort[2], 9985.0) {
		t.Errorf("Tier3 Short wrong: %f", boundariesShort[2])
	}

	// Test Long Zone (Price > Level)
	// Price is above level, in LONG zone
	// Tiers are ABOVE level at L * (1 + Pct)
	// Tier1 = 10000 * 1.005 = 10050
	// Tier2 = 10000 * 1.003 = 10030
	// Tier3 = 10000 * 1.0015 = 10015
	boundariesLong := evaluator.CalculateBoundaries(level, tiers, domain.SideLong)

	if !floatEquals(boundariesLong[0], 10050.0) {
		t.Errorf("Tier1 Long wrong: %f", boundariesLong[0])
	}
	if !floatEquals(boundariesLong[1], 10030.0) {
		t.Errorf("Tier2 Long wrong: %f", boundariesLong[1])
	}
	if !floatEquals(boundariesLong[2], 10015.0) {
		t.Errorf("Tier3 Long wrong: %f", boundariesLong[2])
	}
}
