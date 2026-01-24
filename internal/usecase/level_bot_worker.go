package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/vitos/crypto_trade_level/internal/domain"
	"go.uber.org/zap"
)

type LevelBotWorker struct {
	service *LevelService
	logger  *zap.Logger

	mu         sync.RWMutex
	cached     []domain.CoinData
	lastUpdate time.Time
}

func NewLevelBotWorker(service *LevelService, logger *zap.Logger) *LevelBotWorker {
	return &LevelBotWorker{
		service: service,
		logger:  logger,
	}
}

func (w *LevelBotWorker) Start(ctx context.Context) {
	w.logger.Info("Starting Level Bot Worker")
	ticker := time.NewTicker(1 * time.Minute)

	// Run immediately first time
	go w.collectData(ctx)

	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				w.collectData(ctx)
			}
			// Run cleanup periodically
			go func() {
				cleanupTicker := time.NewTicker(1 * time.Hour)
				defer cleanupTicker.Stop()
				w.cleanupLogs() // Run immediately
				for {
					select {
					case <-ctx.Done():
						return
					case <-cleanupTicker.C:
						w.cleanupLogs()
					}
				}
			}()
		}
	}()
}

func (w *LevelBotWorker) GetData() []domain.CoinData {
	w.mu.RLock()
	defer w.mu.RUnlock()
	// Return copy
	result := make([]domain.CoinData, len(w.cached))
	copy(result, w.cached)
	return result
}

func (w *LevelBotWorker) collectData(ctx context.Context) {
	start := time.Now()
	// w.logger.Info("Worker: Collecting Level Bot data...")

	instruments, err := w.service.GetExchange().GetInstruments(ctx, "linear")
	if err != nil {
		w.logger.Error("Worker: Failed to get instruments", zap.Error(err))
		return
	}

	tickers, err := w.service.GetExchange().GetTickers(ctx, "linear")
	if err != nil {
		w.logger.Error("Worker: Failed to get tickers", zap.Error(err))
		return
	}

	// Map tickers by symbol for easy lookup
	tickerMap := make(map[string]domain.Ticker)
	for _, t := range tickers {
		tickerMap[t.Symbol] = t
	}

	var allCoins []domain.CoinData
	for _, inst := range instruments {
		if inst.Status != "Trading" {
			continue
		}
		t, ok := tickerMap[inst.Symbol]
		coin := domain.CoinData{
			Symbol:    inst.Symbol,
			BaseCoin:  inst.BaseCoin,
			QuoteCoin: inst.QuoteCoin,
			Status:    inst.Status,
		}
		if ok {
			coin.LastPrice = t.LastPrice
			coin.Price24hPcnt = t.Price24hPcnt
			coin.Volume24h = t.Volume24h
			coin.OpenInterest = t.OpenInterest
			coin.OpenInterestValue = t.OpenInterest * t.LastPrice
			coin.FundingRate = t.FundingRate
			// Add 24h Range data from ticker
			if t.Low24h > 0 {
				coin.Range24h = ((t.High24h - t.Low24h) / t.Low24h) * 100
				coin.Max24h = t.High24h
				coin.Min24h = t.Low24h
				// Trend 24h: current vs 24h ago (estimated from 24h change)
				if t.Price24hPcnt > 0 {
					coin.Trend24h = "up"
				} else if t.Price24hPcnt < 0 {
					coin.Trend24h = "down"
				}

				// Highlight logic: within 0.1% of boundary
				threshold := 0.001 // 0.1%
				if coin.Max4h > 0 && math.Abs(coin.LastPrice-coin.Max4h)/coin.Max4h <= threshold {
					coin.Near4hMax = true
				}
				if coin.Min4h > 0 && math.Abs(coin.LastPrice-coin.Min4h)/coin.Min4h <= threshold {
					coin.Near4hMin = true
				}
				if coin.Max1h > 0 && math.Abs(coin.LastPrice-coin.Max1h)/coin.Max1h <= threshold {
					coin.Near1hMax = true
				}
				if coin.Min1h > 0 && math.Abs(coin.LastPrice-coin.Min1h)/coin.Min1h <= threshold {
					coin.Near1hMin = true
				}
				if coin.Max24h > 0 && math.Abs(coin.LastPrice-coin.Max24h)/coin.Max24h <= threshold {
					coin.Near24hMax = true
				}
				if coin.Min24h > 0 && math.Abs(coin.LastPrice-coin.Min24h)/coin.Min24h <= threshold {
					coin.Near24hMin = true
				}
			}
		}
		allCoins = append(allCoins, coin)
	}

	// Sort by Open Interest Value to find "big" coins
	sort.Slice(allCoins, func(i, j int) bool {
		return allCoins[i].OpenInterestValue > allCoins[j].OpenInterestValue
	})

	// Take top coins to calculate range (limit to 60 for performance)
	limit := 60
	if len(allCoins) < limit {
		limit = len(allCoins)
	}

	// We only want to process the top 'limit' coins, but we want to return all coins?
	// The original code only processed the top 60, but passed `allCoins` (which contains ALL coins) to `allCoins[idx]`.
	// Wait, strictly `allCoins` in the original code had size of all instruments.
	// The loop `for i := 0; i < limit; i++` only processed the top 60.
	// The template iterates over `Instruments`, which is passed `allCoins`.
	// So only the top 60 have the detailed candle data (`Range10m` etc), others have 0.
	// That's fine, we replicate that.

	var wg sync.WaitGroup
	semaphore := make(chan struct{}, 10) // Concurrency limit

	for i := 0; i < limit; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			symbol := allCoins[idx].Symbol
			// Range 10m: 10 x 1m candles
			candles10m, _ := w.service.GetExchange().GetCandles(ctx, symbol, "1", 10)
			if len(candles10m) > 0 {
				minL, maxH := candles10m[0].Low, candles10m[0].High
				for _, c := range candles10m {
					if c.Low < minL {
						minL = c.Low
					}
					if c.High > maxH {
						maxH = c.High
					}
				}
				if minL > 0 {
					allCoins[idx].Range10m = ((maxH - minL) / minL) * 100
					allCoins[idx].Max10m = maxH
					allCoins[idx].Min10m = minL
					// Trend: Current vs Start of range
					startPrice := candles10m[0].Open
					if allCoins[idx].LastPrice > startPrice {
						allCoins[idx].Trend10m = "up"
					} else if allCoins[idx].LastPrice < startPrice {
						allCoins[idx].Trend10m = "down"
					}
				}
			}

			// Range 1h: 60 x 1m candles (safer than 1h candle if it just started)
			candles1h, _ := w.service.GetExchange().GetCandles(ctx, symbol, "1", 60)
			if len(candles1h) > 0 {
				minL, maxH := candles1h[0].Low, candles1h[0].High
				for _, c := range candles1h {
					if c.Low < minL {
						minL = c.Low
					}
					if c.High > maxH {
						maxH = c.High
					}
				}
				if minL > 0 {
					allCoins[idx].Range1h = ((maxH - minL) / minL) * 100
					allCoins[idx].Max1h = maxH
					allCoins[idx].Min1h = minL
					// Trend: Current vs Start of range
					startPrice := candles1h[0].Open
					if allCoins[idx].LastPrice > startPrice {
						allCoins[idx].Trend1h = "up"
					} else if allCoins[idx].LastPrice < startPrice {
						allCoins[idx].Trend1h = "down"
					}
				}
			}

			// Range 4h: 4 x 60m candles (last 4 hours)
			candles4h, _ := w.service.GetExchange().GetCandles(ctx, symbol, "60", 4)
			if len(candles4h) > 0 {
				minL, maxH := candles4h[0].Low, candles4h[0].High
				for _, c := range candles4h {
					if c.Low < minL {
						minL = c.Low
					}
					if c.High > maxH {
						maxH = c.High
					}
				}
				if minL > 0 {
					allCoins[idx].Range4h = ((maxH - minL) / minL) * 100
					allCoins[idx].Max4h = maxH
					allCoins[idx].Min4h = minL
					// Trend: Current vs Start of range
					startPrice := candles4h[0].Open
					if allCoins[idx].LastPrice > startPrice {
						allCoins[idx].Trend4h = "up"
					} else if allCoins[idx].LastPrice < startPrice {
						allCoins[idx].Trend4h = "down"
					}
				}
			}

			// Re-calculate highlights because Max/Min might have changed from candles?
			// The original code calculated highlights based on Ticker High/Low 24h, AND 4h/1h/24h.
			// Wait, the original code had:
			// if t.Low24h > 0 {
			//    ...
			//    // Highlight logic
			//    if coin.Max4h > 0 ...
			// }
			// NOTE: In the original loop, coin.Max4h was 0 when created from Ticker.
			// However, coin is a COPY in the loop? `coin := CoinData{...}`.
			// No, `allCoins` is a slice of structs.
			// Inside the Goroutine, `allCoins[idx]` is modified.
			// In the original Ticker loop, `coin.Max4h` is NOT set yet.
			// So `coin.Near4hMax` would be false initially.
			// BUT, the original code had `if t.Low24h > 0` block where it did highlight logic.
			// At that point `coin.Max4h` etc are 0.
			// So `coin.Max4h > 0` checks would fail.
			// Thus, `Near4hMax` etc were effectively mostly false unless I missed something.
			// Wait, `Max24h` IS set fromTicker. `Next24hMax` is checked.
			// But `Max4h` is zero.
			// So `Near4hMax` was likely broken in the original code logic order?
			// Or maybe I missed where `Max4h` was set before highlight logic?
			// `coin.Max4h` is set in the goroutine later.
			// So initially `Near4hMax` is false.
			// The goroutine sets `Max4h` but does NOT update `Near4hMax`.
			// So `Near4hMax` was probably never true in the original code?

			// Let's re-read the original ViewCodeItem.
			/*
			   if t.Low24h > 0 {
			       coin.Range24h = ...
			       coin.Max24h = t.High24h
			       ...
			       if coin.Max4h > 0 && ... // This is inside the initial loop. coin.Max4h IS 0 here.
			   }
			   allCoins = append(allCoins, coin)
			*/
			// Yes, checks for Max4h > 0 inside the first loop are futile.
			// Max24h IS set. So at least Near24hMax works.

			// I should probably fix this opportunity to update the highlight logic AFTER setting the candles.
			// I will add highlight logic inside the goroutine after setting Max4h/Min4h.

			threshold := 0.001
			if allCoins[idx].Max4h > 0 && math.Abs(allCoins[idx].LastPrice-allCoins[idx].Max4h)/allCoins[idx].Max4h <= threshold {
				allCoins[idx].Near4hMax = true
			}
			if allCoins[idx].Min4h > 0 && math.Abs(allCoins[idx].LastPrice-allCoins[idx].Min4h)/allCoins[idx].Min4h <= threshold {
				allCoins[idx].Near4hMin = true
			}
			if allCoins[idx].Max1h > 0 && math.Abs(allCoins[idx].LastPrice-allCoins[idx].Max1h)/allCoins[idx].Max1h <= threshold {
				allCoins[idx].Near1hMax = true
			}
			if allCoins[idx].Min1h > 0 && math.Abs(allCoins[idx].LastPrice-allCoins[idx].Min1h)/allCoins[idx].Min1h <= threshold {
				allCoins[idx].Near1hMin = true
			}

		}(i)
	}

	wg.Wait()

	duration := time.Since(start)
	w.logger.Info("Worker: Data collection complete", zap.Duration("duration", duration), zap.Int("coins", len(allCoins)))

	w.mu.Lock()
	w.cached = allCoins
	w.lastUpdate = time.Now()
	w.mu.Unlock()

	w.logData(allCoins)
}

func (w *LevelBotWorker) logData(coins []domain.CoinData) {
	entry := struct {
		Time time.Time         `json:"time"`
		Data []domain.CoinData `json:"data"`
	}{
		Time: time.Now(),
		Data: coins,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		w.logger.Error("Failed to marshal log entry", zap.Error(err))
		return
	}

	dir := "logs/level_bot"
	if err := os.MkdirAll(dir, 0755); err != nil {
		w.logger.Error("Failed to create log directory", zap.Error(err))
		return
	}

	filename := fmt.Sprintf("data_%s.jsonl", time.Now().Format("2006-01-02"))
	filepath := filepath.Join(dir, filename)

	f, err := os.OpenFile(filepath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		w.logger.Error("Failed to open log file", zap.Error(err))
		return
	}
	defer f.Close()

	if _, err := f.Write(data); err != nil {
		w.logger.Error("Failed to write log entry", zap.Error(err))
		return
	}
	f.WriteString("\n")
}

func (w *LevelBotWorker) cleanupLogs() {
	dir := "logs/level_bot"
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			w.logger.Error("Failed to read log directory for cleanup", zap.Error(err))
		}
		return
	}

	retention := 72 * time.Hour // Keep 3 days to ensure full 24h coverage
	cutoff := time.Now().Add(-retention)

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "data_") || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}

		// Parse date from filename: data_2006-01-02.jsonl
		dateStr := strings.TrimSuffix(strings.TrimPrefix(entry.Name(), "data_"), ".jsonl")
		date, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			continue // Skip files with unexpected formats
		}

		// Check if the file's date is before the cutoff (comparing dates effectively)
		// Since filenames are dates (00:00:00), if the date is before cutoff (e.g. 3 days ago), delete it.
		// Example: Today is 23rd. Cutoff (3 days ago) is 20th.
		// file 19th -> 19 < 20 -> Delete.
		// file 20th -> 20 == 20 -> Keep (maybe).
		if date.Before(cutoff) {
			fullPath := filepath.Join(dir, entry.Name())
			if err := os.Remove(fullPath); err != nil {
				w.logger.Error("Failed to remove old log file", zap.String("file", fullPath), zap.Error(err))
			} else {
				w.logger.Info("Removed old log file", zap.String("file", fullPath))
			}
		}
	}
}
