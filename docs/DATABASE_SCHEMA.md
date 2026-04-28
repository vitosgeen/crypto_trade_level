# Database Schema

The bot uses **SQLite** for persistence. The database file is typically named `bot.db` in the root directory.

## Tables

### `levels`
Stores the price levels configured for the bot to defend.

| Column | Type | Description |
| :--- | :--- | :--- |
| `id` | TEXT (PK) | Unique identifier for the level. |
| `exchange` | TEXT | Exchange name (e.g., "Bybit"). |
| `symbol` | TEXT | Trading pair (e.g., "BTCUSDT"). |
| `level_price` | REAL | The core price level being defended. |
| `side` | TEXT | `LONG`, `SHORT`, or `BOTH`. |
| `base_size` | REAL | The base quantity for the first tier. |
| `leverage` | INTEGER | Leverage to use for the position. |
| `margin_type` | TEXT | `isolated` or `cross`. |
| `cool_down_ms` | INTEGER | Cooldown in milliseconds after a trigger. |
| `take_profit_pct`| REAL | Take profit percentage. |
| `is_auto` | BOOLEAN | Whether the level was auto-created (e.g., via RSI). |
| `created_at` | DATETIME | Timestamp of creation. |

### `trades`
Logs every individual execution (order) performed by the bot.

| Column | Type | Description |
| :--- | :--- | :--- |
| `id` | INTEGER (PK)| Auto-incrementing ID. |
| `level_id` | TEXT | Reference to the `levels` table. |
| `side` | TEXT | `Buy` or `Sell`. |
| `size` | REAL | Quantity traded. |
| `price` | REAL | Execution price. |
| `realized_pnl` | REAL | PnL if this trade closed a position. |
| `created_at` | DATETIME | Timestamp of execution. |

### `position_history`
Records completed trade cycles (from Open to Close).

| Column | Type | Description |
| :--- | :--- | :--- |
| `id` | INTEGER (PK)| Auto-incrementing ID. |
| `symbol` | TEXT | Trading pair. |
| `side` | TEXT | `Long` or `Short`. |
| `size` | REAL | Total position size. |
| `entry_price` | REAL | Average entry price. |
| `exit_price` | REAL | Average exit price. |
| `realized_pnl` | REAL | Total profit/loss for the position. |
| `opened_at` | DATETIME | Timestamp when position opened. |
| `closed_at` | DATETIME | Timestamp when position closed. |

### `symbol_tiers`
Stores default tier percentages for specific symbols.

| Column | Type | Description |
| :--- | :--- | :--- |
| `exchange` | TEXT | Exchange name. |
| `symbol` | TEXT | Trading pair. |
| `tier1_pct` | REAL | Percentage for Tier 1. |
| `tier2_pct` | REAL | Percentage for Tier 2. |
| `tier3_pct` | REAL | Percentage for Tier 3. |

### `wallet_balances`
Periodic snapshots of the account balance for performance tracking.

| Column | Type | Description |
| :--- | :--- | :--- |
| `coin` | TEXT | Asset (e.g., "USDT"). |
| `total` | REAL | Total wallet balance. |
| `equity` | REAL | Total equity (including unrealized PnL). |
| `unrealized_pnl`| REAL | Current open PnL at snapshot time. |
| `timestamp` | DATETIME | Snapshot time. |

## Indices
- `idx_levels_exchange_symbol`: Speeds up level lookups for active trading.
- `idx_position_pnl_symbol_time`: Optimized for performance charts.
- `idx_wallet_coin_time`: Optimized for balance history charts.
