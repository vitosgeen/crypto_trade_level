package web

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/vitos/crypto_trade_level/internal/domain"
	"github.com/vitos/crypto_trade_level/internal/usecase"
	"go.uber.org/zap"
)

// Templates
var templates *template.Template

func InitTemplates(dir string) error {
	var err error
	funcMap := template.FuncMap{
		"mul": func(a, b float64) float64 {
			return a * b
		},
		"div": func(a, b float64) float64 {
			if b == 0 {
				return 0
			}
			return a / b
		},
	}
	templates, err = template.New("").Funcs(funcMap).ParseGlob(filepath.Join(dir, "*.html"))
	return err
}

type LevelView struct {
	*domain.Level
	CurrentPrice          float64
	Side                  domain.Side
	LongTiers             []float64
	ShortTiers            []float64
	ConsecutiveBaseCloses int
}

func (s *Server) handleLanding(w http.ResponseWriter, r *http.Request) {
	if err := templates.ExecuteTemplate(w, "landing.html", nil); err != nil {
		s.logger.Error("Template error", zap.Error(err))
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	// Fetch initial data
	levels, _ := s.levelRepo.ListLevels(r.Context())
	history, _ := s.tradeRepo.ListPositionHistory(r.Context(), 50)

	// Fetch all symbols for autocomplete
	allSymbols, _ := s.service.GetAllSymbols(r.Context())

	var views []LevelView
	evaluator := usecase.NewLevelEvaluator()

	for _, l := range levels {
		price := s.service.GetLatestPrice(l.Symbol)

		// Fetch tiers
		tiers, err := s.levelRepo.GetSymbolTiers(r.Context(), l.Exchange, l.Symbol)
		if err != nil || tiers == nil {
			// Defaults if not found
			tiers = &domain.SymbolTiers{
				Tier1Pct: 0.005,
				Tier2Pct: 0.003,
				Tier3Pct: 0.0015,
			}
		}

		side := evaluator.DetermineSide(l.LevelPrice, price)
		longTiers := evaluator.CalculateBoundaries(l, tiers, domain.SideLong)
		shortTiers := evaluator.CalculateBoundaries(l, tiers, domain.SideShort)

		// Get Runtime State
		state := s.service.GetLevelState(l.ID)

		views = append(views, LevelView{
			Level:                 l,
			CurrentPrice:          price,
			Side:                  side,
			LongTiers:             longTiers,
			ShortTiers:            shortTiers,
			ConsecutiveBaseCloses: state.ConsecutiveBaseCloses,
		})
	}

	data := map[string]interface{}{
		"Levels":     views,
		"History":    history,
		"AllSymbols": allSymbols,
	}

	if err := templates.ExecuteTemplate(w, "index.html", data); err != nil {
		s.logger.Error("Template error", zap.Error(err))
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func (s *Server) handleLevelsTable(w http.ResponseWriter, r *http.Request) {
	levels, _ := s.levelRepo.ListLevels(r.Context())

	var views []LevelView
	evaluator := usecase.NewLevelEvaluator()

	for _, l := range levels {
		price := s.service.GetLatestPrice(l.Symbol)

		// Fetch tiers
		tiers, err := s.levelRepo.GetSymbolTiers(r.Context(), l.Exchange, l.Symbol)
		if err != nil || tiers == nil {
			// Defaults if not found
			tiers = &domain.SymbolTiers{
				Tier1Pct: 0.001,
				Tier2Pct: 0.002,
				Tier3Pct: 0.003,
			}
		}

		side := evaluator.DetermineSide(l.LevelPrice, price)
		longTiers := evaluator.CalculateBoundaries(l, tiers, domain.SideLong)
		shortTiers := evaluator.CalculateBoundaries(l, tiers, domain.SideShort)

		// Get Runtime State
		state := s.service.GetLevelState(l.ID)

		views = append(views, LevelView{
			Level:                 l,
			CurrentPrice:          price,
			Side:                  side,
			LongTiers:             longTiers,
			ShortTiers:            shortTiers,
			ConsecutiveBaseCloses: state.ConsecutiveBaseCloses,
		})
	}

	if err := templates.ExecuteTemplate(w, "levels_table", views); err != nil {
		s.logger.Error("Template error", zap.Error(err))
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func (s *Server) handleAddLevel(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad Request", 400)
		return
	}

	price, _ := strconv.ParseFloat(r.FormValue("level_price"), 64)
	baseSize, _ := strconv.ParseFloat(r.FormValue("base_size"), 64)
	leverage, _ := strconv.Atoi(r.FormValue("leverage"))
	coolDownMs, _ := strconv.ParseInt(r.FormValue("cool_down_ms"), 10, 64)

	tier1, _ := strconv.ParseFloat(r.FormValue("tier1"), 64)
	tier2, _ := strconv.ParseFloat(r.FormValue("tier2"), 64)
	tier3, _ := strconv.ParseFloat(r.FormValue("tier3"), 64)

	// Convert percentage to decimal (e.g. 1.0 -> 0.01)
	tier1 = tier1 / 100
	tier2 = tier2 / 100
	tier3 = tier3 / 100

	takeProfitPct, _ := strconv.ParseFloat(r.FormValue("take_profit_pct"), 64)
	if takeProfitPct == 0 {
		takeProfitPct = 2.0 // Default
	}
	takeProfitPct = takeProfitPct / 100

	takeProfitMode := r.FormValue("take_profit_mode")
	if takeProfitMode == "" {
		takeProfitMode = "fixed"
	}

	exchange := r.FormValue("exchange")
	symbol := r.FormValue("symbol")
	marginType := r.FormValue("margin_type")
	stopLossAtBase := r.FormValue("stop_loss_at_base") == "on"
	stopLossMode := r.FormValue("stop_loss_mode")
	if stopLossMode == "" {
		stopLossMode = "exchange"
	}
	disableSpeedClose := r.FormValue("disable_speed_close") == "on"

	maxConsecutiveBaseCloses, _ := strconv.Atoi(r.FormValue("max_consecutive_base_closes"))
	baseCloseCooldownMinutes, _ := strconv.Atoi(r.FormValue("base_close_cooldown_minutes"))
	baseCloseCooldownMs := int64(baseCloseCooldownMinutes) * 60 * 1000
	autoModeEnabled := r.FormValue("auto_mode_enabled") == "on"

	level := &domain.Level{
		ID:                       fmt.Sprintf("%d", time.Now().UnixNano()),
		Exchange:                 exchange,
		Symbol:                   symbol,
		LevelPrice:               price,
		BaseSize:                 baseSize,
		Leverage:                 leverage,
		MarginType:               marginType,
		CoolDownMs:               coolDownMs,
		StopLossAtBase:           stopLossAtBase,
		StopLossMode:             stopLossMode,
		DisableSpeedClose:        disableSpeedClose,
		MaxConsecutiveBaseCloses: maxConsecutiveBaseCloses,
		BaseCloseCooldownMs:      baseCloseCooldownMs,
		TakeProfitPct:            takeProfitPct,
		TakeProfitMode:           takeProfitMode,
		IsAuto:                   false,
		AutoModeEnabled:          autoModeEnabled, // Enabled if checkbox checked
		Source:                   "manual-web",
		CreatedAt:                time.Now(),
	}

	if err := s.service.CreateLevel(r.Context(), level); err != nil {
		s.logger.Error("Failed to create level", zap.Error(err))
		http.Error(w, "Failed to save level", http.StatusInternalServerError)
		return
	}

	// Create Tiers
	tiers := &domain.SymbolTiers{
		Exchange:  exchange,
		Symbol:    symbol,
		Tier1Pct:  tier1,
		Tier2Pct:  tier2,
		Tier3Pct:  tier3,
		UpdatedAt: time.Now(),
	}
	if err := s.levelRepo.SaveSymbolTiers(r.Context(), tiers); err != nil {
		s.logger.Error("Failed to save tiers", zap.Error(err))
		// Continue, but log error
	}

	// Return updated table
	s.handleLevelsTable(w, r)
}

func (s *Server) handleDeleteLevel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.levelRepo.DeleteLevel(r.Context(), id); err != nil {
		s.logger.Error("Failed to delete level", zap.Error(err))
		http.Error(w, "Failed to delete level", http.StatusInternalServerError)
		return
	}
	s.handleLevelsTable(w, r)
}

func (s *Server) handleAutoCreateLevel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.service.AutoCreateNextLevel(r.Context(), id); err != nil {
		s.logger.Error("Failed to auto-create level", zap.Error(err))
		http.Error(w, fmt.Sprintf("Failed to auto-create level: %v", err), http.StatusInternalServerError)
		return
	}
	s.handleLevelsTable(w, r)
}

func (s *Server) handleUpdateTiers(w http.ResponseWriter, r *http.Request) {
	// Implementation for updating tiers
	// ...
}

func (s *Server) handlePositionsTable(w http.ResponseWriter, r *http.Request) {
	positions, err := s.service.GetPositions(r.Context())
	if err != nil {
		s.logger.Error("Failed to get positions", zap.Error(err))
		http.Error(w, "Failed to get positions", http.StatusInternalServerError)
		return
	}

	if err := templates.ExecuteTemplate(w, "positions_table", positions); err != nil {
		s.logger.Error("Template error", zap.Error(err))
	}
}

func (s *Server) handleIncrementCloses(w http.ResponseWriter, r *http.Request) {
	levelID := r.PathValue("id")
	if levelID == "" {
		http.Error(w, "Missing level ID", http.StatusBadRequest)
		return
	}

	if err := s.service.IncrementBaseCloses(r.Context(), levelID); err != nil {
		s.logger.Error("Error incrementing base closes", zap.Error(err))
		http.Error(w, "Failed to increment base closes", http.StatusInternalServerError)
		return
	}

	// Trigger table refresh
	w.Header().Set("HX-Trigger", "levelsUpdated")
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleClosePosition(w http.ResponseWriter, r *http.Request) {
	symbol := r.PathValue("symbol")
	if symbol == "" {
		http.Error(w, "Symbol is required", http.StatusBadRequest)
		return
	}

	if err := s.service.ClosePosition(r.Context(), symbol); err != nil {
		s.logger.Error("Failed to close position", zap.String("symbol", symbol), zap.Error(err))
		http.Error(w, fmt.Sprintf("Failed to close position: %v", err), http.StatusInternalServerError)
		return
	}

	// Return updated positions table
	s.handlePositionsTable(w, r)
}

func (s *Server) handleTradesTable(w http.ResponseWriter, r *http.Request) {
	trades, _ := s.tradeRepo.ListTrades(r.Context(), 50)
	if err := templates.ExecuteTemplate(w, "trades_table", trades); err != nil {
		s.logger.Error("Template error", zap.Error(err))
	}
}

func (s *Server) handleHistoryTable(w http.ResponseWriter, r *http.Request) {
	history, _ := s.tradeRepo.ListPositionHistory(r.Context(), 50)
	if err := templates.ExecuteTemplate(w, "history_table", history); err != nil {
		s.logger.Error("Template error", zap.Error(err))
	}
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	// Return status HTML
	w.Write([]byte("<div>System OK</div>"))
}

func (s *Server) handleGetCandles(w http.ResponseWriter, r *http.Request) {
	symbol := r.URL.Query().Get("symbol")
	interval := r.URL.Query().Get("interval")
	limitStr := r.URL.Query().Get("limit")

	if symbol == "" {
		symbol = "BTCUSDT"
	}
	if interval == "" {
		interval = "15" // 15 minutes default
	}
	limit := 200
	if limitStr != "" {
		l, err := strconv.Atoi(limitStr)
		if err == nil && l > 0 {
			limit = l
		}
	}

	candles, err := s.service.GetExchange().GetCandles(r.Context(), symbol, interval, limit)
	if err != nil {
		s.logger.Error("Failed to get candles", zap.Error(err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(candles)
}

func (s *Server) handleLiquidity(w http.ResponseWriter, r *http.Request) {
	symbol := r.URL.Query().Get("symbol")
	if symbol == "" {
		symbol = "BTCUSDT"
	}

	clusters, err := s.marketService.GetLiquidityClusters(r.Context(), symbol)
	if err != nil {
		s.logger.Error("Failed to get liquidity clusters", zap.Error(err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(clusters)
}

func (s *Server) handleLiquidityHistory(w http.ResponseWriter, r *http.Request) {
	symbol := r.URL.Query().Get("symbol")
	if symbol == "" {
		symbol = "BTCUSDT"
	}

	history := s.marketService.GetLiquidityHistory(symbol)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(history)
}

func (s *Server) handleMarketStats(w http.ResponseWriter, r *http.Request) {
	symbol := r.URL.Query().Get("symbol")
	if symbol == "" {
		symbol = "BTCUSDT"
	}

	stats, err := s.marketService.GetMarketStats(r.Context(), symbol)
	if err != nil {
		s.logger.Error("Failed to get market stats", zap.Error(err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

type CoinData struct {
	Symbol            string
	BaseCoin          string
	QuoteCoin         string
	Status            string
	LastPrice         float64
	Price24hPcnt      float64
	Volume24h         float64
	OpenInterest      float64
	OpenInterestValue float64
	Range10m          float64
	Range1h           float64
	Range4h           float64
	Trend10m          string // "up", "down", or ""
	Trend1h           string
	Trend4h           string
	FundingRate       float64
}

func (s *Server) handleLevelBot(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	instruments, err := s.service.GetExchange().GetInstruments(ctx, "linear")
	if err != nil {
		s.logger.Error("Failed to get instruments", zap.Error(err))
		http.Error(w, "Failed to fetch instruments", http.StatusInternalServerError)
		return
	}

	tickers, err := s.service.GetExchange().GetTickers(ctx, "linear")
	if err != nil {
		s.logger.Error("Failed to get tickers", zap.Error(err))
		http.Error(w, "Failed to fetch tickers", http.StatusInternalServerError)
		return
	}

	// Map tickers by symbol for easy lookup
	tickerMap := make(map[string]domain.Ticker)
	for _, t := range tickers {
		tickerMap[t.Symbol] = t
	}

	var allCoins []CoinData
	for _, inst := range instruments {
		if inst.Status != "Trading" {
			continue
		}
		t, ok := tickerMap[inst.Symbol]
		coin := CoinData{
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
			candles10m, _ := s.service.GetExchange().GetCandles(ctx, symbol, "1", 10)
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
			candles1h, _ := s.service.GetExchange().GetCandles(ctx, symbol, "1", 60)
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
			candles4h, _ := s.service.GetExchange().GetCandles(ctx, symbol, "60", 4)
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
					// Trend: Current vs Start of range
					startPrice := candles4h[0].Open
					if allCoins[idx].LastPrice > startPrice {
						allCoins[idx].Trend4h = "up"
					} else if allCoins[idx].LastPrice < startPrice {
						allCoins[idx].Trend4h = "down"
					}
				}
			}
		}(i)
	}

	wg.Wait()

	data := map[string]interface{}{
		"Instruments": allCoins,
	}

	if err := templates.ExecuteTemplate(w, "level_coins.html", data); err != nil {
		s.logger.Error("Template error", zap.Error(err))
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func (s *Server) handleSpeedBot(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	instruments, err := s.service.GetExchange().GetInstruments(ctx, "linear")
	if err != nil {
		s.logger.Error("Failed to get instruments", zap.Error(err))
		http.Error(w, "Failed to fetch instruments", http.StatusInternalServerError)
		return
	}

	tickers, err := s.service.GetExchange().GetTickers(ctx, "linear")
	if err != nil {
		s.logger.Error("Failed to get tickers", zap.Error(err))
		http.Error(w, "Failed to fetch tickers", http.StatusInternalServerError)
		return
	}

	// Map tickers by symbol for easy lookup
	tickerMap := make(map[string]domain.Ticker)
	for _, t := range tickers {
		tickerMap[t.Symbol] = t
	}

	var coins []CoinData
	for _, inst := range instruments {
		t, ok := tickerMap[inst.Symbol]
		coin := CoinData{
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
		}
		coins = append(coins, coin)
	}

	data := map[string]interface{}{
		"Instruments": coins,
	}

	if err := templates.ExecuteTemplate(w, "coins.html", data); err != nil {
		s.logger.Error("Template error", zap.Error(err))
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

type FundingCoinData struct {
	Symbol          string
	FundingRate     float64
	AbsFundingRate  float64
	NextFundingTime int64
	BotRunning      bool
}

func (s *Server) handleFundingBot(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tickers, err := s.service.GetExchange().GetTickers(ctx, "linear")
	if err != nil {
		s.logger.Error("Failed to get tickers", zap.Error(err))
		http.Error(w, "Failed to fetch tickers", http.StatusInternalServerError)
		return
	}

	var coins []FundingCoinData
	for _, t := range tickers {
		// Filter non-zero funding rates (or active instruments)
		// We can filter by FundingRate != 0, but sometimes it's very small.
		// Let's include everything for now, or maybe filter very small ones?
		// User said "page for symbols with funding rate + - not zero".
		if t.FundingRate != 0 {
			absRate := t.FundingRate
			if absRate < 0 {
				absRate = -absRate
			}
			coins = append(coins, FundingCoinData{
				Symbol:      t.Symbol,
				FundingRate: t.FundingRate * 100, // Convert to percentage for display if needed, but template handles it. Wait, template uses %.4f%%, so it expects decimal? Usually API returns 0.0001 for 0.01%.
				// Let's check Bybit API. Funding Rate is e.g. "0.0001".
				// So 0.0001 * 100 = 0.01%.
				// The template uses {{printf "%.4f%%" .FundingRate}}.
				// If I pass 0.0001, it prints "0.0001%". That's wrong. It should be "0.0100%".
				// So I should multiply by 100 here.
				AbsFundingRate:  absRate * 100,
				NextFundingTime: t.NextFundingTime,
				BotRunning:      s.fundingBotService.IsBotRunning(t.Symbol),
			})
		}
	}

	data := map[string]interface{}{
		"Instruments":        coins,
		"AutoScannerRunning": s.fundingBotService.IsAutoScannerRunning(),
	}

	if err := templates.ExecuteTemplate(w, "funding_coins.html", data); err != nil {
		s.logger.Error("Template error", zap.Error(err))
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func (s *Server) handleCoinDetail(w http.ResponseWriter, r *http.Request) {
	symbol := r.PathValue("symbol")
	if symbol == "" {
		http.Error(w, "Symbol is required", http.StatusBadRequest)
		return
	}

	// Determine bot type from URL
	if strings.Contains(r.URL.Path, "/funding-bot/") {
		s.handleFundingCoinDetail(w, r, symbol)
	} else {
		s.handleSpeedCoinDetail(w, r, symbol)
	}
}

func (s *Server) handleSpeedCoinDetail(w http.ResponseWriter, r *http.Request, symbol string) {
	// Fetch market stats for the symbol
	stats, err := s.marketService.GetMarketStats(r.Context(), symbol)
	if err != nil {
		s.logger.Error("Failed to get market stats", zap.Error(err))
		stats = &usecase.MarketStats{} // Use empty stats on error
	}

	data := map[string]interface{}{
		"Symbol": symbol,
		"Stats":  stats,
	}

	if err := templates.ExecuteTemplate(w, "coin_detail.html", data); err != nil {
		s.logger.Error("Template error", zap.Error(err))
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func (s *Server) handleFundingCoinDetail(w http.ResponseWriter, r *http.Request, symbol string) {
	// Fetch market stats for the symbol
	stats, err := s.marketService.GetMarketStats(r.Context(), symbol)
	if err != nil {
		s.logger.Error("Failed to get market stats", zap.Error(err))
		stats = &usecase.MarketStats{} // Use empty stats on error
	}

	// Get funding time
	var nextFundingTime int64
	tickers, err := s.service.GetExchange().GetTickers(r.Context(), "linear")
	if err == nil {
		for _, t := range tickers {
			if t.Symbol == symbol {
				nextFundingTime = t.NextFundingTime
				break
			}
		}
	} else {
		s.logger.Error("Failed to get tickers for funding time", zap.Error(err))
	}

	data := map[string]interface{}{
		"Symbol":          symbol,
		"Stats":           stats,
		"NextFundingTime": nextFundingTime,
	}

	if err := templates.ExecuteTemplate(w, "funding_coin_detail.html", data); err != nil {
		s.logger.Error("Template error", zap.Error(err))
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// Speed Bot API Handlers

func (s *Server) handleStartSpeedBot(w http.ResponseWriter, r *http.Request) {
	// Decode into a temporary struct to handle cooldown as integer ms
	type SpeedBotConfigRequest struct {
		Symbol       string  `json:"symbol"`
		PositionSize float64 `json:"position_size"`
		Leverage     int     `json:"leverage"`
		MarginType   string  `json:"margin_type"`
		CooldownMs   int64   `json:"cooldown"` // Read as integer ms
	}

	var req SpeedBotConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.Symbol == "" {
		http.Error(w, "Symbol is required", http.StatusBadRequest)
		return
	}
	if req.PositionSize <= 0 {
		http.Error(w, "PositionSize must be greater than 0", http.StatusBadRequest)
		return
	}
	if req.Leverage <= 0 {
		http.Error(w, "Leverage must be greater than 0", http.StatusBadRequest)
		return
	}
	if req.MarginType == "" {
		http.Error(w, "MarginType is required", http.StatusBadRequest)
		return
	}

	config := usecase.SpeedBotConfig{
		Symbol:       req.Symbol,
		PositionSize: req.PositionSize,
		Leverage:     req.Leverage,
		MarginType:   req.MarginType,
		Cooldown:     time.Duration(req.CooldownMs) * time.Millisecond,
	}

	if err := s.speedBotService.StartBot(r.Context(), config); err != nil {
		s.logger.Error("Failed to start speed bot", zap.Error(err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "started"})
}

func (s *Server) handleStopSpeedBot(w http.ResponseWriter, r *http.Request) {
	symbol := r.URL.Query().Get("symbol")
	if symbol == "" {
		http.Error(w, "symbol parameter required", http.StatusBadRequest)
		return
	}

	if err := s.speedBotService.StopBot(symbol); err != nil {
		s.logger.Error("Failed to stop speed bot", zap.Error(err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "stopped"})
}

func (s *Server) handleSpeedBotStatus(w http.ResponseWriter, r *http.Request) {
	symbol := r.URL.Query().Get("symbol")
	if symbol == "" {
		http.Error(w, "symbol parameter required", http.StatusBadRequest)
		return
	}

	status, err := s.speedBotService.GetBotStatus(r.Context(), symbol)
	if err != nil {
		s.logger.Error("Failed to get speed bot status", zap.Error(err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}
