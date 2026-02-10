package usecase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/vitos/crypto_trade_level/internal/domain"
	"github.com/vitos/crypto_trade_level/internal/usecase"
)

// MockLevelRepo
type MockLevelRepo struct {
	Levels []*domain.Level
	Tiers  *domain.SymbolTiers
}

func (m *MockLevelRepo) SaveLevel(ctx context.Context, level *domain.Level) error { return nil }
func (m *MockLevelRepo) GetLevel(ctx context.Context, id string) (*domain.Level, error) {
	return m.Levels[0], nil
}
func (m *MockLevelRepo) ListLevels(ctx context.Context) ([]*domain.Level, error) {
	return m.Levels, nil
}
func (m *MockLevelRepo) DeleteLevel(ctx context.Context, id string) error { return nil }
func (m *MockLevelRepo) GetSymbolTiers(ctx context.Context, exchange, symbol string) (*domain.SymbolTiers, error) {
	return m.Tiers, nil
}
func (m *MockLevelRepo) SaveSymbolTiers(ctx context.Context, tiers *domain.SymbolTiers) error {
	return nil
}

func (m *MockLevelRepo) CountActiveLevels(ctx context.Context, symbol string) (int, error) {
	count := 0
	for _, l := range m.Levels {
		if l.Symbol == symbol {
			count++
		}
	}
	return count, nil
}

func (m *MockLevelRepo) GetLevelsBySymbol(ctx context.Context, symbol string) ([]*domain.Level, error) {
	var levels []*domain.Level
	for _, l := range m.Levels {
		if l.Symbol == symbol {
			levels = append(levels, l)
		}
	}
	return levels, nil
}

func (m *MockLevelRepo) SaveLiquiditySnapshot(ctx context.Context, snap *domain.LiquiditySnapshot) error {
	return nil
}
func (m *MockLevelRepo) ListLiquiditySnapshots(ctx context.Context, symbol string, limit int) ([]*domain.LiquiditySnapshot, error) {
	return nil, nil
}

// MockTradeRepo
type MockTradeRepo struct {
	LastTrade   *domain.Order
	LastHistory *domain.PositionHistory
}

func (m *MockTradeRepo) SaveTrade(ctx context.Context, trade *domain.Order) error {
	m.LastTrade = trade
	return nil
}
func (m *MockTradeRepo) ListTrades(ctx context.Context, limit int) ([]*domain.Order, error) {
	return nil, nil
}

func (m *MockTradeRepo) SavePositionHistory(ctx context.Context, history *domain.PositionHistory) error {
	m.LastHistory = history
	return nil
}

func (m *MockTradeRepo) ListPositionHistory(ctx context.Context, limit int) ([]*domain.PositionHistory, error) {
	return nil, nil
}

func (m *MockTradeRepo) SaveTradeSessionLog(ctx context.Context, log *domain.TradeSessionLog) error {
	return nil
}
func (m *MockTradeRepo) ListTradeSessionLogs(ctx context.Context, symbol string, limit int) ([]*domain.TradeSessionLog, error) {
	return nil, nil
}
func (m *MockTradeRepo) GetTradeSessionLog(ctx context.Context, id string) (*domain.TradeSessionLog, error) {
	return nil, nil
}

type MockExchange struct {
	BuyCalled    bool
	SellCalled   bool
	LastStopLoss float64
}

func (m *MockExchange) GetCurrentPrice(ctx context.Context, symbol string) (float64, error) {
	return 0, nil
}
func (m *MockExchange) MarketBuy(ctx context.Context, symbol string, size float64, leverage int, marginType string, stopLoss float64) error {
	m.BuyCalled = true
	m.LastStopLoss = stopLoss
	return nil
}
func (m *MockExchange) MarketSell(ctx context.Context, symbol string, size float64, leverage int, marginType string, stopLoss float64) error {
	m.SellCalled = true
	m.LastStopLoss = stopLoss
	return nil
}
func (m *MockExchange) ClosePosition(ctx context.Context, symbol string) error {
	return nil
}
func (m *MockExchange) GetPosition(ctx context.Context, symbol string) (*domain.Position, error) {
	return &domain.Position{Symbol: symbol, Size: 0}, nil
}
func (m *MockExchange) GetPositions(ctx context.Context) ([]*domain.Position, error) {
	return []*domain.Position{}, nil
}
func (m *MockExchange) GetOrderBook(ctx context.Context, symbol string, category string) (*domain.OrderBook, error) {
	return nil, nil
}
func (m *MockExchange) GetCandles(ctx context.Context, symbol, interval string, limit int) ([]domain.Candle, error) {
	return nil, nil
}
func (m *MockExchange) GetTickers(ctx context.Context, category string) ([]domain.Ticker, error) {
	return nil, nil
}

func (m *MockExchange) OnTradeUpdate(callback func(symbol string, side string, size float64, price float64)) {
}
func (m *MockExchange) GetRecentTrades(ctx context.Context, symbol string, limit int) ([]domain.PublicTrade, error) {
	return nil, nil
}
func (m *MockExchange) GetInstruments(ctx context.Context, category string) ([]domain.Instrument, error) {
	return nil, nil
}

func (m *MockExchange) Subscribe(symbols []string) error {
	return nil
}

func (m *MockExchange) PlaceOrder(ctx context.Context, order *domain.Order) (*domain.Order, error) {
	return order, nil
}

func (m *MockExchange) GetOrder(ctx context.Context, symbol, orderID string) (*domain.Order, error) {
	return &domain.Order{OrderID: orderID, Symbol: symbol, Status: "Filled"}, nil
}

func (m *MockExchange) CancelOrder(ctx context.Context, symbol, orderID string) error {
	return nil
}

func (m *MockExchange) GetWSStatus() domain.WSStatus {
	return domain.WSStatus{Connected: true}
}

func (m *MockExchangeForService) PlaceOrder(ctx context.Context, order *domain.Order) (*domain.Order, error) {
	return order, nil
}

func (m *MockExchangeForService) GetOrder(ctx context.Context, symbol, orderID string) (*domain.Order, error) {
	return &domain.Order{OrderID: orderID, Symbol: symbol, Status: "Filled"}, nil
}

func (m *MockExchangeForService) CancelOrder(ctx context.Context, symbol, orderID string) error {
	return nil
}

func TestLevelService_ClosePositionFailure_ResetsState(t *testing.T) {
	// Setup
	level := &domain.Level{
		ID:             "test-level",
		Symbol:         "BTCUSDT",
		Exchange:       "bybit",
		LevelPrice:     10000,
		BaseSize:       0.1,
		StopLossAtBase: true,
	}
	tiers := &domain.SymbolTiers{
		Tier1Pct: 0.005, // 10050
		Tier2Pct: 0.010,
		Tier3Pct: 0.015,
	}

	mockLevelRepo := &MockLevelRepo{Levels: []*domain.Level{level}, Tiers: tiers}
	mockTradeRepo := &MockTradeRepo{}
	mockEx := &MockExchangeForService{
		Position: &domain.Position{Size: 0},
	} // Reusing MockExchange from trade_executor_test.go if in same package, but we are in usecase_test

	// We need to define MockExchange here since we are in a new file/package (or same package test)
	// Let's redefine minimal mock

	// We need to define MockExchange here since we are in a new file/package (or same package test)
	// Let's redefine minimal mock

	marketService := usecase.NewMarketService(mockEx, mockLevelRepo)
	service := usecase.NewLevelService(mockLevelRepo, mockTradeRepo, mockEx, marketService)
	ctx := context.Background()

	// Populate Cache
	service.UpdateCache(ctx)

	// 1. Trigger Open (Tier 1)
	// Long Entry: Price falls from above Tier 1 (10050)
	// Init at 10100
	service.ProcessTick(ctx, "bybit", "BTCUSDT", 10100)

	// Second tick to trigger Open (Cross 10050 Downward)
	mockEx.BuyCalled = false
	service.ProcessTick(ctx, "bybit", "BTCUSDT", 10040)

	if !mockEx.BuyCalled {
		t.Fatal("Expected Buy on Tier 1")
	}
	// Simulate Position Opened
	mockEx.Position = &domain.Position{
		Symbol:     "BTCUSDT",
		Side:       domain.SideLong,
		Size:       0.1,
		EntryPrice: 10040,
	}

	// 2. Trigger Close (Base Level)
	// Price falls to 10000
	// Mock ClosePosition to FAIL
	mockEx.CloseError = errors.New("position not found")

	service.ProcessTick(ctx, "bybit", "BTCUSDT", 10000)

	// Mock that position is gone (consistent with "position not found")
	mockEx.Position = &domain.Position{Size: 0}
	mockEx.BuyCalled = false // Reset
	mockEx.CloseError = nil

	// 3. Trigger Open Again (Re-entry)
	// Must reset price above Tier 1 first to trigger "Cross Down"
	service.ProcessTick(ctx, "bybit", "BTCUSDT", 10100)

	// Trigger Open (Cross 10050 Downward)
	service.ProcessTick(ctx, "bybit", "BTCUSDT", 10040)

	if !mockEx.BuyCalled {
		t.Fatal("Expected Buy (Re-entry) after failed Close. State should have been reset.")
	}
}

// Enhanced MockExchange
type MockExchangeForService struct {
	BuyCalled    bool
	SellCalled   bool
	CloseCalled  bool
	LastStopLoss float64
	CloseError   error

	TradeCallback func(symbol string, side string, size float64, price float64)
	Position      *domain.Position
}

func (m *MockExchangeForService) GetCurrentPrice(ctx context.Context, symbol string) (float64, error) {
	return 0, nil
}
func (m *MockExchangeForService) MarketBuy(ctx context.Context, symbol string, size float64, leverage int, marginType string, stopLoss float64) error {
	m.BuyCalled = true
	m.LastStopLoss = stopLoss
	return nil
}
func (m *MockExchangeForService) MarketSell(ctx context.Context, symbol string, size float64, leverage int, marginType string, stopLoss float64) error {
	m.SellCalled = true
	m.LastStopLoss = stopLoss
	return nil
}
func (m *MockExchangeForService) ClosePosition(ctx context.Context, symbol string) error {
	m.CloseCalled = true
	return m.CloseError
}
func (m *MockExchangeForService) GetPosition(ctx context.Context, symbol string) (*domain.Position, error) {
	if m.Position != nil {
		return m.Position, nil
	}
	return &domain.Position{Symbol: symbol, Size: 0.1, Side: domain.SideLong, EntryPrice: 100}, nil // Default Long for Exit Test
}
func (m *MockExchangeForService) GetPositions(ctx context.Context) ([]*domain.Position, error) {
	if m.Position != nil && m.Position.Size > 0 {
		return []*domain.Position{m.Position}, nil
	}
	return []*domain.Position{}, nil
}
func (m *MockExchangeForService) GetCandles(ctx context.Context, symbol, interval string, limit int) ([]domain.Candle, error) {
	return nil, nil
}
func (m *MockExchangeForService) GetOrderBook(ctx context.Context, symbol string, category string) (*domain.OrderBook, error) {
	return nil, nil
}
func (m *MockExchangeForService) GetTickers(ctx context.Context, category string) ([]domain.Ticker, error) {
	return nil, nil
}

func (m *MockExchangeForService) OnTradeUpdate(callback func(symbol string, side string, size float64, price float64)) {
	m.TradeCallback = callback
}
func (m *MockExchangeForService) GetRecentTrades(ctx context.Context, symbol string, limit int) ([]domain.PublicTrade, error) {
	return nil, nil
}
func (m *MockExchangeForService) GetInstruments(ctx context.Context, category string) ([]domain.Instrument, error) {
	return nil, nil
}

func (m *MockExchangeForService) Subscribe(symbols []string) error {
	return nil
}

func (m *MockExchangeForService) GetWSStatus() domain.WSStatus {
	return domain.WSStatus{Connected: true}
}

func TestLevelService_StopLossMode(t *testing.T) {
	// Setup
	levelExchange := &domain.Level{
		ID:             "level-exchange",
		Symbol:         "BTCUSDT",
		Exchange:       "bybit",
		LevelPrice:     10000,
		BaseSize:       0.1,
		StopLossAtBase: true,
		StopLossMode:   "exchange",
	}
	levelApp := &domain.Level{
		ID:             "level-app",
		Symbol:         "ETHUSDT",
		Exchange:       "bybit",
		LevelPrice:     2000,
		BaseSize:       1.0,
		StopLossAtBase: true,
		StopLossMode:   "app",
	}

	tiers := &domain.SymbolTiers{
		Tier1Pct: 0.005,
		Tier2Pct: 0.010,
		Tier3Pct: 0.015,
	}

	mockLevelRepo := &MockLevelRepo{Levels: []*domain.Level{levelExchange, levelApp}, Tiers: tiers}
	mockTradeRepo := &MockTradeRepo{}
	mockEx := &MockExchange{}

	marketService := usecase.NewMarketService(mockEx, mockLevelRepo)
	service := usecase.NewLevelService(mockLevelRepo, mockTradeRepo, mockEx, marketService)
	ctx := context.Background()
	service.UpdateCache(ctx)

	// Case 1: Exchange Mode
	// Trigger Long Entry at Tier 1 (10050)
	// Price falls from above Tier 1
	service.ProcessTick(ctx, "bybit", "BTCUSDT", 10100) // Above
	service.ProcessTick(ctx, "bybit", "BTCUSDT", 10040) // Cross 10050 Downward

	if !mockEx.BuyCalled {
		t.Error("Expected Buy for Exchange Mode")
	}
	if mockEx.LastStopLoss != 10000 {
		t.Errorf("Expected StopLoss 10000 for Exchange Mode, got %f", mockEx.LastStopLoss)
	}

	// Reset Mock
	mockEx.BuyCalled = false
	mockEx.LastStopLoss = 0

	// Case 2: App Mode
	// Trigger Long Entry at Tier 1 (2010)
	// Price falls from above Tier 1
	service.ProcessTick(ctx, "bybit", "ETHUSDT", 2020) // Above
	service.ProcessTick(ctx, "bybit", "ETHUSDT", 2005) // Cross 2010 Downward

	if !mockEx.BuyCalled {
		t.Error("Expected Buy for App Mode")
	}

	if mockEx.LastStopLoss != 0 {
		t.Errorf("Expected StopLoss 0 for App Mode, got %f", mockEx.LastStopLoss)
	}
}

func TestLevelService_SentimentLogic(t *testing.T) {
	// Setup
	level := &domain.Level{
		ID:         "level-sentiment",
		Symbol:     "BTCUSDT",
		Exchange:   "bybit",
		LevelPrice: 10000,
		BaseSize:   0.1,
	}
	tiers := &domain.SymbolTiers{
		Tier1Pct: 0.005, // 10050
		Tier2Pct: 0.010,
		Tier3Pct: 0.015,
	}

	mockLevelRepo := &MockLevelRepo{Levels: []*domain.Level{level}, Tiers: tiers}
	mockTradeRepo := &MockTradeRepo{}
	mockEx := &MockExchangeForService{
		Position: &domain.Position{Size: 0},
	} // Use Enhanced Mock

	marketService := usecase.NewMarketService(mockEx, mockLevelRepo)
	service := usecase.NewLevelService(mockLevelRepo, mockTradeRepo, mockEx, marketService)
	ctx := context.Background()
	service.UpdateCache(ctx)

	// Ensure MockExchange captured the callback
	if mockEx.TradeCallback == nil {
		t.Fatal("MarketService did not subscribe to trades")
	}

	// 1. Inject Bearish Sentiment (Strong Sell)
	// Sell 100 BTC @ 10000 -> 1M Volume
	mockEx.TradeCallback("BTCUSDT", "Sell", 100, 10000)

	// Verify Sentiment
	sentiment, _ := marketService.GetTradeSentiment(ctx, "BTCUSDT")
	if sentiment != -1.0 {
		t.Fatalf("Expected Sentiment -1.0, got %f", sentiment)
	}

	// 2. Try to Open Long (Cross Tier 1 Downwards)
	// Level 10000. Tier 1 10050.
	service.ProcessTick(ctx, "bybit", "BTCUSDT", 10100) // Above
	service.ProcessTick(ctx, "bybit", "BTCUSDT", 10040) // Cross 10050

	if mockEx.BuyCalled {
		t.Error("Expected Long Entry to be BLOCKED by Bearish Sentiment")
	}

	// 3. Inject Bullish Sentiment (Flip to Strong Buy)
	// Buy 300 BTC @ 10000 -> 3M Volume. Total Buy 3M, Sell 1M. Net +2M. Total 4M. Sentiment +0.5.
	// Need more to reach > 0.6.
	// Buy 1000 BTC -> 10M. Total Buy 10M, Sell 1M. Net 9M. Total 11M. Sentiment ~0.81.
	mockEx.TradeCallback("BTCUSDT", "Buy", 1000, 10000)

	sentiment, _ = marketService.GetTradeSentiment(ctx, "BTCUSDT")
	if sentiment <= 0.6 {
		t.Fatalf("Expected Sentiment > 0.6, got %f", sentiment)
	}

	// 4. Try to Open Long Again (Reset Trigger first?)
	// The previous attempt didn't trigger the engine because it returned early?
	// No, `processLevel` returns early. `engine.Evaluate` was called?
	// Let's check `processLevel` logic.
	// `action, size := s.engine.Evaluate(...)`
	// `if action != ActionNone { ... if blocked return ... }`
	// So `engine` state WAS updated (Tier1Triggered = true).
	// So we can't re-trigger Tier 1 unless we reset.
	// But wait, if we blocked the trade, we shouldn't have updated the state?
	// `Evaluate` updates the state internally!
	// This is a side effect. If we block the trade, the state remains "Triggered" but no trade happened.
	// So the bot "missed" the trade. This is correct behavior for a filter.
	// To test Entry again, we need to trigger Tier 2 or reset state.
	// Let's trigger Tier 2 (10000 * 1.01 = 10100). Wait, Long Tiers are above.
	// T1: 10050. T2: 10100? No, Tiers are distances.
	// T1 0.5% -> 10050.
	// T2 1.0% -> 10100.
	// T3 1.5% -> 10150.
	// Wait, Long Tiers are usually closer to level?
	// "Tier1 is farthest, Tier3 closest."
	// T1 0.5% -> 10050.
	// T2 0.3% -> 10030.
	// T3 0.15% -> 10015.
	// My test setup: T1 0.005, T2 0.010, T3 0.015.
	// If T2 > T1, then T2 is FARTHER.
	// Let's assume standard config: T1 > T2 > T3.
	// But here I set T2=0.01 (1%). So T2 is 10100.
	// If Price is 10040. We crossed T1 (10050).
	// To cross T2 (10100), we need to go UP? No, Long Entry is usually on DIP.
	// Price comes from Above.
	// Crosses T1 (10050) -> 10040.
	// Crosses T2 (10030) -> 10020.
	// So T2 should be smaller pct if we want it closer to level.
	// But if I configured T2=0.01 (1%), then T2 is 10100.
	// So T2 is ABOVE T1.
	// If price is 10040, we are below T2.
	// Did we cross T2?
	// Prev 10100. Curr 10040.
	// T2 is 10100.
	// We touched T2.
	// `crosses` logic: (p1 < b && p2 >= b) || (p1 > b && p2 <= b).
	// 10100 > 10100 (False). 10040 <= 10100 (True).
	// So we might have crossed T2 too?
	// Let's reset state to be clean.
	// Accessing private engine is hard.
	// I'll just use a new level ID or reset mock.
	// Or I can just trigger Tier 2 if I configure it correctly.

	// Let's just create a new level for the second part.
	level2 := &domain.Level{
		ID:         "level-sentiment-2",
		Symbol:     "ETHUSDT",
		Exchange:   "bybit",
		LevelPrice: 2000,
		BaseSize:   1.0,
	}
	mockLevelRepo.Levels = append(mockLevelRepo.Levels, level2)
	service.UpdateCache(ctx)

	// Inject Bullish Sentiment for ETH too (same logic if symbol-agnostic? No, per symbol)
	mockEx.TradeCallback("ETHUSDT", "Buy", 1000, 2000)

	// Trigger Long Entry on ETH
	// T1 0.5% -> 2010.
	service.ProcessTick(ctx, "bybit", "ETHUSDT", 2020)
	service.ProcessTick(ctx, "bybit", "ETHUSDT", 2005) // Cross 2010

	if !mockEx.BuyCalled {
		t.Error("Expected Long Entry to be ALLOWED by Bullish Sentiment")
	}

	// 5. Test Exit Trigger
	// We have a Long Position on ETH (Manual Set)
	mockEx.Position = &domain.Position{
		Symbol:     "ETHUSDT",
		Side:       domain.SideLong,
		Size:       1.0,
		EntryPrice: 2000,
	}
	// Inject Bearish Sentiment for ETH
	mockEx.TradeCallback("ETHUSDT", "Sell", 5000, 2000) // Flip to Bearish

	// Reset CloseError
	mockEx.CloseError = nil

	// Process Tick (Price doesn't matter much, just needs to trigger check)
	service.ProcessTick(ctx, "bybit", "ETHUSDT", 2005)

	// MockExchange.ClosePosition should be called
	if !mockEx.CloseCalled {
		t.Error("Expected ClosePosition to be called due to Bearish Sentiment")
	}
}

func TestLevelService_DisableSpeedClose(t *testing.T) {
	// Setup
	level := &domain.Level{
		ID:                "level-disable-speed",
		Symbol:            "SOLUSDT",
		Exchange:          "bybit",
		LevelPrice:        20,
		BaseSize:          1.0,
		DisableSpeedClose: true, // ENABLED
	}
	tiers := &domain.SymbolTiers{
		Tier1Pct: 0.005,
		Tier2Pct: 0.010,
		Tier3Pct: 0.015,
	}

	mockLevelRepo := &MockLevelRepo{Levels: []*domain.Level{level}, Tiers: tiers}
	mockTradeRepo := &MockTradeRepo{}
	mockEx := &MockExchangeForService{}

	marketService := usecase.NewMarketService(mockEx, mockLevelRepo)
	service := usecase.NewLevelService(mockLevelRepo, mockTradeRepo, mockEx, marketService)
	ctx := context.Background()
	service.UpdateCache(ctx)

	// Ensure MockExchange captured the callback
	if mockEx.TradeCallback == nil {
		t.Fatal("MarketService did not subscribe to trades")
	}

	// 1. Inject Bearish Sentiment (Strong Sell)
	mockEx.TradeCallback("SOLUSDT", "Sell", 10000, 20)

	// 2. Verify Sentiment is Bearish
	sentiment, _ := marketService.GetTradeSentiment(ctx, "SOLUSDT")
	if sentiment != -1.0 {
		t.Fatalf("Expected Sentiment -1.0, got %f", sentiment)
	}

	// 3. Trigger Check (Price update)
	// We have a Long Position on SOL (from Mock GetPosition default)
	// Reset CloseCalled
	mockEx.CloseCalled = false

	service.ProcessTick(ctx, "bybit", "SOLUSDT", 20)

	// 4. Verify ClosePosition was NOT called
	if mockEx.CloseCalled {
		t.Error("Expected ClosePosition to be SKIPPED due to DisableSpeedClose=true")
	}
}

func TestLevelService_CheckSafety(t *testing.T) {
	// Setup
	level := &domain.Level{
		ID:         "level-safety",
		Symbol:     "BTCUSDT",
		Exchange:   "bybit",
		LevelPrice: 100, // Base Level
		BaseSize:   0.1,
	}
	tiers := &domain.SymbolTiers{
		Tier1Pct: 0.005,
		Tier2Pct: 0.010,
		Tier3Pct: 0.015,
	}

	mockLevelRepo := &MockLevelRepo{Levels: []*domain.Level{level}, Tiers: tiers}
	mockTradeRepo := &MockTradeRepo{}
	mockEx := &MockExchangeForService{} // Returns Long @ 100 by default

	marketService := usecase.NewMarketService(mockEx, mockLevelRepo)
	service := usecase.NewLevelService(mockLevelRepo, mockTradeRepo, mockEx, marketService)
	ctx := context.Background()
	service.UpdateCache(ctx)

	// 1. Safe Scenario
	// Level 100. Position Long @ 100. Price 105.
	// Should NOT Close.
	service.ProcessTick(ctx, "bybit", "BTCUSDT", 105) // Update LastPrice
	mockEx.CloseCalled = false

	service.CheckSafety(ctx)

	if mockEx.CloseCalled {
		t.Error("Expected Safety Check to PASS (No Close) when Price > Level for Long")
	}

	// 2. Unsafe Scenario
	// Level 100. Position Long @ 100. Price 95.
	// Should Close.
	service.ProcessTick(ctx, "bybit", "BTCUSDT", 95) // Update LastPrice
	mockEx.CloseCalled = false

	service.CheckSafety(ctx)

	if !mockEx.CloseCalled {
		t.Error("Expected Safety Check to FAIL (Close) when Price < Level for Long")
	}
}

func TestLevelService_TakeProfit(t *testing.T) {
	// Setup
	level := &domain.Level{
		ID:            "level-tp",
		Symbol:        "BTCUSDT",
		Exchange:      "bybit",
		LevelPrice:    10000,
		BaseSize:      0.1,
		TakeProfitPct: 0.02, // 2%
	}
	tiers := &domain.SymbolTiers{
		Tier1Pct: 0.005,
		Tier2Pct: 0.010,
		Tier3Pct: 0.015,
	}

	mockLevelRepo := &MockLevelRepo{Levels: []*domain.Level{level}, Tiers: tiers}
	mockTradeRepo := &MockTradeRepo{}
	mockEx := &MockExchangeForService{}

	marketService := usecase.NewMarketService(mockEx, mockLevelRepo)
	service := usecase.NewLevelService(mockLevelRepo, mockTradeRepo, mockEx, marketService)
	ctx := context.Background()
	service.UpdateCache(ctx)

	// 1. Long Position
	// Entry 10000. TP 2% -> 10200.
	mockEx.Position = &domain.Position{
		Symbol:     "BTCUSDT",
		Side:       domain.SideLong,
		Size:       0.1,
		EntryPrice: 10000,
	}

	// Price 10100 (Below TP)
	service.ProcessTick(ctx, "bybit", "BTCUSDT", 10100)
	if mockEx.CloseCalled {
		t.Error("Expected NO Close when Price < TP")
	}

	// Price 10200 (Hit TP)
	service.ProcessTick(ctx, "bybit", "BTCUSDT", 10200)
	if !mockEx.CloseCalled {
		t.Error("Expected Close when Price >= TP")
	}

	// Reset
	mockEx.CloseCalled = false

	// 2. Short Position
	// Entry 10000. TP 2% -> 9800.
	mockEx.Position = &domain.Position{
		Symbol:     "BTCUSDT",
		Side:       domain.SideShort,
		Size:       0.1,
		EntryPrice: 10000,
	}

	// Price 9900 (Above TP)
	service.ProcessTick(ctx, "bybit", "BTCUSDT", 9900)
	if mockEx.CloseCalled {
		t.Error("Expected NO Close when Price > TP (Short)")
	}

	// Price 9800 (Hit TP)
	service.ProcessTick(ctx, "bybit", "BTCUSDT", 9800)
	if !mockEx.CloseCalled {
		t.Error("Expected Close when Price <= TP (Short)")
	}
}

func TestLevelService_StopLossAtBase_Restart(t *testing.T) {
	// Setup
	level := &domain.Level{
		ID:             "level-sl-restart",
		Symbol:         "BTCUSDT",
		Exchange:       "bybit",
		LevelPrice:     10000,
		BaseSize:       0.1,
		StopLossAtBase: true,
	}
	tiers := &domain.SymbolTiers{
		Tier1Pct: 0.005,
		Tier2Pct: 0.010,
		Tier3Pct: 0.015,
	}

	mockLevelRepo := &MockLevelRepo{Levels: []*domain.Level{level}, Tiers: tiers}
	mockTradeRepo := &MockTradeRepo{}
	mockEx := &MockExchangeForService{}

	marketService := usecase.NewMarketService(mockEx, mockLevelRepo)
	// New Service -> Fresh Engine State (ActiveSide is empty)
	service := usecase.NewLevelService(mockLevelRepo, mockTradeRepo, mockEx, marketService)
	ctx := context.Background()
	service.UpdateCache(ctx)

	// 1. Simulate Existing LONG Position (from before restart)
	mockEx.Position = &domain.Position{
		Symbol:     "BTCUSDT",
		Side:       domain.SideLong,
		Size:       0.1,
		EntryPrice: 10050, // Entered above level
	}

	// 2. Price Drops BELOW Level (9990)
	// DetermineSide will return SideShort.
	// Engine has no ActiveSide.
	// Engine will treat this as Short Zone check.
	// Short SL check: Price >= Level? 9990 >= 10000 -> False.
	// BUG: It won't close.
	service.ProcessTick(ctx, "bybit", "BTCUSDT", 9990)

	if !mockEx.CloseCalled {
		t.Error("Expected Close when Price < Level for Long Position (Restart Scenario)")
	}
}

func TestLevelService_ManualClose(t *testing.T) {
	// Setup
	level := &domain.Level{
		ID:         "level-manual-close",
		Symbol:     "BTCUSDT",
		Exchange:   "bybit",
		LevelPrice: 10000,
		BaseSize:   0.1,
	}
	tiers := &domain.SymbolTiers{
		Tier1Pct: 0.005,
		Tier2Pct: 0.010,
		Tier3Pct: 0.015,
	}

	mockLevelRepo := &MockLevelRepo{Levels: []*domain.Level{level}, Tiers: tiers}
	mockTradeRepo := &MockTradeRepo{}
	mockEx := &MockExchangeForService{}

	marketService := usecase.NewMarketService(mockEx, mockLevelRepo)
	service := usecase.NewLevelService(mockLevelRepo, mockTradeRepo, mockEx, marketService)
	ctx := context.Background()
	service.UpdateCache(ctx)

	// 1. Simulate Active Position
	mockEx.Position = &domain.Position{
		Symbol:     "BTCUSDT",
		Side:       domain.SideLong,
		Size:       0.1,
		EntryPrice: 10000,
	}

	// 2. Call ClosePosition
	err := service.ClosePosition(ctx, "BTCUSDT")
	if err != nil {
		t.Fatalf("Expected ClosePosition to succeed, got %v", err)
	}

	// 3. Verify Exchange Close Called
	if !mockEx.CloseCalled {
		t.Error("Expected Exchange.ClosePosition to be called")
	}
}

func TestLevelService_PnLCalculation(t *testing.T) {
	// Setup
	level := &domain.Level{
		ID:         "level-pnl",
		Symbol:     "BTCUSDT",
		Exchange:   "bybit",
		LevelPrice: 10000,
		BaseSize:   0.1,
	}
	tiers := &domain.SymbolTiers{
		Tier1Pct: 0.005,
		Tier2Pct: 0.010,
		Tier3Pct: 0.015,
	}

	mockLevelRepo := &MockLevelRepo{Levels: []*domain.Level{level}, Tiers: tiers}
	mockTradeRepo := &MockTradeRepo{}
	mockEx := &MockExchangeForService{}

	marketService := usecase.NewMarketService(mockEx, mockLevelRepo)
	service := usecase.NewLevelService(mockLevelRepo, mockTradeRepo, mockEx, marketService)
	ctx := context.Background()
	service.UpdateCache(ctx)

	// 1. Simulate Active LONG Position
	// Entry: 10000. Size: 0.1.
	mockEx.Position = &domain.Position{
		Symbol:     "BTCUSDT",
		Side:       domain.SideLong,
		Size:       0.1,
		EntryPrice: 10000,
	}

	// 2. Mock Price Update to 11000 (Profit)
	service.ProcessTick(ctx, "bybit", "BTCUSDT", 11000)

	// 3. Call ClosePosition manually
	// This should trigger PnL calc: (11000 - 10000) * 0.1 = 1000 * 0.1 = 100
	err := service.ClosePosition(ctx, "BTCUSDT")
	if err != nil {
		t.Fatalf("Expected ClosePosition to succeed, got %v", err)
	}

	// 4. Verify Trade Saved with PnL
	if mockTradeRepo.LastTrade == nil {
		t.Fatal("Expected SaveTrade to be called")
	}

	expectedPnL := (11000.0 - 10000.0) * 0.1 // 100.0
	// Use a small epsilon for float comparison
	if mockTradeRepo.LastTrade.RealizedPnL < expectedPnL-0.001 || mockTradeRepo.LastTrade.RealizedPnL > expectedPnL+0.001 {
		t.Errorf("Expected RealizedPnL %f, got %f", expectedPnL, mockTradeRepo.LastTrade.RealizedPnL)
	}
}

func TestLevelService_PositionHistory(t *testing.T) {
	// Setup
	level := &domain.Level{
		ID:         "level-history",
		Symbol:     "ETHUSDT",
		Exchange:   "bybit",
		LevelPrice: 2000,
		BaseSize:   1.0,
	}
	tiers := &domain.SymbolTiers{
		Tier1Pct: 0.005,
		Tier2Pct: 0.010,
		Tier3Pct: 0.015,
	}

	mockLevelRepo := &MockLevelRepo{Levels: []*domain.Level{level}, Tiers: tiers}
	mockTradeRepo := &MockTradeRepo{}
	mockEx := &MockExchangeForService{}

	marketService := usecase.NewMarketService(mockEx, mockLevelRepo)
	service := usecase.NewLevelService(mockLevelRepo, mockTradeRepo, mockEx, marketService)
	ctx := context.Background()
	service.UpdateCache(ctx)

	// 1. Simulate Active SHORT Position
	// Entry: 2000. Size: 1.0.
	mockEx.Position = &domain.Position{
		Symbol:     "ETHUSDT",
		Side:       domain.SideShort,
		Size:       1.0,
		EntryPrice: 2000,
		Exchange:   "bybit",
	}

	// 2. Mock Price Update to 1900 (Profit)
	service.ProcessTick(ctx, "bybit", "ETHUSDT", 1900)

	// 3. Call ClosePosition manually
	// PnL: (2000 - 1900) * 1.0 = 100
	err := service.ClosePosition(ctx, "ETHUSDT")
	if err != nil {
		t.Fatalf("Expected ClosePosition to succeed, got %v", err)
	}

	// 4. Verify PositionHistory Saved
	if mockTradeRepo.LastHistory == nil {
		t.Fatal("Expected SavePositionHistory to be called")
	}

	if mockTradeRepo.LastHistory.Symbol != "ETHUSDT" {
		t.Errorf("Expected Symbol ETHUSDT, got %s", mockTradeRepo.LastHistory.Symbol)
	}
	if mockTradeRepo.LastHistory.Side != domain.SideShort {
		t.Errorf("Expected Side Short, got %s", mockTradeRepo.LastHistory.Side)
	}

	expectedPnL := 100.0
	if mockTradeRepo.LastHistory.RealizedPnL < expectedPnL-0.001 || mockTradeRepo.LastHistory.RealizedPnL > expectedPnL+0.001 {
		t.Errorf("Expected RealizedPnL %f, got %f", expectedPnL, mockTradeRepo.LastHistory.RealizedPnL)
	}

	// 5. Verify Trade Saved (Close marker)
	if mockTradeRepo.LastTrade == nil {
		t.Fatal("Expected SaveTrade to be called")
	}
	if mockTradeRepo.LastTrade.LevelID != "manual-close" {
		t.Errorf("Expected LevelID manual-close, got %s", mockTradeRepo.LastTrade.LevelID)
	}
}
