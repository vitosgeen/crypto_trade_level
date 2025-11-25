package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/vitos/crypto_trade_level/internal/infrastructure/exchange"
	"github.com/vitos/crypto_trade_level/internal/infrastructure/logger"
	"github.com/vitos/crypto_trade_level/internal/infrastructure/storage"
	"github.com/vitos/crypto_trade_level/internal/usecase"
	"github.com/vitos/crypto_trade_level/internal/web"
	"go.uber.org/zap"
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
	Polling struct {
		LevelsReloadMs int `yaml:"levels_reload_ms"`
	} `yaml:"polling"`
	Logging struct {
		Level string `yaml:"level"`
	} `yaml:"logging"`
	Server struct {
		Port int `yaml:"port"`
	} `yaml:"server"`
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

	// 2. Init Logger
	log, err := logger.NewLogger(cfg.Logging.Level)
	if err != nil {
		fmt.Printf("Failed to init logger: %v\n", err)
		os.Exit(1)
	}
	defer log.Sync()

	// 3. Init Storage
	store, err := storage.NewSQLiteStore("bot.db")
	if err != nil {
		log.Fatal("Failed to init sqlite", zap.Error(err))
	}

	// 4. Init Exchange (Bybit)
	// Assuming single exchange for MVP
	bybitCfg := cfg.Exchanges[0]
	bybitAdapter := exchange.NewBybitAdapter(bybitCfg.APIKey, bybitCfg.APISecret, bybitCfg.RESTEndpoint, bybitCfg.WSEndpoint)

	// 5. Init Service
	marketService := usecase.NewMarketService(bybitAdapter)
	svc := usecase.NewLevelService(store, store, bybitAdapter, marketService)

	// Init Cache
	if err := svc.UpdateCache(context.Background()); err != nil {
		log.Error("Failed to init cache", zap.Error(err))
	}

	// 9. Wait for Shutdown (moved up to allow goroutines to use 'stop')
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	// 6. Connect WS and Start Processing (with Reload Loop)
	// Register callback once
	bybitAdapter.OnPriceUpdate(func(symbol string, price float64) {
		if err := svc.ProcessTick(context.Background(), "bybit", symbol, price); err != nil {
			log.Error("Error processing tick", zap.Error(err))
		}
	})

	go func() {
		ticker := time.NewTicker(time.Duration(cfg.Polling.LevelsReloadMs) * time.Millisecond)
		defer ticker.Stop()

		activeSymbols := make(map[string]bool)

		for {
			// Initial run + Ticker
			ctx := context.Background()

			// Update Cache
			if err := svc.UpdateCache(ctx); err != nil {
				log.Error("Failed to update cache", zap.Error(err))
			}

			levels, err := store.ListLevels(ctx)
			if err != nil {
				log.Error("Failed to list levels", zap.Error(err))
			} else {
				// Diff symbols
				newSymbols := make(map[string]bool)
				var toSubscribe []string

				for _, l := range levels {
					newSymbols[l.Symbol] = true
					if !activeSymbols[l.Symbol] {
						toSubscribe = append(toSubscribe, l.Symbol)
						activeSymbols[l.Symbol] = true
					}
				}

				// Subscribe to new symbols
				if len(toSubscribe) > 0 {
					log.Info("Subscribing to new symbols", zap.Strings("symbols", toSubscribe))
					if err := bybitAdapter.Subscribe(toSubscribe); err != nil {
						log.Error("Failed to subscribe", zap.Error(err))
					}
					// Note: BybitAdapter.ConnectWS currently dials. We need a Subscribe method if already connected.
					// For MVP, if not connected, Connect. If connected, we need to send subscribe op.
					// Let's update BybitAdapter to handle this or just Connect if first time.

					// For now, let's assume we just call ConnectWS if it's the first time,
					// or we need to expose a Subscribe method on the adapter.
					// Let's check the adapter code.
				}
			}

			select {
			case <-ticker.C:
				continue
			case <-stop:
				return
			}
		}
	}()

	// Safety Monitor Loop (Every 1s)
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				svc.CheckSafety(context.Background())
			case <-stop:
				return
			}
		}
	}()

	// 7. Init Web Server
	if err := web.InitTemplates("internal/web/templates"); err != nil {
		log.Fatal("Failed to initialize templates", zap.Error(err))
	}
	port := cfg.Server.Port
	if port == 0 {
		port = 8080 // Default
	}

	// Init Speed Bot Service
	speedBotService := usecase.NewSpeedBotService(bybitAdapter, marketService, log)

	server := web.NewServer(port, store, store, svc, marketService, speedBotService, log)

	// 8. Start Server
	go func() {
		if err := server.Start(); err != nil {
			log.Fatal("Server failed", zap.Error(err))
		}
	}()

	// 9. Wait for Shutdown
	<-stop

	log.Info("Shutting down...")
	server.Shutdown(context.Background())
}
