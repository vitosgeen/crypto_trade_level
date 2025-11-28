@Copilot commented on this pull request.
Pull request overview

This pull request implements a take profit feature and moves stop loss at base logic from the engine to the service layer. The PR adds position caching, fixes trigger direction logic for tier entries, and includes extensive UI improvements.

Key Changes:

    Adds configurable take profit percentage feature with database schema updates
    Moves stop loss at base logic from SublevelEngine to LevelService for better position state awareness
    Implements position caching with 1-second TTL to reduce exchange API calls
    Fixes tier trigger direction (short entries trigger on price rise, long entries on price fall)

Reviewed changes

Copilot reviewed 11 out of 11 changed files in this pull request and generated 10 comments.
Show a summary per file
File 	Description
internal/domain/level.go 	Adds TakeProfitPct field to Level struct
internal/infrastructure/storage/sqlite.go 	Adds take_profit_pct column to database schema with migration
internal/web/handlers.go 	Adds take profit percentage handling in form submission
internal/usecase/level_service.go 	Implements position caching, take profit logic, and moves stop loss at base from engine
internal/usecase/sublevel_engine.go 	Removes stop loss at base logic, fixes trigger direction for defensive-only tier entries
internal/usecase/level_service_test.go 	Adds tests for take profit and stop loss at base restart scenarios
internal/usecase/sublevel_engine_test.go 	Updates tests to reflect removal of stop loss logic and bidirectional trigger changes
tests/scenarios_test.go 	Adds sleep for unique timestamps, removes empty trade check, adds reset ticks
tests/repro_bug_test.go 	New test file reproducing stop loss at base behavior
tests/e2e_test.go 	Adds position simulation to mock exchange MarketBuy/MarketSell methods
internal/web/templates/index.html 	Major UI redesign with dark theme, improved layout, and new take profit input

💡 Add Copilot custom instructions for smarter, more guided reviews. Learn how to get started.

In tests/scenarios_test.go:

> @@ -106,9 +108,6 @@ func (h *TestScenarioHelper) AssertLastTrade(side domain.Side, size float64) {
 	if err != nil {
 		h.t.Fatalf("Failed to list trades: %v", err)
 	}

The AssertLastTrade function accesses trades[0] without checking if the trades slice is empty. While the removal of the empty check at lines 111-113 may be intentional, this could cause a panic if no trades exist. Consider adding a check or documenting why this is safe.
⬇️ Suggested change

-	}
+	}
+	if len(trades) == 0 {
+		h.t.Fatalf("No trades exist to assert last trade")
+	}

In internal/usecase/level_service.go:

> +					tpPrice := activeLevel.LevelPrice * (1 + activeLevel.TakeProfitPct)
+					if price >= tpPrice {
+						log.Printf("TAKE PROFIT: LONG on %s. Price %f >= TP %f. Closing...", symbol, price, tpPrice)
+						shouldTP = true
+					}
+				} else if pos.Side == domain.SideShort {
+					tpPrice := activeLevel.LevelPrice * (1 - activeLevel.TakeProfitPct)

The take profit calculation uses activeLevel.LevelPrice as the base instead of pos.EntryPrice. This could lead to incorrect TP targets. For example, if a long position is entered at 10050 (Tier 1) but the level price is 10000, a 2% TP would target 10200 (10000 * 1.02) instead of 10251 (10050 * 1.02). Consider using pos.EntryPrice as the base for TP calculations.
⬇️ Suggested change

-					tpPrice := activeLevel.LevelPrice * (1 + activeLevel.TakeProfitPct)
-					if price >= tpPrice {
-						log.Printf("TAKE PROFIT: LONG on %s. Price %f >= TP %f. Closing...", symbol, price, tpPrice)
-						shouldTP = true
-					}
-				} else if pos.Side == domain.SideShort {
-					tpPrice := activeLevel.LevelPrice * (1 - activeLevel.TakeProfitPct)
+					tpPrice := pos.EntryPrice * (1 + activeLevel.TakeProfitPct)
+					if price >= tpPrice {
+						log.Printf("TAKE PROFIT: LONG on %s. Price %f >= TP %f. Closing...", symbol, price, tpPrice)
+						shouldTP = true
+					}
+				} else if pos.Side == domain.SideShort {
+					tpPrice := pos.EntryPrice * (1 - activeLevel.TakeProfitPct)

In internal/usecase/level_service.go:

> +					tpPrice := activeLevel.LevelPrice * (1 + activeLevel.TakeProfitPct)
+					if price >= tpPrice {
+						log.Printf("TAKE PROFIT: LONG on %s. Price %f >= TP %f. Closing...", symbol, price, tpPrice)
+						shouldTP = true
+					}
+				} else if pos.Side == domain.SideShort {
+					tpPrice := activeLevel.LevelPrice * (1 - activeLevel.TakeProfitPct)

The take profit calculation uses activeLevel.LevelPrice as the base instead of pos.EntryPrice. This could lead to incorrect TP targets. For example, if a short position is entered at 9950 (Tier 1) but the level price is 10000, a 2% TP would target 9800 (10000 * 0.98) instead of 9751 (9950 * 0.98). Consider using pos.EntryPrice as the base for TP calculations.
⬇️ Suggested change

-					tpPrice := activeLevel.LevelPrice * (1 + activeLevel.TakeProfitPct)
-					if price >= tpPrice {
-						log.Printf("TAKE PROFIT: LONG on %s. Price %f >= TP %f. Closing...", symbol, price, tpPrice)
-						shouldTP = true
-					}
-				} else if pos.Side == domain.SideShort {
-					tpPrice := activeLevel.LevelPrice * (1 - activeLevel.TakeProfitPct)
+					tpPrice := pos.EntryPrice * (1 + activeLevel.TakeProfitPct)
+					if price >= tpPrice {
+						log.Printf("TAKE PROFIT: LONG on %s. Price %f >= TP %f. Closing...", symbol, price, tpPrice)
+						shouldTP = true
+					}
+				} else if pos.Side == domain.SideShort {
+					tpPrice := pos.EntryPrice * (1 - activeLevel.TakeProfitPct)

In internal/usecase/level_service.go:

> +						s.engine.UpdateState(l.ID, func(ls *LevelState) {
+							if realizedPnL > 0 {
+								ls.ConsecutiveWins++
+								log.Printf("AUDIT: Win recorded for Level %s. Consecutive Wins: %d", l.ID, ls.ConsecutiveWins)
+							} else {
+								ls.ConsecutiveWins = 0
+								log.Printf("AUDIT: Loss recorded for Level %s. Streak reset.", l.ID)
+							}
+						})
+						s.engine.ResetState(l.ID)

Both UpdateState and ResetState are called sequentially for each level. Since ResetState clears all state including ConsecutiveWins, the UpdateState call has no lasting effect. Either remove the UpdateState call or move ResetState logic to only clear tier triggers while preserving the win/loss tracking.

In internal/infrastructure/storage/sqlite.go:

> @@ -41,6 +41,7 @@ func (s *SQLiteStore) initSchema() error {
 			stop_loss_at_base BOOLEAN NOT NULL DEFAULT 0,
 			stop_loss_mode TEXT NOT NULL DEFAULT 'exchange',
 			disable_speed_close BOOLEAN NOT NULL DEFAULT 0,
+			take_profit_pct REAL NOT NULL DEFAULT 2.0,

The default value for take_profit_pct in the database schema is 2.0, but this appears to represent 2% as a whole number (which will be divided by 100 in the handler). However, the domain layer expects this value to be stored as a decimal (0.02 for 2%). This inconsistency means existing records migrated with the default will have the wrong value (2.0 instead of 0.02), resulting in a 200% take profit instead of 2%. The default should be 0.02 to match the expected format.

In internal/usecase/level_service_test.go:

> +	// 1. Long Position
+	// Entry 10000. TP 2% -> 10200.
+	mockEx.Position = &domain.Position{
+		Symbol:     "BTCUSDT",
+		Side:       domain.SideLong,
+		Size:       0.1,
+		EntryPrice: 10000,
+	}
+
+	// Price 10100 (Below TP)
+	service.ProcessTick(ctx, "bybit", "BTCUSDT", 10100)
+	if mockEx.CloseCalled {
+		t.Error("Expected NO Close when Price < TP")
+	}
+
+	// Price 10200 (Hit TP)
+	service.ProcessTick(ctx, "bybit", "BTCUSDT", 10200)

The test expects the take profit to be calculated based on LevelPrice (10000 * 1.02 = 10200), but the test comment says "Entry 10000. TP 2% -> 10200." This matches the current buggy implementation that uses LevelPrice instead of EntryPrice. If the bug identified earlier is fixed to use EntryPrice, this test will fail. The test should be updated to reflect the correct behavior or the expectation should be clarified.

In tests/e2e_test.go:

> +	} else {
+		// Add to position (simplified average entry price logic omitted for mock unless needed)
+		m.Position.Size += size

When adding to an existing position, the mock exchange should update the entry price to reflect the average entry price. The current implementation only increases size without adjusting the entry price, which could lead to incorrect PnL calculations in tests. Consider: m.Position.EntryPrice = (m.Position.EntryPrice * m.Position.Size + m.Price * size) / (m.Position.Size + size) before updating the size.

In tests/e2e_test.go:

>  	return nil
 }
 func (m *MockExchange) MarketSell(ctx context.Context, symbol string, size float64, leverage int, marginType string, stopLoss float64) error {
 	m.SellCalled = true
+	// Simulate Position Update
+	if m.Position == nil {
+		m.Position = &domain.Position{
+			Symbol:     symbol,
+			Side:       domain.SideShort,
+			Size:       size,
+			EntryPrice: m.Price,
+		}
+	} else {

When adding to an existing short position, the mock exchange should update the entry price to reflect the average entry price. The current implementation only increases size without adjusting the entry price, which could lead to incorrect PnL calculations in tests. Consider: m.Position.EntryPrice = (m.Position.EntryPrice * m.Position.Size + m.Price * size) / (m.Position.Size + size) before updating the size.
⬇️ Suggested change

-	} else {
+	} else {
+		// Update average entry price when adding to short position
+		m.Position.EntryPrice = (m.Position.EntryPrice*m.Position.Size + m.Price*size) / (m.Position.Size + size)

In internal/usecase/level_service.go:

> @@ -133,6 +139,38 @@ func (s *LevelService) UpdateCache(ctx context.Context) error {
 	return nil
 }
 
+func (s *LevelService) getPosition(ctx context.Context, symbol string) (*domain.Position, error) {
+	s.mu.RLock()
+	cached, ok := s.positionCache[symbol]
+	ts, timeOk := s.positionTime[symbol]
+	s.mu.RUnlock()
+
+	// Cache TTL: 1 second
+	if ok && timeOk && time.Since(ts) < 1*time.Second {
+		return cached, nil

The position cache returns a cached pointer to a domain.Position object. If the caller modifies this object, it will affect the cached value for subsequent callers. Consider returning a copy of the position to prevent unintended mutations: cachedCopy := *cached; return &cachedCopy, nil
⬇️ Suggested change

-		return cached, nil
+		cachedCopy := *cached
+		return &cachedCopy, nil

In internal/infrastructure/storage/sqlite.go:

> @@ -77,35 +78,36 @@ func (s *SQLiteStore) initSchema() error {
 	_, _ = s.db.Exec(`ALTER TABLE levels ADD COLUMN stop_loss_at_base BOOLEAN NOT NULL DEFAULT 0`)
 	_, _ = s.db.Exec(`ALTER TABLE levels ADD COLUMN stop_loss_mode TEXT NOT NULL DEFAULT 'exchange'`)
 	_, _ = s.db.Exec(`ALTER TABLE levels ADD COLUMN disable_speed_close BOOLEAN NOT NULL DEFAULT 0`)
+	_, _ = s.db.Exec(`ALTER TABLE levels ADD COLUMN take_profit_pct REAL NOT NULL DEFAULT 2.0`)

The ALTER TABLE migration uses a default value of 2.0 for take_profit_pct, but this is inconsistent with how the value is stored. The handler divides input by 100 (line 148 in handlers.go), so a 2% input becomes 0.02. However, existing records will get 2.0 as the default, representing 200% instead of 2%. The default should be 0.02 to match the expected format.