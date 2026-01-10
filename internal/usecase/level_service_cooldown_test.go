package usecase_test

import (
	"context"
	"testing"
	"time"

	"github.com/vitos/crypto_trade_level/internal/domain"
	"github.com/vitos/crypto_trade_level/internal/usecase"
)

func TestLevelService_CooldownLogic(t *testing.T) {
	// Setup
	mockRepo := &MockLevelRepo{
		Levels: make([]*domain.Level, 0),
		Tiers:  &domain.SymbolTiers{},
	}
	mockTradeRepo := &MockTradeRepo{}
	mockExchange := &MockExchangeForService{} // Use Enhanced Mock
	mockMarket := usecase.NewMarketService(mockExchange, mockRepo)

	service := usecase.NewLevelService(mockRepo, mockTradeRepo, mockExchange, mockMarket)

	// Create a level with cooldown config
	level := &domain.Level{
		ID:                       "level-cooldown-test",
		Exchange:                 "bybit",
		Symbol:                   "BTCUSDT",
		LevelPrice:               50000,
		BaseSize:                 0.1,
		Leverage:                 10,
		MarginType:               "isolated",
		CoolDownMs:               0,
		StopLossAtBase:           true,
		StopLossMode:             "exchange",
		MaxConsecutiveBaseCloses: 2,
		BaseCloseCooldownMs:      100, // 100ms for fast test
		CreatedAt:                time.Now(),
	}
	mockRepo.Levels = append(mockRepo.Levels, level)

	tiers := &domain.SymbolTiers{
		Exchange: "bybit",
		Symbol:   "BTCUSDT",
		Tier1Pct: 0.001, // 50050
		Tier2Pct: 0.002,
		Tier3Pct: 0.003,
	}
	mockRepo.Tiers = tiers

	service.UpdateCache(context.Background())

	// Helper to simulate a tick sequence
	// 1. Trigger Long (Price drops to Tier 1)
	// 2. Close at Base (Price rises to Level)

	ctx := context.Background()

	// --- Cycle 1 ---
	// Initial Price: 50100 (Above Level)
	service.ProcessTick(ctx, "bybit", "BTCUSDT", 50100)

	// Drop to 50040 (Crosses Tier 1 50050 Downwards) -> OPEN LONG
	service.ProcessTick(ctx, "bybit", "BTCUSDT", 50040)

	// Verify Position Opened
	// We can't access getPosition directly as it is private and we are in usecase_test
	// But we can check MockExchange.Position or BuyCalled
	if !mockExchange.BuyCalled {
		t.Fatalf("Cycle 1: Failed to open position (Buy not called)")
	}
	// Simulate Position creation in Mock
	mockExchange.Position = &domain.Position{
		Symbol:     "BTCUSDT",
		Side:       domain.SideLong,
		Size:       0.1,
		EntryPrice: 50040,
	}

	// Rise to 50000 (Level Price) -> STOP LOSS AT BASE
	// This should increment ConsecutiveBaseCloses to 1
	service.ProcessTick(ctx, "bybit", "BTCUSDT", 50000)

	// Verify Position Closed
	if !mockExchange.CloseCalled {
		t.Fatalf("Cycle 1: Failed to close position (Close not called)")
	}
	// Simulate Close
	mockExchange.Position = nil
	mockExchange.BuyCalled = false // Reset for next cycle
	mockExchange.CloseCalled = false

	// Check State
	// We can't access service.engine directly.
	// But we can verify behavior. If state is correct, next cycle should work.
	// If ConsecutiveBaseCloses is 1, nothing special happens yet.

	// --- Cycle 2 ---
	// Reset Price to above
	service.ProcessTick(ctx, "bybit", "BTCUSDT", 50100)

	// Drop to 50040 -> OPEN LONG again
	service.ProcessTick(ctx, "bybit", "BTCUSDT", 50040)

	if !mockExchange.BuyCalled {
		t.Fatalf("Cycle 2: Failed to open position")
	}
	mockExchange.Position = &domain.Position{
		Symbol:     "BTCUSDT",
		Side:       domain.SideLong,
		Size:       0.1,
		EntryPrice: 50040,
	}

	// Rise to 50000 -> STOP LOSS AT BASE
	// This should increment ConsecutiveBaseCloses to 2 -> TRIGGER COOLDOWN
	service.ProcessTick(ctx, "bybit", "BTCUSDT", 50000)

	if !mockExchange.CloseCalled {
		t.Fatalf("Cycle 2: Failed to close position")
	}
	mockExchange.Position = nil
	mockExchange.BuyCalled = false
	mockExchange.CloseCalled = false

	// Check State
	// Now we expect Cooldown.

	// --- Cycle 3 (During Cooldown) ---
	// Reset Price
	service.ProcessTick(ctx, "bybit", "BTCUSDT", 50100)

	// Drop to 50040 -> SHOULD NOT OPEN
	service.ProcessTick(ctx, "bybit", "BTCUSDT", 50040)

	// Verify NO Position
	if mockExchange.BuyCalled {
		t.Fatalf("Cycle 3: Opened position during cooldown!")
	}

	// --- Cycle 4 (After Cooldown) ---
	time.Sleep(150 * time.Millisecond)

	// Reset Price to above to allow cross detection
	service.ProcessTick(ctx, "bybit", "BTCUSDT", 50100)

	// Drop to 50040 -> SHOULD OPEN
	service.ProcessTick(ctx, "bybit", "BTCUSDT", 50040)

	if !mockExchange.BuyCalled {
		t.Fatalf("Cycle 4: Failed to open position after cooldown")
	}
}
