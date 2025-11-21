package main

import (
	"context"
	"fmt"
	"os"

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
	fmt.Printf("Testing Bybit Interaction...\n")
	fmt.Printf("Endpoint: %s\n", bybitCfg.RESTEndpoint)
	fmt.Printf("API Key: %s...\n", bybitCfg.APIKey[:4])

	adapter := exchange.NewBybitAdapter(bybitCfg.APIKey, bybitCfg.APISecret, bybitCfg.RESTEndpoint, bybitCfg.WSEndpoint)
	ctx := context.Background()

	// 2. Check Public Endpoint (Price)
	price, err := adapter.GetCurrentPrice(ctx, "BTCUSDT")
	if err != nil {
		fmt.Printf("❌ Failed to get price: %v\n", err)
	} else {
		fmt.Printf("✅ Current Price (BTCUSDT): %f\n", price)
	}

	// 3. Check Private Endpoint (Position)
	pos, err := adapter.GetPosition(ctx, "BTCUSDT")
	if err != nil {
		fmt.Printf("❌ Failed to get position: %v\n", err)
	} else {
		fmt.Printf("✅ Position (BTCUSDT): Size=%f, Side=%s, Entry=%f, PnL=%f\n",
			pos.Size, pos.Side, pos.EntryPrice, pos.UnrealizedPnL)
	}
}
