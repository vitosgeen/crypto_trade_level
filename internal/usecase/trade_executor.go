package usecase

import (
	"context"
	"fmt"

	"github.com/vitos/crypto_trade_level/internal/domain"
)

type TradeExecutor struct {
	exchange domain.Exchange
}

func NewTradeExecutor(exchange domain.Exchange) *TradeExecutor {
	return &TradeExecutor{
		exchange: exchange,
	}
}

func (e *TradeExecutor) Execute(ctx context.Context, symbol string, side domain.Side, size float64, leverage int, marginType string, stopLoss float64) error {
	if side == domain.SideLong {
		return e.exchange.MarketBuy(ctx, symbol, size, leverage, marginType, stopLoss)
	} else if side == domain.SideShort {
		return e.exchange.MarketSell(ctx, symbol, size, leverage, marginType, stopLoss)
	}
	return fmt.Errorf("invalid side: %s", side)
}
