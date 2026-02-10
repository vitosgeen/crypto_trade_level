package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/vitos/crypto_trade_level/internal/domain"
)

type LogEntry struct {
	Time time.Time         `json:"time"`
	Data []domain.CoinData `json:"data"`
}

type AnalysisResult struct {
	Symbol       string
	StartOI      float64
	EndOI        float64
	ChangePcnt   float64
	IsConsistent bool
	Datapoints   int
	Direction    string
}

func main() {
	logDir := "logs/level_bot"
	files, err := os.ReadDir(logDir)
	if err != nil {
		fmt.Printf("Error reading log dir: %v\n", err)
		return
	}

	// Get latest file
	var latestFile string
	for _, f := range files {
		if filepath.Ext(f.Name()) == ".jsonl" {
			latestFile = filepath.Join(logDir, f.Name())
		}
	}

	if latestFile == "" {
		fmt.Println("No log files found.")
		return
	}

	fmt.Printf("Analyzing file: %s\n", latestFile)

	file, err := os.Open(latestFile)
	if err != nil {
		fmt.Printf("Error opening file: %v\n", err)
		return
	}
	defer file.Close()

	// Map symbol -> list of (Time, OI)
	type Point struct {
		Time time.Time
		OI   float64
	}
	history := make(map[string][]Point)

	decoder := json.NewDecoder(file)
	for decoder.More() {
		var entry LogEntry
		if err := decoder.Decode(&entry); err != nil {
			fmt.Printf("Decode error: %v\n", err)
			continue
		}

		for _, coin := range entry.Data {
			history[coin.Symbol] = append(history[coin.Symbol], Point{
				Time: entry.Time,
				OI:   coin.OpenInterest,
			})
		}
	}

	var results []AnalysisResult

	for symbol, points := range history {
		if len(points) < 2 {
			continue
		}

		startOI := points[0].OI
		endOI := points[len(points)-1].OI

		if startOI == 0 {
			continue
		}

		changePcnt := ((endOI - startOI) / startOI) * 100

		// Check consistency
		upSteps := 0
		downSteps := 0
		totalSteps := 0

		for i := 1; i < len(points); i++ {
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

		if startOI > 0 {
			results = append(results, AnalysisResult{
				Symbol:       symbol,
				StartOI:      startOI,
				EndOI:        endOI,
				ChangePcnt:   changePcnt,
				IsConsistent: isConsistent,
				Datapoints:   len(points),
				Direction:    direction,
			})
		}
	}

	// Sort by absolute change percentage
	sort.Slice(results, func(i, j int) bool {
		return abs(results[i].ChangePcnt) > abs(results[j].ChangePcnt)
	})

	fmt.Printf("\nTop log variations (total analyzed: %d symbols):\n", len(results))
	fmt.Printf("%-20s | %-10s | %-15s | %-10s | %s\n", "Symbol", "Direction", "Change %", "Points", "Consistent?")
	fmt.Println("--------------------------------------------------------------------------------")

	count := 0
	for _, res := range results {
		if count >= 30 {
			break
		}
		consistentMark := ""
		if res.IsConsistent {
			consistentMark = "YES"
		}
		fmt.Printf("%-20s | %-10s | %-15.5f | %-10d | %s\n",
			res.Symbol, res.Direction, res.ChangePcnt, res.Datapoints, consistentMark)
		count++
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
