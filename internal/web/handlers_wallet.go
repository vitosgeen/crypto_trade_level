package web

import (
	"encoding/json"
	"net/http"
	"strconv"

	"go.uber.org/zap"
)

func (s *Server) handleWalletPage(w http.ResponseWriter, r *http.Request) {
	if err := templates.ExecuteTemplate(w, "wallet.html", nil); err != nil {
		s.logger.Error("Template error", zap.Error(err))
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func (s *Server) handleWalletHistory(w http.ResponseWriter, r *http.Request) {
	coin := r.URL.Query().Get("coin")
	if coin == "" {
		coin = "USDT" // Default
	}

	limitStr := r.URL.Query().Get("limit")
	limit := 100
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
		limit = l
	}

	if s.walletRepo == nil {
		http.Error(w, "Wallet repository not initialized", http.StatusInternalServerError)
		return
	}

	balances, err := s.walletRepo.GetWalletBalanceHistory(r.Context(), coin, limit)
	if err != nil {
		s.logger.Error("Failed to get wallet history", zap.Error(err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(balances)
}
