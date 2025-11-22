package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/vitos/crypto_trade_level/internal/infrastructure/exchange"
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

	bybitCfg := cfg.Exchanges[0]
	fmt.Printf("Testing Trading on Bybit (Testnet)...\n")

	adapter := exchange.NewBybitAdapter(bybitCfg.APIKey, bybitCfg.APISecret, bybitCfg.RESTEndpoint, bybitCfg.WSEndpoint)
	ctx := context.Background()
	symbol := "BTCUSDT"
	size := 0.001 // Small size for testing
	leverage := 10

	// --- Test LONG ---
	fmt.Println("\n--- Testing LONG ---")
	fmt.Printf("Placing Market Buy Order (Size: %f)...\n", size)
	if err := adapter.MarketBuy(ctx, symbol, size, leverage, "isolated", 0.0); err != nil {
		fmt.Printf("❌ Failed to buy: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✅ Buy Order Placed")

	// Retry loop for position check
	for i := 0; i < 5; i++ {
		time.Sleep(2 * time.Second)
		pos, err := adapter.GetPosition(ctx, symbol)
		if err != nil {
			fmt.Printf("⚠️ Failed to get position (attempt %d): %v\n", i+1, err)
			continue
		}
		if pos.Size > 0 {
			fmt.Printf("✅ Position: Size=%f, Side=%s, Entry=%f\n", pos.Size, pos.Side, pos.EntryPrice)
			break
		}
		fmt.Printf("⏳ Waiting for position... (Size=%f)\n", pos.Size)
	}

	fmt.Println("Closing Position...")
	if err := adapter.ClosePosition(ctx, symbol); err != nil {
		fmt.Printf("❌ Failed to close: %v\n", err)
	} else {
		fmt.Println("✅ Position Closed")
	}

	// --- Test SHORT ---
	fmt.Println("\n--- Testing SHORT ---")
	time.Sleep(2 * time.Second)

	fmt.Printf("Placing Market Sell Order (Size: %f)...\n", size)
	if err := adapter.MarketSell(ctx, symbol, size, leverage, "isolated", 0.0); err != nil {
		fmt.Printf("❌ Failed to sell: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✅ Sell Order Placed")

	// Retry loop for position check
	for i := 0; i < 5; i++ {
		time.Sleep(2 * time.Second)
		pos, err := adapter.GetPosition(ctx, symbol)
		if err != nil {
			fmt.Printf("⚠️ Failed to get position (attempt %d): %v\n", i+1, err)
			continue
		}
		if pos.Size > 0 {
			fmt.Printf("✅ Position: Size=%f, Side=%s, Entry=%f\n", pos.Size, pos.Side, pos.EntryPrice)
			break
		}
		fmt.Printf("⏳ Waiting for position... (Size=%f)\n", pos.Size)
	}

	fmt.Println("Closing Position...")
	if err := adapter.ClosePosition(ctx, symbol); err != nil {
		fmt.Printf("❌ Failed to close: %v\n", err)
	} else {
		fmt.Println("✅ Position Closed")
	}
}
