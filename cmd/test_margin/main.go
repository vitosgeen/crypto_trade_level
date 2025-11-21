package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/joho/godotenv"
	"github.com/vitos/crypto_trade_level/internal/infrastructure/exchange"
)

func main() {
	// Load .env
	godotenv.Load()

	apiKey := os.Getenv("BYBIT_API_KEY")
	apiSecret := os.Getenv("BYBIT_API_SECRET")

	if apiKey == "" || apiSecret == "" {
		log.Fatal("Missing BYBIT_API_KEY or BYBIT_API_SECRET")
	}

	client := exchange.NewBybitAdapter(apiKey, apiSecret, exchange.BybitBaseURL, exchange.BybitWSURL)

	ctx := context.Background()

	// Test setting margin mode to isolated
	fmt.Println("Testing margin mode setting...")

	// This will call setMarginMode internally
	err := client.MarketBuy(ctx, "BTCUSDT", 0.001, 10, "isolated")
	if err != nil {
		log.Printf("Error: %v", err)
	} else {
		fmt.Println("Success! Check the logs above for margin mode debug output.")
	}
}
