# API Integration: Bybit V5

The bot integrates with the **Bybit V5 API** (Linear Perpetual) for real-time market data and trade execution.

## Connectivity

### 1. REST API
-   **Base URL**: `https://api.bybit.com`
-   **Authentication**: Uses HMAC-SHA256 signing with `API Key` and `API Secret`.
-   **Endpoints Used**:
    -   `POST /v5/order/create`: Place market or limit orders.
    -   `POST /v5/position/set-leverage`: Adjust leverage for a symbol.
    -   `POST /v5/position/switch-mode`: Switch between Cross and Isolated margin.
    -   `GET /v5/position/list`: Fetch current open positions.
    -   `GET /v5/account/wallet-balance`: Fetch account equity and coin balances.
    -   `GET /v5/market/tickers`: Fetch current market price (fallback/initial).

### 2. WebSocket (WS)
-   **URL**: `wss://stream.bybit.com/v5/public/linear`
-   **Subscription**: The bot subscribes to `tickers.<symbol>` for real-time price updates.
-   **Handling**:
    -   A background goroutine (`readLoop`) listens for incoming messages.
    -   Price updates are propagated via callbacks to the `LevelService`.
    -   A `pingLoop` maintains the connection by sending heartbeats every 20 seconds.
    -   Automatic reconnection logic is implemented to handle network drops.

## Implementation Details

### `BybitAdapter`
Located in `internal/infrastructure/exchange/bybit.go`, this struct implements the `domain.Exchange` interface.

-   **Thread Safety**: Uses `sync.Mutex` to protect the WebSocket connection and callback list.
-   **Precision Handling**: Uses `strconv.FormatFloat` to ensure prices and quantities meet exchange requirements.
-   **Fees**: Incorporates a hardcoded `BybitTakerFeeRate` (0.055%) for accurate realized PnL calculations.

## Trade Execution Flow

1.  **Preparation**: Before placing an order, the adapter ensures the correct **Margin Mode** (Isolated/Cross) and **Leverage** are set for the symbol.
2.  **Order Placement**: Sends a Market Order for speed, as the "Level Defense" strategy relies on timely execution at specific boundaries.
3.  **Position Management**: If a position is already open, subsequent "ADD" actions increase the size. "CLOSE" actions use `reduceOnly=true` to ensure the position is closed without opening an opposite one.

## Error Handling
-   **RetCode Check**: Every API response is checked for `retCode != 0`.
-   **Logging**: All API errors and raw responses are logged for debugging.
-   **WS Latency**: The bot monitors WebSocket latency (Time between Ping and Pong) and displays it on the dashboard.
