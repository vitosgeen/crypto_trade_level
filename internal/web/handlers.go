package web

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"path/filepath"
	"strconv"
	"time"

	"github.com/vitos/crypto_trade_level/internal/domain"
	"github.com/vitos/crypto_trade_level/internal/usecase"
	"go.uber.org/zap"
)

// Templates
var templates *template.Template

func InitTemplates(dir string) error {
	var err error
	templates, err = template.ParseGlob(filepath.Join(dir, "*.html"))
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
		"Levels":  views,
		"History": history,
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
		AutoModeEnabled:          false, // Disabled by default, manual trigger only
		Source:                   "manual-web",
		CreatedAt:                time.Now(),
	}

	if err := s.levelRepo.SaveLevel(r.Context(), level); err != nil {
		s.logger.Error("Failed to save level", zap.Error(err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Save Tiers
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

func (s *Server) handleCoinDetail(w http.ResponseWriter, r *http.Request) {
	symbol := r.PathValue("symbol")
	if symbol == "" {
		http.Error(w, "Symbol is required", http.StatusBadRequest)
		return
	}

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
