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
	service         *LevelService
	logger          *zap.Logger
	refreshInterval time.Duration

	mu              sync.RWMutex
	cached          []domain.CoinData
	lastUpdate      time.Time
	history         map[string][]LogPoint
	analysisResults []AnalysisResult
	analyzer        *LogAnalyzerService
}

func NewLevelBotWorker(service *LevelService, logger *zap.Logger, refreshInterval time.Duration) *LevelBotWorker {
	if refreshInterval <= 0 {
		refreshInterval = 1 * time.Minute
	}
	return &LevelBotWorker{
		service:         service,
		logger:          logger,
		refreshInterval: refreshInterval,
		history:         make(map[string][]LogPoint),
		analyzer:        NewLogAnalyzerService(logger),
	}
}

func (w *LevelBotWorker) Start(ctx context.Context) {
	w.logger.Info("Starting Level Bot Worker", zap.Duration("refresh_interval", w.refreshInterval))

	// Prime history from file
	w.primeHistory()

	ticker := time.NewTicker(w.refreshInterval)

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
		}
	}()

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

func (w *LevelBotWorker) primeHistory() {
	allResults, err := w.analyzer.AnalyzeLatestLogs()
	if err == nil {
		w.mu.Lock()
		w.analysisResults = allResults
		// Also try to load entries for continuous history
		logDir := "logs/level_bot"
		files, _ := os.ReadDir(logDir)
		var latestFile string
		for _, f := range files {
			if filepath.Ext(f.Name()) == ".jsonl" {
				latestFile = filepath.Join(logDir, f.Name())
			}
		}
		if latestFile != "" {
			file, err := os.Open(latestFile)
			if err == nil {
				defer file.Close()
				decoder := json.NewDecoder(file)
				for decoder.More() {
					var entry LogEntry
					if err := decoder.Decode(&entry); err == nil {
						for _, coin := range entry.Data {
							w.history[coin.Symbol] = append(w.history[coin.Symbol], LogPoint{
								Time:       entry.Time,
								OI:         coin.OpenInterest,
								Price:      coin.LastPrice,
								Volume:     coin.Volume24h,
								RSI:        coin.RSI,
								MACD:       coin.MACD,
								MACDHist:   coin.MACDHist,
								ShadowPcnt: coin.ShadowPcnt,
							})
						}
					}
				}
			}
		}
		w.mu.Unlock()
	}
}

func (w *LevelBotWorker) GetData() []domain.CoinData {
	w.mu.RLock()
	defer w.mu.RUnlock()
	// Return copy
	result := make([]domain.CoinData, len(w.cached))
	copy(result, w.cached)
	return result
}

func (w *LevelBotWorker) GetRefreshInterval() time.Duration {
	return w.refreshInterval
}

func (w *LevelBotWorker) GetAnalysisResults() []AnalysisResult {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.analysisResults
}

func (w *LevelBotWorker) collectData(ctx context.Context) {
	start := time.Now()

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

	limit := 200
	if len(allCoins) < limit {
		limit = len(allCoins)
	}

	var wg sync.WaitGroup
	semaphore := make(chan struct{}, 10) // Concurrency limit

	for i := 0; i < limit; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			symbol := allCoins[idx].Symbol
			candles1m, err := w.service.GetExchange().GetCandles(ctx, symbol, "1", 100)
			if err == nil && len(candles1m) >= 34 {
				rsi := w.service.market.CalculateRSI(candles1m, 14)
				if len(rsi) > 0 {
					allCoins[idx].RSI = rsi[len(rsi)-1]
				}
				macd, signal, hist := w.service.market.CalculateMACD(candles1m, 12, 26, 9)
				if len(macd) > 0 {
					allCoins[idx].MACD = macd[len(macd)-1]
					allCoins[idx].MACDSignal = signal[len(signal)-1]
					allCoins[idx].MACDHist = hist[len(hist)-1]
				}
				count := 0
				totalShadowPcnt := 0.0
				limitShadow := 60
				if len(candles1m) < limitShadow {
					limitShadow = len(candles1m)
				}
				for j := len(candles1m) - limitShadow; j < len(candles1m); j++ {
					c := candles1m[j]
					rng := c.High - c.Low
					if rng > 0 {
						body := math.Abs(c.Open - c.Close)
						shadows := rng - body
						totalShadowPcnt += (shadows / rng) * 100
						count++
					}
				}
				if count > 0 {
					allCoins[idx].ShadowPcnt = totalShadowPcnt / float64(count)
				}
			}

			if len(candles1m) >= 10 {
				last10 := candles1m[len(candles1m)-10:]
				minL, maxH := last10[0].Low, last10[0].High
				for _, c := range last10 {
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
					startPrice := last10[0].Open
					if allCoins[idx].LastPrice > startPrice {
						allCoins[idx].Trend10m = "up"
					} else if allCoins[idx].LastPrice < startPrice {
						allCoins[idx].Trend10m = "down"
					}
				}
			}

			if len(candles1m) >= 60 {
				last60 := candles1m[len(candles1m)-60:]
				minL, maxH := last60[0].Low, last60[0].High
				for _, c := range last60 {
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
					startPrice := last60[0].Open
					if allCoins[idx].LastPrice > startPrice {
						allCoins[idx].Trend1h = "up"
					} else if allCoins[idx].LastPrice < startPrice {
						allCoins[idx].Trend1h = "down"
					}
				}
			}

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
					startPrice := candles4h[0].Open
					if allCoins[idx].LastPrice > startPrice {
						allCoins[idx].Trend4h = "up"
					} else if allCoins[idx].LastPrice < startPrice {
						allCoins[idx].Trend4h = "down"
					}
				}
			}

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

	// Update History (Compact)
	maxHistoryPoints := int(24 * time.Hour / w.refreshInterval)
	if maxHistoryPoints < 100 {
		maxHistoryPoints = 100
	}

	for _, coin := range allCoins {
		point := LogPoint{
			Time:       w.lastUpdate,
			OI:         coin.OpenInterest,
			Price:      coin.LastPrice,
			Volume:     coin.Volume24h,
			RSI:        coin.RSI,
			MACD:       coin.MACD,
			MACDHist:   coin.MACDHist,
			ShadowPcnt: coin.ShadowPcnt,
		}
		w.history[coin.Symbol] = append(w.history[coin.Symbol], point)

		// Prune
		if len(w.history[coin.Symbol]) > maxHistoryPoints {
			w.history[coin.Symbol] = w.history[coin.Symbol][len(w.history[coin.Symbol])-maxHistoryPoints:]
		}
	}

	// Run Analysis in memory
	w.analysisResults = w.analyzer.AnalyzeMap(w.history)

	// Trigger Research Logging for Top 5 coins based on analysis (or just top OI)
	// We do this to ensure research files are populated even without active positions
	for i := 0; i < 5 && i < len(allCoins); i++ {
		w.service.RecordResearchMetrics(ctx, allCoins[i].Symbol)
	}

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

		dateStr := strings.TrimSuffix(strings.TrimPrefix(entry.Name(), "data_"), ".jsonl")
		date, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			continue
		}

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
