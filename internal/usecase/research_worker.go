package usecase

import (
	"context"
	"sort"
	"time"

	"go.uber.org/zap"
)

type ResearchWorker struct {
	service                *LevelService
	logger                 *zap.Logger
	refreshInterval        time.Duration
	currentResearchSymbols map[string]bool
}

func NewResearchWorker(service *LevelService, logger *zap.Logger, refreshInterval time.Duration) *ResearchWorker {
	if refreshInterval <= 0 {
		refreshInterval = 1 * time.Minute
	}
	return &ResearchWorker{
		service:                service,
		logger:                 logger,
		refreshInterval:        refreshInterval,
		currentResearchSymbols: make(map[string]bool),
	}
}

func (w *ResearchWorker) Start(ctx context.Context) {
	w.logger.Info("Starting Research Worker", zap.Duration("refresh_interval", w.refreshInterval))

	go func() {
		defer func() {
			if r := recover(); r != nil {
				w.logger.Error("ResearchWorker: Recovered from panic in loop", zap.Any("error", r))
				time.Sleep(5 * time.Second)
				w.Start(ctx) // Restart
			}
		}()

		ticker := time.NewTicker(w.refreshInterval)
		defer ticker.Stop()

		// Initial run
		w.collect(ctx)

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				w.collect(ctx)
			}
		}
	}()
}

func (w *ResearchWorker) collect(ctx context.Context) {
	// 1. Sync research symbols from DB
	researchSymbols, err := w.service.levelRepo.ListResearchSymbols(ctx)
	if err == nil {
		newSymbols := make(map[string]bool)
		for _, sym := range researchSymbols {
			newSymbols[sym] = true
		}

		// Detect removed symbols to unsubscribe
		for sym := range w.currentResearchSymbols {
			if !newSymbols[sym] {
				w.logger.Info("ResearchWorker: Unsubscribing from removed symbol", zap.String("symbol", sym))
				w.service.market.Unsubscribe(sym)
			}
		}

		w.currentResearchSymbols = newSymbols
	} else {
		w.logger.Error("ResearchWorker: Failed to list research symbols", zap.Error(err))
	}

	// 2. Fetch all tickers to find Top 5 OI
	allCoins, err := w.service.exchange.GetTickers(ctx, "linear")

	toMonitor := make(map[string]bool)
	for sym := range w.currentResearchSymbols {
		toMonitor[sym] = true
	}

	if err == nil {
		sort.Slice(allCoins, func(i, j int) bool {
			return allCoins[i].OpenInterest > allCoins[j].OpenInterest
		})
		// Add top 5 by OI
		for i := 0; i < 5 && i < len(allCoins); i++ {
			toMonitor[allCoins[i].Symbol] = true
		}
	} else {
		w.logger.Error("ResearchWorker: Failed to fetch tickers for Top OI", zap.Error(err))
	}

	// 3. Record metrics for all symbols in the list
	monitoredList := make([]string, 0, len(toMonitor))
	for sym := range toMonitor {
		w.service.RecordResearchMetrics(ctx, sym, nil, nil)
		monitoredList = append(monitoredList, sym)
	}

	w.logger.Info("ResearchWorker: Recorded metrics for research symbols", zap.Strings("monitored", monitoredList))
}
