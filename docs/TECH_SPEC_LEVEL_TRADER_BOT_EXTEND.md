# TECH_SPEC_LEVEL_TRADER_BOT.md

## 1. Purpose

Build an autonomous trading bot in Go that **defends predefined price levels** on crypto perpetual futures.

The bot:

- Monitors real-time prices via exchange WebSocket.
- Maintains a **set of levels** per exchange/symbol.
- Trades **only when price is near a level**, within configurable percentage tiers.
- Automatically chooses **side** based on price vs. level:
  - If price is **above** the level → open/scale a **SHORT**.
  - If price is **below** the level → open/scale a **LONG**.
- Uses **three sublevels (tiers)** around each level to **scale in** (doubling position).
- Allows levels and tiers to be configured at runtime via a **minimal web UI using HTMX**.
- Follows **TDD**, **SOLID**, and **Clean Architecture** principles.

This document is the technical specification. No implementation code is included.

---

## 2. Terminology

| Term                  | Description |
|-----------------------|-------------|
| Level                 | A base price (anchor) around which the bot defends (e.g., 62750 USDT). |
| Sublevel / Tier       | A percentage distance from the level (e.g., 0.5%, 0.3%, 0.15%) that triggers scaling trades. |
| Tier1 / Tier2 / Tier3| The three sublevels configured for a symbol. Tier1 is farthest, Tier3 closest. |
| Side                  | Derived from price vs. level (above → SHORT, below → LONG). Not configured manually. |
| Level Defense         | The strategy of opening/adding to positions as price approaches a level. |
| Exchange Connector    | Abstraction layer for REST and WebSocket of the exchange. |
| Position Sizing       | Logic for how big each trade should be (MVP: deterministic doubling across tiers). |
| HTMX                  | Lightweight JS library used only for partial HTML updates in the web UI. |

---

## 3. High-Level Architecture (Clean Architecture)

```text
/internal
    /domain
        level.go
        position.go
        order.go
        level_service.go

    /usecase
        level_evaluator.go
        sublevel_engine.go
        signal_generator.go
        trade_executor.go
        risk_manager.go

    /infrastructure
        /exchange
            bybit_rest.go
            bybit_ws.go
        /storage
            sqlite.go
        /logger
            zap_logger.go

    /web
        server.go
        handlers.go
        templates/
            layout.html
            index.html
            levels.html
            positions.html
            trades.html
            status.html
            levels_form.html

/cmd
    /bot
        main.go

/config
    config.yaml      # global config (keys, polling, logging)
    levels.yaml      # optional static levels (MVP/testing)
3.1 Architectural Principles

Separation of concerns:

domain contains core entities and business rules.

usecase orchestrates workflows and trading logic.

infrastructure handles external systems (exchange, DB, logging).

web is a thin adapter layer exposing read/write operations for levels and status.

SOLID:

Exchange is an interface; specific exchanges are adapters.

Level evaluation logic is independent from storage and UI.

TDD-first:

Core logic (level evaluation, tier triggers, doubling, side selection) is specified and tested before implementation.

KISS/DRY:

Minimal indispensable features for MVP.

No unnecessary configuration in the first version.

4. Level & Sublevel Model
4.1 Level Concept

Each Level is an anchor price L for an (exchange, symbol) pair.
Around that level, the bot uses three sublevels (tiers) on both sides (above and below the level) that define when and how it scales in.

The side is not configured manually:

Price above L → the bot defends the level by shorting.

Price below L → the bot defends the level by longing.

4.2 Sublevels (Tiers) Per Symbol

For each (exchange, symbol) we configure three tier percentages via the web UI:

tier1_pct – outermost distance (e.g., 0.5%).

tier2_pct – middle distance (e.g., 0.3%).

tier3_pct – closest distance (e.g., 0.15%).

These tiers are:

The same for all levels on that symbol (by design).

Different symbols can have different tiers (BTC vs DOGE, etc.).

Stored in DB so the bot can use them for every level on that symbol.

Example:
BTCUSDT on Bybit:
  tier1_pct = 0.005   # 0.5%
  tier2_pct = 0.003   # 0.3%
  tier3_pct = 0.0015  # 0.15%
4.3 Level Data Structure

A Level in the domain model (conceptual):
type Level struct {
    ID           string
    Exchange     string   // "bybit", "binance", etc.
    Symbol       string   // "BTCUSDT"
    LevelPrice   float64  // anchor price L
    BaseSize     float64  // base position size for Tier1
    Leverage     int      // leverage for this level/symbol
    MarginType   string   // "isolated" or "cross"
    CoolDownMs   int      // cooldown after last trigger for this level
    Source       string   // e.g. "manual-web", "orderbook", "liquidations"
    CreatedAt    time.Time
}
Separate configuration object for tiers per symbol:
type SymbolTiers struct {
    Exchange  string
    Symbol    string
    Tier1Pct  float64 // e.g., 0.005
    Tier2Pct  float64 // e.g., 0.003
    Tier3Pct  float64 // e.g., 0.0015
    UpdatedAt time.Time
}
Important rule:
For a given (exchange, symbol), the same tier percentages apply to all levels on that symbol.

5. Trigger Logic: Sublevels and Doubling
5.1 Side Determination

For each tick and each level:

Let P = current mid price (e.g., (bestBid + bestAsk)/2).

Let L = Level.LevelPrice.

Side:

If P > L → SHORT side.

If P < L → LONG side.

If P == L → considered a “touch” of the level (future: optional close logic).

Side is always derived from P vs L. No manual direction configuration.

5.2 Tier Boundaries

Given tiers for (exchange, symbol):

tier1_pct, tier2_pct, tier3_pct with tier1_pct > tier2_pct > tier3_pct.

For each level L:
Above level (used for shorts):
Tier1_above = L * (1 + tier1_pct)
Tier2_above = L * (1 + tier2_pct)
Tier3_above = L * (1 + tier3_pct)
Below level (used for longs):
Tier1_below = L * (1 - tier1_pct)
Tier2_below = L * (1 - tier2_pct)
Tier3_below = L * (1 - tier3_pct)
5.3 When Triggers Fire (User Q1)

A tier fires when the price crosses that tier from “outside towards the level”.

Example for SHORT (price coming from above):

Price moves from P > Tier1_above to P <= Tier1_above → Tier1 trigger.

Later, price moves from P > Tier2_above to P <= Tier2_above → Tier2 trigger.

Later, price moves from P > Tier3_above to P <= Tier3_above → Tier3 trigger.

Similarly for LONG (price coming from below):

Price moves from P < Tier1_below to P >= Tier1_below → Tier1 trigger.

Then P < Tier2_below to P >= Tier2_below → Tier2 trigger.

Then P < Tier3_below to P >= Tier3_below → Tier3 trigger.

Once a tier triggers, it must not trigger again until the position/tier state is reset (e.g., after position close or manual reset).

5.4 Doubling Logic (User Q2)

When a tier triggers, the bot opens an additional order in the same direction (never closes and reopens).

With BaseSize defined on the level, behavior is:

Tier1:

open initial position with size = BaseSize.

Tier2:

add an order that doubles the total position.

If current total size is BaseSize, add another BaseSize:

new total size = 2 * BaseSize.

Tier3:

add an order that doubles total again.

If current total size is 2 * BaseSize, add 2 * BaseSize:

new total size = 4 * BaseSize.

Summary of incremental orders:

Tier1: +1 * BaseSize

Tier2: +1 * BaseSize (total 2x)

Tier3: +2 * BaseSize (total 4x)

5.5 Reset of Multipliers

When a position is closed with profit (or via manual command / future strategy rules), tier state for that (exchange, symbol, level) is reset:

Tier1, Tier2, Tier3 all become “not triggered”.

Next time price approaches from outside, the full Tier1→Tier2→Tier3 sequence is available again.

5.6 Cooldown

Each level has CoolDownMs:

After any tier triggers, record lastTriggerTime.

Ignore new tier triggers for that level while:
now - lastTriggerTime < CoolDownMs
This prevents spam trades when price chops around a tier.

6. Position & Risk Management
6.1 MVP Behavior

For each (exchange, symbol) the bot maintains one aggregated position per side.

When tiers trigger, the bot adds to the existing position in the same direction.

No partial closes in MVP; closing strategies are deliberately kept simple:

manual close (out of scope of MVP UI, but can be REST or exchange side).

future: automatic TP/SL or close on level touch.

Parameters per level/symbol:

BaseSize — starting size for Tier1.

Leverage — numeric leverage used for that symbol on that exchange.

MarginType — "isolated" or "cross".

6.2 Future Extensions (Not MVP)

Dynamic BaseSize based on account balance (% of equity).

Different BaseSize per tier (non-doubling patterns).

Per-tier custom leverage.

Risk-limits per symbol and per exchange (max position size, max number of open levels, etc.).

Close rules based on PnL, time, volatility, etc.

7. Exchange Integration (Abstracted)
7.1 WebSocket

The bot subscribes to real-time market data:

last traded price OR mid price.

best bid / best ask (optional but recommended).

Requirements:

Automatic reconnect with exponential backoff.

Heartbeat detection and timeout.

On reconnect, resync open positions using REST.

7.2 REST

The bot uses REST API for:

Placing market orders (buy/sell).

Querying open positions.

Setting leverage and margin mode (if required).

Closing positions for a symbol (manual or emergency).

Standard interface for trading logic:
type Exchange interface {
    GetCurrentPrice(symbol string) (float64, error)
    MarketBuy(symbol string, size float64, leverage int, marginType string) error
    MarketSell(symbol string, size float64, leverage int, marginType string) error
    ClosePosition(symbol string) error
    GetPosition(symbol string) (Position, error)
}
Concrete adapters (Bybit, Binance, MEXC, etc.) implement this.
8. Storage (SQLite)
8.1 Tables

levels
CREATE TABLE levels (
    id           TEXT PRIMARY KEY,
    exchange     TEXT NOT NULL,
    symbol       TEXT NOT NULL,
    level_price  REAL NOT NULL,
    base_size    REAL NOT NULL,
    leverage     INTEGER NOT NULL,
    margin_type  TEXT NOT NULL,  -- 'isolated' or 'cross'
    cool_down_ms INTEGER NOT NULL,
    source       TEXT,
    created_at   DATETIME NOT NULL
);

CREATE INDEX idx_levels_exchange_symbol
    ON levels(exchange, symbol);

symbol_tiers
CREATE TABLE symbol_tiers (
    exchange   TEXT NOT NULL,
    symbol     TEXT NOT NULL,
    tier1_pct  REAL NOT NULL,
    tier2_pct  REAL NOT NULL,
    tier3_pct  REAL NOT NULL,
    updated_at DATETIME NOT NULL,
    PRIMARY KEY (exchange, symbol)
);
trades
CREATE TABLE trades (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    exchange    TEXT NOT NULL,
    symbol      TEXT NOT NULL,
    level_id    TEXT NOT NULL,
    side        TEXT NOT NULL,  -- 'LONG' or 'SHORT'
    size        REAL NOT NULL,
    price       REAL NOT NULL,
    created_at  DATETIME NOT NULL
);

sessions (optional runtime metadata)
CREATE TABLE sessions (
    id          TEXT PRIMARY KEY,
    started_at  DATETIME NOT NULL,
    stopped_at  DATETIME
);
9. Configuration
Global config file (config.yaml):
exchanges:
  - name: "bybit"
    api_key: "..."
    api_secret: "..."
    ws_endpoint: "wss://..."
    rest_endpoint: "https://..."

polling:
  levels_reload_ms: 5000   # reload levels & tiers from DB every 5s

logging:
  level: "info"            # debug/info/warn/error


Static levels (optional, mainly for tests and initial setup) may be defined in levels.yaml, but the main source of truth for runtime is DB + web UI.

10. Logging & Monitoring

Use structured logging (Zap or similar).

Log events:

Level added / removed.

Symbol tiers updated.

Tier1/Tier2/Tier3 triggers (with price, level ID, tier).

Orders sent and their results (success/failure, response time).

WebSocket reconnects and errors.

REST call failures and retries.

Position changes (open, scale, close).

11. Testing (TDD Plan)
11.1 Unit Tests

LevelEvaluator:

Correct side determination (above → short, below → long).

Correct tier boundary computation for given L and tierX_pct.

SublevelEngine:

Tier triggers only when crossing from outside toward the level.

Each tier triggers at most once before reset.

Correct incremental order sizes (1x, +1x, +2x).

Cooldown respects CoolDownMs.

Tier states reset when position is closed.

SignalGenerator:

Combines tier triggers into a simple signal model (Buy/Sell/NoOp).

TradeExecutor:

Calls the correct methods on the Exchange interface with the right parameters.

Handles and logs errors.

11.2 Integration Tests

With a mock WebSocket feed:

Price path moves from far away through Tier1, Tier2, Tier3.

Verify the sequence of generated orders and the sizes.

With a mock Exchange implementation:

Assert that MarketBuy/MarketSell/ClosePosition are called correctly.

11.3 End-to-End Tests

Start the bot with:

One exchange adapter (mock or sandbox).

One symbol (e.g., BTCUSDT).

One level and tiers configured.

Feed price data programmatically.

Verify:

Web dashboard shows current levels, tiers, and triggers.

Trades table is populated properly.

No duplicate triggers beyond tiers/W cooldown.

12. Non-Functional Requirements

Language: Go ≥ 1.22.

Memory footprint: < 100 MB for typical configuration.

Latency:

Reaction to a price tick: within ~50 ms (excluding network).

Graceful shutdown:

On SIGTERM, stop consuming new ticks.

Finish in-flight REST requests.

Optionally leave positions open (MVP) – closing behavior can be configured later.

No global shared mutable state; use scoped services, dependency injection where appropriate.

13. Minimal Monitoring Dashboard (HTMX)

The bot exposes a minimal web UI for monitoring, built using:

Go HTTP server.

Go HTML templates.

HTMX for partial updates (no SPA framework).

13.1 Routes
Method	Path	Description
GET	/	Main dashboard.
GET	/levels	Overview of levels for all exchanges/symbols.
GET	/levels/{exchange}	Levels & tiers forms for one exchange.
GET	/levels/{exchange}/{symbol}	Levels + tiers + forms for a specific symbol.
POST	/levels/{exchange}/{symbol}	Add a new level for that symbol.
DELETE	/levels/{exchange}/{symbol}/{id}	Delete a specific level.
GET	/positions	Active positions list.
GET	/trades	Recent trades table.
GET	/status	System and WS/REST status.

Makes use of:
<div
  hx-get="/status"
  hx-trigger="every 2s"
  hx-swap="outerHTML">
</div>


13.3 Levels UI

For each (exchange, symbol) page:

Symbol tiers form:

Fields:

tier1_pct, tier2_pct, tier3_pct (e.g., 0.005, 0.003, 0.0015).

On submit:

Updates symbol_tiers row in DB.

Returns an updated tiers widget and levels table.

Level creation form:

Fields:

Field	Type	Description
level_price	number	Anchor price L.
base_size	number	Base size for Tier1.
leverage	integer	Leverage for this level.
margin_type	select	isolated or cross.
cool_down_ms	number	Cooldown after trigger.

Example:
<form
  hx-post="/levels/bybit/BTCUSDT"
  hx-target="#levels-table"
  hx-swap="outerHTML">
  <!-- inputs here -->
</form>


Levels table under the form:
Columns:
| ID | Level Price | Base Size | Leverage | Margin | Cooldown | Source | Delete |

Delete button per row:
<button
  hx-delete="/levels/bybit/BTCUSDT/{{.ID}}"
  hx-confirm="Delete this level?"
  hx-target="#levels-table"
  hx-swap="outerHTML">
  Delete
</button>

Auto-refresh wrapper:
<div
  id="levels-table"
  hx-get="/levels/bybit/BTCUSDT/table"
  hx-trigger="every 2s"
  hx-swap="outerHTML">
</div>

13.4 Positions View (/positions)

Table with:

Exchange

Symbol

Side (LONG/SHORT)

Size

Entry price

Current price

Unrealized PnL

Leverage

Margin type

Updated periodically with HTMX (hx-trigger="every 2s").

13.5 Trades View (/trades)

Shows last N trades:

Timestamp

Exchange

Symbol

Level ID

Side

Size

Price

Polling via HTMX (e.g., every 3–5 seconds).

13.6 Status View (/status)

Displays:

WebSocket status and reconnect counters.

REST latency and last error per exchange.

Memory usage (optional).

Current session ID and uptime.

14. Level Input & Management (Web Interface)
14.1 Overview

Levels and symbol tiers are managed through the web interface for runtime convenience.

Editing existing levels is not supported in MVP:

To change a level, the user deletes it and creates a new one with desired parameters.

14.2 Symbol Tiers (per (exchange, symbol))

Tiers (tier1_pct, tier2_pct, tier3_pct) are configured via a simple form.

These tiers apply to all levels of that (exchange, symbol).

Different symbols may use different tiers (depends on coin behavior).

14.3 Runtime Reloading

Bot periodically:

Reloads levels and symbol_tiers from SQLite (every levels_reload_ms).

Computes diff vs in-memory state:

New levels → added to active levels.

Deleted levels → removed from active levels.

Tiers changes → update tier boundaries for that symbol.

No bot restart is required.

14.4 Behavior on Deletion

When a level is deleted:

It is removed from in-memory structures.

Tier state related to that level is dropped.

Existing open positions are not automatically closed in MVP (future config option).

15. Roadmap
Phase 1 – MVP

Single exchange integration (e.g., Bybit).

Per-symbol tiers with 3 sublevels.

Level-based tier triggers and doubling logic.

Fixed BaseSize and leverage per level.

Market orders only.

SQLite persistence.

Minimal HTMX dashboard:

Levels + tiers (forms).

Positions.

Trades.

System status.

Unit + integration tests for tier logic and trade execution.

Phase 2 – Enhanced Control

More exchanges (Binance, MEXC, OKX, etc.).

Close-position rules (e.g., on level touch or PnL target).

Telegram notifications (tier triggers, orders, errors).

Ability to pause/resume the bot via UI.

Optional WebSocket → UI push updates.

Phase 3 – Advanced Features

Backtesting engine for level + sublevel strategy.

Auto-discovery of levels (orderbook heatmaps, liquidation clusters, swing highs/lows).

Parameter optimizer for tiers and BaseSize.

Rich web dashboard with charts and analytics.


16. Sentiment-Based Trading Logic
16.1 Concept

The bot uses "Market Speed" (Trade Volume Sentiment) to filter entries and trigger exits.
Sentiment Score: A value from -1.0 (Strong Sell) to +1.0 (Strong Buy).
Calculation: (BuyVolume - SellVolume) / (BuyVolume + SellVolume) over the last 60 seconds.

16.2 Entry Filter ("Don't catch a falling knife")

Before opening a position (Long or Short), the bot checks the Sentiment Score.
Confident Threshold: 0.6 (60% dominance).
Rules:
Long Entry: Blocked if Sentiment < -0.6 (Strong Sell Pressure).
Short Entry: Blocked if Sentiment > 0.6 (Strong Buy Pressure).

16.3 Exit Trigger ("Ride the trend until it bends")

The bot continuously monitors open positions against the Sentiment Score.
Rules:
Long Position: Close if Sentiment drops below -0.6 (Reversal to Strong Sell).
Short Position: Close if Sentiment rises above 0.6 (Reversal to Strong Buy).

## TODO

Position Management:
Auto-close position when price crosses back to the base level
Take-profit / stop-loss functionality
Position size scaling based on PnL

Level Management:
Edit existing levels (change tiers, leverage, etc.)
Pause/resume levels without deleting
Level templates for quick setup

Monitoring & Alerts:
Telegram/Discord notifications when trades trigger
Email alerts for important events
Sound alerts in the web UI

Analytics & Reporting:
Trade history with P&L analysis
Win rate statistics per level
Performance charts

Risk Management:
Maximum position size limits
Daily loss limits
Auto-pause trading on drawdown

UI Improvements:
Dark mode toggle
Mobile-responsive design
Real-time PnL updates