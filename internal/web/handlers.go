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
	CurrentPrice float64
	Side         domain.Side
	LongTiers    []float64
	ShortTiers   []float64
}

func (s *Server) handleLanding(w http.ResponseWriter, r *http.Request) {
	if err := templates.ExecuteTemplate(w, "landing.html", nil); err != nil {
		s.logger.Error("Template error", zap.Error(err))
		http.Error(w, "Internal Server Error", 500)
	}
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	// Fetch initial data
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
				Tier1Pct: 0.005,
				Tier2Pct: 0.003,
				Tier3Pct: 0.0015,
			}
		}

		side := evaluator.DetermineSide(l.LevelPrice, price)
		longTiers := evaluator.CalculateBoundaries(l, tiers, domain.SideLong)
		shortTiers := evaluator.CalculateBoundaries(l, tiers, domain.SideShort)

		views = append(views, LevelView{
			Level:        l,
			CurrentPrice: price,
			Side:         side,
			LongTiers:    longTiers,
			ShortTiers:   shortTiers,
		})
	}

	data := map[string]interface{}{
		"Levels": views,
	}

	if err := templates.ExecuteTemplate(w, "index.html", data); err != nil {
		s.logger.Error("Template error", zap.Error(err))
		http.Error(w, "Internal Server Error", 500)
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

		views = append(views, LevelView{
			Level:        l,
			CurrentPrice: price,
			Side:         side,
			LongTiers:    longTiers,
			ShortTiers:   shortTiers,
		})
	}

	if err := templates.ExecuteTemplate(w, "levels_table", views); err != nil {
		s.logger.Error("Template error", zap.Error(err))
		http.Error(w, "Internal Server Error", 500)
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

	exchange := r.FormValue("exchange")
	symbol := r.FormValue("symbol")
	marginType := r.FormValue("margin_type")
	stopLossAtBase := r.FormValue("stop_loss_at_base") == "on"

	level := &domain.Level{
		ID:             fmt.Sprintf("%d", time.Now().UnixNano()),
		Exchange:       exchange,
		Symbol:         symbol,
		LevelPrice:     price,
		BaseSize:       baseSize,
		Leverage:       leverage,
		MarginType:     marginType,
		CoolDownMs:     coolDownMs,
		StopLossAtBase: stopLossAtBase,
		Source:         "manual-web",
		CreatedAt:      time.Now(),
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
		http.Error(w, "Failed to delete level", 500)
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
		http.Error(w, "Failed to get positions", 500)
		return
	}

	if err := templates.ExecuteTemplate(w, "positions_table", positions); err != nil {
		s.logger.Error("Template error", zap.Error(err))
	}
}

func (s *Server) handleTradesTable(w http.ResponseWriter, r *http.Request) {
	trades, _ := s.tradeRepo.ListTrades(r.Context(), 50)
	if err := templates.ExecuteTemplate(w, "trades_table", trades); err != nil {
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
		http.Error(w, err.Error(), 500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(candles)
}
