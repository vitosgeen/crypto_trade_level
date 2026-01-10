package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/vitos/crypto_trade_level/internal/usecase"
	"go.uber.org/zap"
)

// Funding Bot API Handlers

func (s *Server) handleStartFundingBot(w http.ResponseWriter, r *http.Request) {
	// Decode into a temporary struct to handle countdown as integer seconds
	type FundingBotConfigRequest struct {
		Symbol                  string  `json:"symbol"`
		PositionSize            float64 `json:"position_size"`
		Leverage                int     `json:"leverage"`
		MarginType              string  `json:"margin_type"`
		CountdownThreshold      int64   `json:"countdown_threshold"` // Seconds before funding
		MinFundingRate          float64 `json:"min_funding_rate"`
		WallCheckEnabled        bool    `json:"wall_check_enabled"`
		WallThresholdMultiplier float64 `json:"wall_threshold_multiplier"`
		StopLossPercentage      float64 `json:"stop_loss_percentage"`
		TakeProfitPercentage    float64 `json:"take_profit_percentage"`
	}

	var req FundingBotConfigRequest
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

	// Set defaults
	if req.CountdownThreshold == 0 {
		req.CountdownThreshold = 60 // Default 60 seconds
	}
	if req.MinFundingRate == 0 {
		req.MinFundingRate = 0.0001 // Default 0.01%
	}

	config := usecase.FundingBotConfig{
		Symbol:                  req.Symbol,
		PositionSize:            req.PositionSize,
		Leverage:                req.Leverage,
		MarginType:              req.MarginType,
		CountdownThreshold:      time.Duration(req.CountdownThreshold) * time.Second,
		MinFundingRate:          req.MinFundingRate,
		WallCheckEnabled:        req.WallCheckEnabled,
		WallThresholdMultiplier: req.WallThresholdMultiplier,
		StopLossPercentage:      req.StopLossPercentage,
		TakeProfitPercentage:    req.TakeProfitPercentage,
	}

	if err := s.fundingBotService.StartBot(r.Context(), config); err != nil {
		s.logger.Error("Failed to start funding bot", zap.Error(err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "started"})
}

func (s *Server) handleStopFundingBot(w http.ResponseWriter, r *http.Request) {
	symbol := r.URL.Query().Get("symbol")
	if symbol == "" {
		http.Error(w, "symbol parameter required", http.StatusBadRequest)
		return
	}

	if err := s.fundingBotService.StopBot(symbol); err != nil {
		s.logger.Error("Failed to stop funding bot", zap.Error(err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "stopped"})
}

func (s *Server) handleFundingBotStatus(w http.ResponseWriter, r *http.Request) {
	symbol := r.URL.Query().Get("symbol")
	if symbol == "" {
		http.Error(w, "symbol parameter required", http.StatusBadRequest)
		return
	}

	status, err := s.fundingBotService.GetBotStatus(r.Context(), symbol)
	if err != nil {
		s.logger.Error("Failed to get funding bot status", zap.Error(err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

func (s *Server) handleTestFundingBot(w http.ResponseWriter, r *http.Request) {
	symbol := r.URL.Query().Get("symbol")
	if symbol == "" {
		http.Error(w, "symbol parameter required", http.StatusBadRequest)
		return
	}

	if err := s.fundingBotService.TriggerTestEvent(r.Context(), symbol); err != nil {
		s.logger.Error("Test funding event failed", zap.Error(err))
		http.Error(w, fmt.Sprintf("Test event failed: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "test_triggered"})
}

func (s *Server) handleStartAutoScanner(w http.ResponseWriter, r *http.Request) {
	go s.fundingBotService.StartAutoScanner(context.Background())
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "auto_scanner_started"})
}

func (s *Server) handleStopAutoScanner(w http.ResponseWriter, r *http.Request) {
	s.fundingBotService.StopAutoScanner()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "auto_scanner_stopped"})
}

func (s *Server) handleGetAutoScannerStatus(w http.ResponseWriter, r *http.Request) {
	status := "stopped"
	if s.fundingBotService.IsAutoScannerRunning() {
		status = "running"
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": status})
}
