package main

import (
	"context"
	"fmt"
	"os"

	"github.com/vitos/crypto_trade_level/internal/infrastructure/storage"
)

func main() {
	store, err := storage.NewSQLiteStore("bot.db")
	if err != nil {
		fmt.Printf("Failed to init sqlite: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()
	levels, err := store.ListLevels(ctx)
	if err != nil {
		fmt.Printf("Failed to list levels: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Found %d levels:\n", len(levels))
	for _, l := range levels {
		fmt.Printf("- Level ID: %s, Symbol: %s, Price: %f\n", l.ID, l.Symbol, l.LevelPrice)

		tiers, err := store.GetSymbolTiers(ctx, l.Exchange, l.Symbol)
		if err != nil {
			fmt.Printf("  ❌ Failed to get tiers: %v\n", err)
		} else if tiers == nil {
			fmt.Printf("  ⚠️ No tiers found for %s/%s\n", l.Exchange, l.Symbol)
		} else {
			fmt.Printf("  ✅ Tiers: T1=%f (%.4f%%), T2=%f (%.4f%%), T3=%f (%.4f%%)\n",
				tiers.Tier1Pct, tiers.Tier1Pct*100,
				tiers.Tier2Pct, tiers.Tier2Pct*100,
				tiers.Tier3Pct, tiers.Tier3Pct*100)
		}
	}
}
