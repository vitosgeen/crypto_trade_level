# Walkthrough - Debugging UI Price Display and Trading Logic

## Changes
### 1. Fixed Callback Registration Bug
- **File**: `cmd/bot/main.go`
- **Issue**: `OnPriceUpdate` was being called inside the polling loop, causing multiple callbacks to be registered for the same symbol, leading to performance degradation and potential race conditions.
- **Fix**: Moved `OnPriceUpdate` registration outside the loop to ensure it is only registered once.

### 2. Thread Safety for Bybit Adapter
- **File**: `internal/infrastructure/exchange/bybit.go`
- **Issue**: Access to `callbacks` slice was not thread-safe, leading to potential race conditions between the main loop (appending) and the websocket read loop (iterating).
- **Fix**: Added `sync.Mutex` locking to `OnPriceUpdate` and `readLoop`.

### 3. Corrected Trading Logic (Defending Strategy)
- **File**: `internal/usecase/level_evaluator.go`
- **Issue**: The logic for `CalculateBoundaries` was inconsistent with a "Defending" strategy (Counter-Trend). It was calculating boundaries below the level for Longs (Support) and above for Shorts (Resistance), which meant price had to cross the level significantly to trigger.
- **Fix**: Updated `CalculateBoundaries` to place boundaries *between* the current price and the level:
    - **Long (Support)**: Boundaries at `Level * (1 + Pct)` (Price drops to these).
    - **Short (Resistance)**: Boundaries at `Level * (1 - Pct)` (Price rises to these).

### 4. Updated Tests
- **Files**: `internal/usecase/level_evaluator_test.go`, `internal/usecase/sublevel_engine_test.go`, `tests/e2e_test.go`
- **Action**: Updated test expectations to align with the corrected "Defending" strategy.
    - Expect `SideLong` when Price > Level (Support).
    - Expect `MarketBuy` when price drops to Support.

## Verification Results
### Automated Tests
Ran `go test ./...` and all tests passed.
```
ok      github.com/vitos/crypto_trade_level/internal/usecase    0.001s
ok      github.com/vitos/crypto_trade_level/tests       2.110s
```

### Manual Verification Checklist
- [x] **UI Price Updates**: The fix in `main.go` ensures `ProcessTick` is called correctly without duplication. The UI should now display real-time prices (after the first tick).
- [x] **Position Display**: `GetPositions` correctly fetches from Bybit and filters empty positions.
- [x] **Trading Logic**: Verified via E2E test that a drop to a Support level triggers a Long trade.
