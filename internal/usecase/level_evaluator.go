package usecase

import "github.com/vitos/crypto_trade_level/internal/domain"

type LevelEvaluator struct{}

func NewLevelEvaluator() *LevelEvaluator {
	return &LevelEvaluator{}
}

func (e *LevelEvaluator) DetermineSide(levelPrice, currentPrice float64) domain.Side {
	if currentPrice > levelPrice {
		return domain.SideLong // Price is above, level is Support -> Long
	}
	if currentPrice < levelPrice {
		return domain.SideShort // Price is below, level is Resistance -> Short
	}
	return "" // Exact match
}

// CalculateBoundaries returns [Tier1, Tier2, Tier3] prices
func (e *LevelEvaluator) CalculateBoundaries(level *domain.Level, tiers *domain.SymbolTiers, side domain.Side) []float64 {
	boundaries := make([]float64, 3)

	if side == domain.SideShort {
		// SHORT zone: Price is BELOW level (Resistance)
		// Tiers are ABOVE level at L * (1 + Pct)
		// Price rises UP through these tiers
		boundaries[0] = level.LevelPrice * (1 + tiers.Tier1Pct)
		boundaries[1] = level.LevelPrice * (1 + tiers.Tier2Pct)
		boundaries[2] = level.LevelPrice * (1 + tiers.Tier3Pct)
	} else {
		// LONG zone: Price is ABOVE level (Support)
		// Tiers are BELOW level at L * (1 - Pct)
		// Price falls DOWN through these tiers
		boundaries[0] = level.LevelPrice * (1 - tiers.Tier1Pct)
		boundaries[1] = level.LevelPrice * (1 - tiers.Tier2Pct)
		boundaries[2] = level.LevelPrice * (1 - tiers.Tier3Pct)
	}

	return boundaries
}
