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
		// Short (Resistance): Price is BELOW level. We want to Sell when it rises.
		// Boundaries should be BELOW level? No, wait.
		// If Resistance is 10000. Price 9000.
		// We want to Sell at 9950, 9970... (Below Level).
		// So Level * (1 - Pct).
		boundaries[0] = level.LevelPrice * (1 - tiers.Tier1Pct)
		boundaries[1] = level.LevelPrice * (1 - tiers.Tier2Pct)
		boundaries[2] = level.LevelPrice * (1 - tiers.Tier3Pct)
	} else {
		// Long (Support): Price is ABOVE level. We want to Buy when it falls.
		// Boundaries should be ABOVE level.
		// So Level * (1 + Pct).
		boundaries[0] = level.LevelPrice * (1 + tiers.Tier1Pct)
		boundaries[1] = level.LevelPrice * (1 + tiers.Tier2Pct)
		boundaries[2] = level.LevelPrice * (1 + tiers.Tier3Pct)
	}

	return boundaries
}
