package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/vitos/crypto_trade_level/internal/domain"
	"github.com/vitos/crypto_trade_level/internal/infrastructure/storage"
)

func main() {
	// Connect to database
	store, err := storage.NewSQLiteStore("bot.db")
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	ctx := context.Background()

	// Create test level close to current price (85,150)
	// Set level at 85,200 (slightly above) to test LONG zone
	level := &domain.Level{
		ID:         fmt.Sprintf("%d", time.Now().UnixNano()),
		Exchange:   "bybit",
		Symbol:     "BTCUSDT",
		LevelPrice: 85200.0,
		BaseSize:   0.001, // Small test size
		Leverage:   1,
		MarginType: "isolated",
		CoolDownMs: 5000,
		Source:     "test-script",
		CreatedAt:  time.Now(),
	}

	if err := store.SaveLevel(ctx, level); err != nil {
		log.Fatalf("Failed to save level: %v", err)
	}

	fmt.Printf("✅ Test level added successfully!\n")
	fmt.Printf("Level ID: %s\n", level.ID)
	fmt.Printf("Symbol: %s\n", level.Symbol)
	fmt.Printf("Level Price: %.2f\n", level.LevelPrice)
	fmt.Printf("Base Size: %.4f\n", level.BaseSize)

	// Save tiers
	tiers := &domain.SymbolTiers{
		Exchange:  "bybit",
		Symbol:    "BTCUSDT",
		Tier1Pct:  0.001, // 0.1%
		Tier2Pct:  0.002, // 0.2%
		Tier3Pct:  0.003, // 0.3%
		UpdatedAt: time.Now(),
	}

	if err := store.SaveSymbolTiers(ctx, tiers); err != nil {
		log.Fatalf("Failed to save tiers: %v", err)
	}

	fmt.Printf("\n✅ Tiers configured:\n")
	fmt.Printf("Tier1: 0.1%% = %.2f\n", level.LevelPrice*(1+tiers.Tier1Pct))
	fmt.Printf("Tier2: 0.2%% = %.2f\n", level.LevelPrice*(1+tiers.Tier2Pct))
	fmt.Printf("Tier3: 0.3%% = %.2f\n", level.LevelPrice*(1+tiers.Tier3Pct))
	fmt.Printf("\nWith current price ~85,150, this level is in LONG zone.\n")
	fmt.Printf("Tiers will be ABOVE the level, and will trigger as price falls DOWN through them.\n")
}
