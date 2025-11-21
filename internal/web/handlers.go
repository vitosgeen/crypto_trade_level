package web

import (
	"fmt"
	"html/template"
	"net/http"
	"path/filepath"
	"strconv"
	"time"

	"github.com/vitos/crypto_trade_level/internal/domain"
	"go.uber.org/zap"
)

// Templates
var templates *template.Template

func InitTemplates(dir string) error {
	var err error
	templates, err = template.ParseGlob(filepath.Join(dir, "*.html"))
	return err
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	// Fetch initial data
	levels, _ := s.levelRepo.ListLevels(r.Context())

	type LevelView struct {
		*domain.Level
		CurrentPrice float64
	}

	var views []LevelView
	for _, l := range levels {
		price := s.service.GetLatestPrice(l.Symbol)
		views = append(views, LevelView{Level: l, CurrentPrice: price})
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

	type LevelView struct {
		*domain.Level
		CurrentPrice float64
	}

	var views []LevelView
	for _, l := range levels {
		price := s.service.GetLatestPrice(l.Symbol)
		views = append(views, LevelView{Level: l, CurrentPrice: price})
	}

	if err := templates.ExecuteTemplate(w, "levels_table", views); err != nil {
		s.logger.Error("Template error", zap.Error(err))
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

	exchange := r.FormValue("exchange")
	symbol := r.FormValue("symbol")
	marginType := r.FormValue("margin_type")

	level := &domain.Level{
		ID:         fmt.Sprintf("%d", time.Now().UnixNano()),
		Exchange:   exchange,
		Symbol:     symbol,
		LevelPrice: price,
		BaseSize:   baseSize,
		Leverage:   leverage,
		MarginType: marginType,
		CoolDownMs: coolDownMs,
		Source:     "manual-web",
		CreatedAt:  time.Now(),
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
