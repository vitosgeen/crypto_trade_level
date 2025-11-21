package main

import (
	"context"
	"fmt"
	"os"

	"github.com/vitos/crypto_trade_level/internal/domain"
	"github.com/vitos/crypto_trade_level/internal/infrastructure/exchange"
	"github.com/vitos/crypto_trade_level/internal/infrastructure/storage"
	"github.com/vitos/crypto_trade_level/internal/usecase"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Exchanges []struct {
		Name         string `yaml:"name"`
		APIKey       string `yaml:"api_key"`
		APISecret    string `yaml:"api_secret"`
		WSEndpoint   string `yaml:"ws_endpoint"`
		RESTEndpoint string `yaml:"rest_endpoint"`
	} `yaml:"exchanges"`
}

func loadConfig(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var cfg Config
	decoder := yaml.NewDecoder(f)
	if err := decoder.Decode(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func main() {
	// 1. Load Config
	cfg, err := loadConfig("config/config.yaml")
	if err != nil {
		fmt.Printf("Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// 2. Init Storage
	store, err := storage.NewSQLiteStore("bot.db")
	if err != nil {
		fmt.Printf("Failed to init sqlite: %v\n", err)
		os.Exit(1)
	}

	// 3. Init Exchange
	bybitCfg := cfg.Exchanges[0]
	adapter := exchange.NewBybitAdapter(bybitCfg.APIKey, bybitCfg.APISecret, bybitCfg.RESTEndpoint, bybitCfg.WSEndpoint)
	ctx := context.Background()

	// 4. List Levels
	levels, err := store.ListLevels(ctx)
	if err != nil {
		fmt.Printf("Failed to list levels: %v\n", err)
		os.Exit(1)
	}

	if len(levels) == 0 {
		fmt.Println("No levels found in DB.")
		return
	}

	fmt.Printf("Found %d levels. Analyzing...\n", len(levels))

	evaluator := usecase.NewLevelEvaluator()

	for _, l := range levels {
		fmt.Printf("\n--------------------------------------------------\n")
		fmt.Printf("Level ID: %s, Symbol: %s, Price: %f\n", l.ID, l.Symbol, l.LevelPrice)

		// Get Current Price
		currPrice, err := adapter.GetCurrentPrice(ctx, l.Symbol)
		if err != nil {
			fmt.Printf("❌ Failed to get current price: %v\n", err)
			continue
		}
		fmt.Printf("Current Market Price: %f\n", currPrice)

		// Determine Side
		side := evaluator.DetermineSide(l.LevelPrice, currPrice)
		fmt.Printf("Determined Side: %s (Defending)\n", side)

		// Calculate Boundaries
		// Create default tiers since we don't have them in DB yet or they are separate
		tiers := &domain.SymbolTiers{
			Tier1Pct: 0.001,
			Tier2Pct: 0.002,
			Tier3Pct: 0.003,
		}

		boundaries := evaluator.CalculateBoundaries(l, tiers, side)
		t1, t2, t3 := boundaries[0], boundaries[1], boundaries[2]

		fmt.Printf("Boundaries:\n")
		fmt.Printf("  Tier 1 (%.2f%%): %f\n", tiers.Tier1Pct*100, t1)
		fmt.Printf("  Tier 2 (%.2f%%): %f\n", tiers.Tier2Pct*100, t2)
		fmt.Printf("  Tier 3 (%.2f%%): %f\n", tiers.Tier3Pct*100, t3)

		// Analyze Trigger
		fmt.Printf("Analysis:\n")

		if side == domain.SideLong { // Support: Price > Level. Trigger if Price <= Boundary
			dist := currPrice - l.LevelPrice
			fmt.Printf("  Distance to Level: %f (%.4f%%)\n", dist, (dist/l.LevelPrice)*100)

			if currPrice <= t1 {
				fmt.Printf("  ✅ HIT Tier 1 (%f)\n", t1)
			} else {
				fmt.Printf("  ❌ Above Tier 1 (Need drop to %f)\n", t1)
			}
			if currPrice <= t2 {
				fmt.Printf("  ✅ HIT Tier 2 (%f)\n", t2)
			} else {
				fmt.Printf("  ❌ Above Tier 2 (Need drop to %f)\n", t2)
			}
			if currPrice <= t3 {
				fmt.Printf("  ✅ HIT Tier 3 (%f)\n", t3)
			} else {
				fmt.Printf("  ❌ Above Tier 3 (Need drop to %f)\n", t3)
			}
		} else { // Resistance: Price < Level. Trigger if Price >= Boundary
			dist := l.LevelPrice - currPrice
			fmt.Printf("  Distance to Level: %f (%.4f%%)\n", dist, (dist/l.LevelPrice)*100)

			if currPrice >= t1 {
				fmt.Printf("  ✅ HIT Tier 1 (%f)\n", t1)
			} else {
				fmt.Printf("  ❌ Below Tier 1 (Need rise to %f)\n", t1)
			}
			if currPrice >= t2 {
				fmt.Printf("  ✅ HIT Tier 2 (%f)\n", t2)
			} else {
				fmt.Printf("  ❌ Below Tier 2 (Need rise to %f)\n", t2)
			}
			if currPrice >= t3 {
				fmt.Printf("  ✅ HIT Tier 3 (%f)\n", t3)
			} else {
				fmt.Printf("  ❌ Below Tier 3 (Need rise to %f)\n", t3)
			}
		}
	}
}
