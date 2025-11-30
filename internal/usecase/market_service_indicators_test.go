package usecase_test

import (
	"context"
	"testing"

	"github.com/vitos/crypto_trade_level/internal/usecase"
)

func TestMarketService_Indicators(t *testing.T) {
	mockEx := &MockExchangeForService{}
	service := usecase.NewMarketService(mockEx)
	ctx := context.Background()

	// Ensure subscription callback is registered
	if mockEx.TradeCallback == nil {
		t.Fatal("MarketService did not subscribe to trades")
	}

	symbol := "BTCUSDT"

	// 1. Inject Trades
	// Buy 1 BTC @ 50000
	mockEx.TradeCallback(symbol, "Buy", 1.0, 50000)
	// Sell 0.5 BTC @ 50000
	mockEx.TradeCallback(symbol, "Sell", 0.5, 50000)

	// 2. Get Stats
	stats, err := service.GetMarketStats(ctx, symbol)
	if err != nil {
		t.Fatalf("GetMarketStats failed: %v", err)
	}

	// 3. Verify Indicators

	// SpeedBuy = 1.0 * 50000 = 50000
	if stats.SpeedBuy != 50000 {
		t.Errorf("Expected SpeedBuy 50000, got %f", stats.SpeedBuy)
	}
	// SpeedSell = 0.5 * 50000 = 25000
	if stats.SpeedSell != 25000 {
		t.Errorf("Expected SpeedSell 25000, got %f", stats.SpeedSell)
	}

	// CVD = SpeedBuy - SpeedSell (Accumulated)
	// CVD = 50000 - 25000 = 25000
	if stats.CVD != 25000 {
		t.Errorf("Expected CVD 25000, got %f", stats.CVD)
	}

	// TSI = Trades / 60 (trades/sec)
	// Trades = 2. TSI = 2 / 60 = 0.0333...
	expectedTSI := 2.0 / 60.0
	if stats.TSI != expectedTSI {
		t.Errorf("Expected TSI %f, got %f", expectedTSI, stats.TSI)
	}

	// GLI = SpeedSell / SpeedBuy = 25000 / 50000 = 0.5
	if stats.GLI != 0.5 {
		t.Errorf("Expected GLI 0.5, got %f", stats.GLI)
	}

	// TradeVelocity = (SpeedBuy + SpeedSell) / 60 = (75000) / 60 = 1250
	expectedVelocity := 75000.0 / 60.0
	if stats.TradeVelocity != expectedVelocity {
		t.Errorf("Expected TradeVelocity %f, got %f", expectedVelocity, stats.TradeVelocity)
	}

	// 4. Test GLI Max Cap
	// Reset service or use new symbol
	symbol2 := "ETHUSDT"
	// Sell 1 ETH @ 3000. No Buys.
	mockEx.TradeCallback(symbol2, "Sell", 1.0, 3000)

	stats2, _ := service.GetMarketStats(ctx, symbol2)
	if stats2.GLI != usecase.MaxGLI {
		t.Errorf("Expected GLI Max Cap %f, got %f", usecase.MaxGLI, stats2.GLI)
	}
}
