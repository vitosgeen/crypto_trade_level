package web

import (
	"encoding/json"
	"net/http"

	"go.uber.org/zap"
)

func (s *Server) handleListLevelsJSON(w http.ResponseWriter, r *http.Request) {
	levels, err := s.levelRepo.ListLevels(r.Context())
	if err != nil {
		s.logger.Error("Failed to list levels", zap.Error(err))
		http.Error(w, "Failed to list levels", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(levels); err != nil {
		s.logger.Error("Failed to encode levels", zap.Error(err))
	}
}
