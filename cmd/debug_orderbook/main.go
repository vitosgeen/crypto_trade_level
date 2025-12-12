package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/vitos/crypto_trade_level/internal/infrastructure/exchange"
)

func main() {
	apiKey := os.Getenv("BYBIT_API_KEY")
	apiSecret := os.Getenv("BYBIT_API_SECRET")
	
	// Use public endpoints if no keys
	if apiKey == "" {
		fmt.Println("No API keys provided, using public endpoints (might be rate limited or restricted)")
	}

	adapter := exchange.NewBybitAdapter(apiKey, apiSecret, exchange.BybitBaseURL, exchange.BybitWSURL)

	symbol := "0GUSDT"
	ctx := context.Background()

	fmt.Printf("Fetching Order Book for %s (Linear)...\n", symbol)
	ob, err := adapter.GetOrderBook(ctx, symbol, "linear")
	if err != nil {
		log.Fatalf("Error fetching linear order book: %v", err)
	}

	fmt.Printf("Linear Order Book: %d Bids, %d Asks\n", len(ob.Bids), len(ob.Asks))
	if len(ob.Bids) > 0 {
		fmt.Printf("Best Bid: %.4f (Size: %.4f)\n", ob.Bids[0].Price, ob.Bids[0].Size)
	}
	if len(ob.Asks) > 0 {
		fmt.Printf("Best Ask: %.4f (Size: %.4f)\n", ob.Asks[0].Price, ob.Asks[0].Size)
	}

	fmt.Printf("\nFetching Order Book for %s (Spot)...\n", symbol)
	spotOB, err := adapter.GetOrderBook(ctx, symbol, "spot")
	if err != nil {
		fmt.Printf("Error fetching spot order book: %v\n", err)
	} else {
		fmt.Printf("Spot Order Book: %d Bids, %d Asks\n", len(spotOB.Bids), len(spotOB.Asks))
		if len(spotOB.Bids) > 0 {
			fmt.Printf("Best Bid: %.4f (Size: %.4f)\n", spotOB.Bids[0].Price, spotOB.Bids[0].Size)
		}
		if len(spotOB.Asks) > 0 {
			fmt.Printf("Best Ask: %.4f (Size: %.4f)\n", spotOB.Asks[0].Price, spotOB.Asks[0].Size)
		}
	}
}
