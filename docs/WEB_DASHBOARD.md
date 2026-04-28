# Web Dashboard

The bot features a web-based dashboard for real-time monitoring and configuration management. It is built using standard Go templates and **HTMX** for dynamic, partial page updates without full reloads.

## Pages

### 1. Dashboard (`/dashboard`)
The central hub of the bot.
-   **Positions Table**: Shows currently open positions, average entry price, current price, and unrealized PnL.
-   **Active Levels**: Lists all levels currently being defended by the bot.
-   **Recent Trades**: A log of the latest executions.

### 2. Wallet (`/wallet`)
-   Displays account equity, balance, and margin usage.
-   Includes historical charts of PnL and balance over time (integrated with the database).

### 3. Funding Bot (`/funding-bot`)
-   Specialized view for monitoring funding-rate arbitrage strategies.
-   Shows symbol details, funding intervals, and session logs.

### 4. Level Bot (`/level-bot`)
-   Interface for managing and analyzing "Level Defense" performance.
-   Allows manual triggering of level creation or adjustment.

## Dynamic Updates (HTMX)

The dashboard uses HTMX to provide a "live" feel. Key components poll the server at regular intervals:

-   **Positions**: Updated every few seconds to reflect changing market prices and PnL.
-   **Price Ticker**: Real-time price updates streamed from the exchange via WebSocket are pushed to the UI.
-   **Status Bar**: Displays WebSocket connectivity status and latency.

## API Endpoints

The web server also provides several JSON endpoints for programmatic access or frontend charting:

-   `GET /api/positions`: Returns current positions as JSON.
-   `GET /api/levels`: Returns configured levels.
-   `GET /api/market-stats`: Returns global market indicators (RSI, Volatility).
-   `GET /api/liquidity`: Returns order book depth snapshots.

## Implementation

Located in `internal/web/`:
-   `server.go`: Initializes the HTTP server and defines all routes.
-   `handlers.go`: Contains the logic for rendering templates and processing form submissions.
-   `templates/`: HTML templates using Go's `html/template` package.
-   `handlers_json.go`: Specialized handlers for returning raw data for charts and HTMX partials.
