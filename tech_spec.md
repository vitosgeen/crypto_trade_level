# Technical Specification: Crypto Trade Level Bot

## 1. Overview
The **Crypto Trade Level Bot** is an autonomous trading system designed to "defend" specific price levels on perpetual futures markets (specifically Bybit). It employs a **Counter-Trend** strategy, buying at support levels and selling at resistance levels. The system is built in **Go** using **Clean Architecture** principles to ensure maintainability, testability, and separation of concerns.

## 2. Architecture
The project follows the **Clean Architecture** pattern, divided into four main layers:

### 2.1. Domain Layer (`internal/domain`)
Contains the core business entities and interface definitions. This layer is independent of any external libraries or frameworks.
- **Entities**: `Level`, `SymbolTiers`, `Trade`, `Position`, `Order`.
- **Interfaces**: `LevelRepository`, `TradeRepository`, `Exchange`.

### 2.2. Usecase Layer (`internal/usecase`)
Contains the business logic and application rules.
- **LevelService**: Orchestrates the flow of data between the Exchange, Repository, and Trading Engine.
- **LevelEvaluator**: Pure logic component that determines:
    - **Side**: Long (Support) vs Short (Resistance).
    - **Boundaries**: Calculates the specific price points for the 3 tiers based on the level price and configured percentages.
- **SublevelEngine**: Manages the state of each level (which tiers have triggered) and evaluates price ticks against boundaries to trigger actions (`OPEN`, `ADD`, `NONE`).
- **TradeExecutor**: Handles the execution of trades via the Exchange interface.

### 2.3. Infrastructure Layer (`internal/infrastructure`)
Implements the interfaces defined in the Domain layer.
- **Exchange**: `BybitAdapter` implements the `Exchange` interface using the Bybit V5 API (REST and WebSocket).
    - Handles WebSocket connection management and reconnection.
    - Thread-safe callback registration for real-time price updates.
- **Storage**: `SQLiteStore` implements `LevelRepository` and `TradeRepository` using `database/sql` and SQLite.
- **Logger**: Structured logging using `uber-go/zap`.

### 2.4. Web Layer (`internal/web`)
Provides a user interface for monitoring and configuration.
- **Server**: HTTP server using `net/http`.
- **Handlers**: REST endpoints for managing levels and HTMX-based endpoints for UI updates.
- **Templates**: HTML templates rendered server-side.
- **HTMX**: Used for dynamic updates (real-time price table, position table) without full page reloads.

## 3. System Design

### 3.1. Data Flow
1.  **Price Update**: The `BybitAdapter` receives a price tick via WebSocket.
2.  **Event Propagation**: The tick is passed to the `LevelService.ProcessTick` method.
3.  **Level Filtering**: The service identifies active levels for the symbol.
4.  **Evaluation**:
    - `LevelEvaluator` determines if the price is interacting with a level (Support/Resistance).
    - `SublevelEngine` checks if specific tiers (1, 2, or 3) have been crossed.
5.  **Execution**: If a tier is triggered:
    - `TradeExecutor` sends a market order to Bybit.
    - The trade is recorded in the database.
    - The UI updates to reflect the new position.

### 3.2. Trading Strategy ("Defending")
The bot implements a **Counter-Trend** strategy:
- **Support (Long)**: When price drops to a defined Support Level.
    - **Tier 1**: Buy `BaseSize` when price hits `Level * (1 + Tier1Pct)`.
    - **Tier 2**: Buy `BaseSize` when price hits `Level * (1 + Tier2Pct)`.
    - **Tier 3**: Buy `2 * BaseSize` when price hits `Level * (1 + Tier3Pct)`.
- **Resistance (Short)**: When price rises to a defined Resistance Level.
    - **Tier 1**: Sell `BaseSize` when price hits `Level * (1 - Tier1Pct)`.
    - **Tier 2**: Sell `BaseSize` when price hits `Level * (1 - Tier2Pct)`.
    - **Tier 3**: Sell `2 * BaseSize` when price hits `Level * (1 - Tier3Pct)`.

### 3.3. Concurrency
- **Thread Safety**: The `BybitAdapter` and `LevelService` use `sync.RWMutex` to protect shared state (callbacks, last prices, level states).
- **Non-Blocking**: WebSocket reading happens in a dedicated goroutine. Processing is fast to avoid blocking the reader, though for high-frequency trading, a worker pool pattern could be considered in the future.

## 4. Technology Stack
- **Language**: Go 1.21+
- **Database**: SQLite
- **Exchange API**: Bybit V5 (Linear Perpetual)
- **Web Framework**: Standard `net/http` + `html/template`
- **Frontend**: HTML + CSS + HTMX
- **Logging**: Zap

## 5. Future Improvements
- **Risk Management**: Max position size limits, daily loss limits.
- **Dynamic Tiers**: Adjusting tier percentages based on volatility (ATR).
- **Backtesting**: Simulator implementation for the `Exchange` interface.
- **User Auth**: Protecting the Web UI.
