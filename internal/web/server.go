package web

import (
	"context"
	"fmt"
	"net/http"

	"github.com/vitos/crypto_trade_level/internal/domain"
	"github.com/vitos/crypto_trade_level/internal/usecase"
	"go.uber.org/zap"
)

type Server struct {
	router        *http.ServeMux
	server        *http.Server
	levelRepo     domain.LevelRepository
	tradeRepo     domain.TradeRepository
	service       *usecase.LevelService
	marketService *usecase.MarketService
	logger        *zap.Logger
}

func NewServer(
	port int,
	levelRepo domain.LevelRepository,
	tradeRepo domain.TradeRepository,
	service *usecase.LevelService,
	marketService *usecase.MarketService,
	logger *zap.Logger,
) *Server {
	s := &Server{
		router:        http.NewServeMux(),
		levelRepo:     levelRepo,
		tradeRepo:     tradeRepo,
		service:       service,
		marketService: marketService,
		logger:        logger,
	}
	s.routes()
	s.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: s.router,
	}
	return s
}

func (s *Server) routes() {
	// Landing Page
	s.router.HandleFunc("GET /", s.handleLanding)

	// Dashboard
	s.router.HandleFunc("GET /dashboard", s.handleDashboard)

	// Levels
	s.router.HandleFunc("GET /levels", s.handleLevelsTable)
	s.router.HandleFunc("POST /levels", s.handleAddLevel)
	s.router.HandleFunc("DELETE /levels/{id}", s.handleDeleteLevel)

	// Tiers
	s.router.HandleFunc("POST /tiers", s.handleUpdateTiers)

	// Positions
	s.router.HandleFunc("GET /positions", s.handlePositionsTable)

	// Trades
	s.router.HandleFunc("GET /trades", s.handleTradesTable)

	// Liquidity
	s.router.HandleFunc("GET /api/liquidity", s.handleLiquidity)

	// Status
	s.router.HandleFunc("GET /status", s.handleStatus)

	// Candles
	s.router.HandleFunc("GET /api/candles", s.handleGetCandles)

	// Market Stats
	s.router.HandleFunc("GET /api/market-stats", s.handleMarketStats)
}

func (s *Server) Start() error {
	s.logger.Info("Starting web server", zap.String("addr", s.server.Addr))
	if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}
