package usecase

import (
	"context"
	"time"

	"github.com/vitos/crypto_trade_level/internal/domain"
	"go.uber.org/zap"
)

type WalletMonitor struct {
	exchange domain.Exchange
	repo     domain.WalletRepository
	log      *zap.Logger
}

func NewWalletMonitor(exchange domain.Exchange, repo domain.WalletRepository, log *zap.Logger) *WalletMonitor {
	return &WalletMonitor{
		exchange: exchange,
		repo:     repo,
		log:      log,
	}
}

func (w *WalletMonitor) Start(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Run immediately
	w.FetchAndSave(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.FetchAndSave(ctx)
		}
	}
}

func (w *WalletMonitor) FetchAndSave(ctx context.Context) {
	balances, err := w.exchange.GetWalletBalance(ctx)
	if err != nil {
		w.log.Error("Failed to fetch wallet balance", zap.Error(err))
		return
	}

	for _, b := range balances {
		if err := w.repo.SaveWalletBalance(ctx, b); err != nil {
			w.log.Error("Failed to save wallet balance", zap.String("coin", b.Coin), zap.Error(err))
		}
	}
	w.log.Info("Wallet balance updated", zap.Int("count", len(balances)))
}
