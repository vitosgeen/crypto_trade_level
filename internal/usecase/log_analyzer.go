package usecase

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/vitos/crypto_trade_level/internal/domain"
	"go.uber.org/zap"
)

type LogEntry struct {
	Time time.Time         `json:"time"`
	Data []domain.CoinData `json:"data"`
}

type TimeframeChange struct {
	ChangePcnt     float64
	ChangeValue    float64
	IsConsistent   bool
	Direction      string
	StartPrice     float64
	EndPrice       float64
	VolChangePcnt  float64
	VolChangeValue float64
}

type AnalysisResult struct {
	Symbol       string
	StartOI      float64 // OI at start of 24h/Max available
	EndOI        float64 // Current OI
	CurrentPrice float64 // Current Price
	Volume24h    float64 // 24h Volume
	Change1m     TimeframeChange
	Change10m    TimeframeChange
	Change1h     TimeframeChange
	Change4h     TimeframeChange
	Change24h    TimeframeChange
}

type LogAnalyzerService struct {
	logger *zap.Logger
}

func NewLogAnalyzerService(logger *zap.Logger) *LogAnalyzerService {
	return &LogAnalyzerService{
		logger: logger,
	}
}

func (s *LogAnalyzerService) AnalyzeLatestLogs() ([]AnalysisResult, error) {
	logDir := "logs/level_bot"
	files, err := os.ReadDir(logDir)
	if err != nil {
		return nil, fmt.Errorf("error reading log dir: %w", err)
	}

	// Get latest file
	var latestFile string
	for _, f := range files {
		if filepath.Ext(f.Name()) == ".jsonl" {
			latestFile = filepath.Join(logDir, f.Name())
		}
	}

	if latestFile == "" {
		return nil, fmt.Errorf("no log files found")
	}

	file, err := os.Open(latestFile)
	if err != nil {
		return nil, fmt.Errorf("error opening file: %w", err)
	}
	defer file.Close()

	// Map symbol -> list of (Time, OI)
	type Point struct {
		Time   time.Time
		OI     float64
		Price  float64
		Volume float64
	}
	history := make(map[string][]Point)

	decoder := json.NewDecoder(file)
	for decoder.More() {
		var entry LogEntry
		if err := decoder.Decode(&entry); err != nil {
			s.logger.Error("Decode error", zap.Error(err))
			continue
		}

		for _, coin := range entry.Data {
			history[coin.Symbol] = append(history[coin.Symbol], Point{
				Time:   entry.Time,
				OI:     coin.OpenInterest,
				Price:  coin.LastPrice,
				Volume: coin.Volume24h,
			})
		}
	}

	var results []AnalysisResult

	// Check for dated futures (e.g. "SYMBOL-27FEB26")
	// The pattern is typically a hyphen followed by some characters.
	// User specifically asked to ignore "coins like this 27FEB26", which appear as "DOGEUSDT-27FEB26".
	datedFuturePattern := regexp.MustCompile(`-[0-9]{2}[A-Z]{3}[0-9]{2}`)

	for symbol, points := range history {
		if len(points) < 2 {
			continue
		}

		// Skip dated futures
		if datedFuturePattern.MatchString(symbol) {
			continue
		}

		// Skip "PERP" coins (e.g. FILPERP) which are usually redundant or deprecated on some exchanges vs linear perp with USDT
		if strings.HasSuffix(symbol, "PERP") {
			continue
		}

		// We assume points are sorted by time (since we read log in order)
		current := points[len(points)-1]
		endOI := current.OI
		endVol := current.Volume
		startOI := points[0].OI // Just for reference of total span

		// Helper to calculate change for a duration
		calcChange := func(duration time.Duration) TimeframeChange {
			targetTime := current.Time.Add(-duration)

			// Find point closest to targetTime (but not after it if possible, or interpolation)
			// Simple approach: find first point >= targetTime
			// Since points are sorted, we can use binary search or simple scan (scan is fine for log files per symbol)
			// But for simplicity, let's just reverse scan until we hit it

			var startPoint Point
			found := false

			for i := len(points) - 1; i >= 0; i-- {
				if points[i].Time.Before(targetTime) || points[i].Time.Equal(targetTime) {
					startPoint = points[i]
					// We want the one just before or at target time.
					// Actually, if we have points at T, T-1m, T-2m...
					// T-10m might correspond exactly.
					found = true
					break
				}
			}

			// If we didn't find a point far back enough, use the detailed oldest point?
			// Or should we return 0?
			// If dataset < duration, we can't calculate accurate duration change.
			if !found {
				// use earliest available if requested duration > available history?
				// User wants "changes by time frames". If we only have 1 hour of logs, 24h change should probably be the 1h change or marked N/A.
				// Let's use 0 if not enough data, to distinguish "no change" from "insufficient data" (though struct is float).
				// Let's just return empty.
				return TimeframeChange{}
			}

			if startPoint.OI == 0 {
				return TimeframeChange{}
			}

			changePcnt := ((endOI - startPoint.OI) / startPoint.OI) * 100

			// Calculate consistency in the range [startPoint index ... end]
			// We need index of startPoint
			startIndex := -1
			for i := 0; i < len(points); i++ {
				if points[i].Time == startPoint.Time {
					startIndex = i
					break
				}
			}

			if startIndex == -1 {
				return TimeframeChange{}
			}

			upSteps := 0
			downSteps := 0
			totalSteps := 0

			for i := startIndex + 1; i < len(points); i++ {
				diff := points[i].OI - points[i-1].OI
				if diff > 0 {
					upSteps++
				} else if diff < 0 {
					downSteps++
				}
				if diff != 0 {
					totalSteps++
				}
			}

			isConsistent := false
			direction := "flat"

			if changePcnt > 0 {
				direction = "up"
				if totalSteps > 0 && float64(upSteps)/float64(totalSteps) > 0.6 {
					isConsistent = true
				}
			} else if changePcnt < 0 {
				direction = "down"
				if totalSteps > 0 && float64(downSteps)/float64(totalSteps) > 0.6 {
					isConsistent = true
				}
			}

			changeValue := endOI - startPoint.OI

			volChangeValue := endVol - startPoint.Volume
			volChangePcnt := 0.0
			if startPoint.Volume > 0 {
				volChangePcnt = ((endVol - startPoint.Volume) / startPoint.Volume) * 100
			}

			return TimeframeChange{
				ChangePcnt:     changePcnt,
				ChangeValue:    changeValue,
				IsConsistent:   isConsistent,
				Direction:      direction,
				StartPrice:     startPoint.Price,
				EndPrice:       current.Price,
				VolChangePcnt:  volChangePcnt,
				VolChangeValue: volChangeValue,
			}
		}

		if endOI > 0 {
			results = append(results, AnalysisResult{
				Symbol:       symbol,
				StartOI:      startOI,
				EndOI:        endOI,
				CurrentPrice: current.Price,
				Volume24h:    current.Volume,
				Change1m:     calcChange(1 * time.Minute),
				Change10m:    calcChange(10 * time.Minute),
				Change1h:     calcChange(1 * time.Hour),
				Change4h:     calcChange(4 * time.Hour),
				Change24h:    calcChange(24 * time.Hour),
			})
		}
	}

	// Sort by 1h absolute change percentage (default sort)
	sort.Slice(results, func(i, j int) bool {
		return math.Abs(results[i].Change1h.ChangePcnt) > math.Abs(results[j].Change1h.ChangePcnt)
	})

	return results, nil
}

type ChartPoint struct {
	Time   time.Time `json:"time"`
	Price  float64   `json:"price"`
	OI     float64   `json:"oi"`
	Volume float64   `json:"volume"`
}

func (s *LogAnalyzerService) GetSymbolHistory(symbol string) ([]ChartPoint, error) {
	logDir := "logs/level_bot"
	files, err := os.ReadDir(logDir)
	if err != nil {
		return nil, fmt.Errorf("error reading log dir: %w", err)
	}

	// Get latest file
	var latestFile string
	for _, f := range files {
		if filepath.Ext(f.Name()) == ".jsonl" {
			latestFile = filepath.Join(logDir, f.Name())
		}
	}

	if latestFile == "" {
		return nil, fmt.Errorf("no log files found")
	}

	file, err := os.Open(latestFile)
	if err != nil {
		return nil, fmt.Errorf("error opening file: %w", err)
	}
	defer file.Close()

	var points []ChartPoint
	decoder := json.NewDecoder(file)

	for decoder.More() {
		var entry LogEntry
		if err := decoder.Decode(&entry); err != nil {
			continue
		}

		for _, coin := range entry.Data {
			if coin.Symbol == symbol {
				points = append(points, ChartPoint{
					Time:   entry.Time,
					Price:  coin.LastPrice,
					OI:     coin.OpenInterest,
					Volume: coin.Volume24h,
				})
				break // Found the symbol for this time step
			}
		}
	}

	return points, nil
}
