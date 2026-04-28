# Clean Architecture

The `crypto_trade_level` project is strictly organized according to **Clean Architecture** principles. This design ensures that the business logic is decoupled from external technical details like the database or the exchange API.

## The Dependency Rule

Dependencies only point **inwards**.
1.  **Domain** (No dependencies)
2.  **Usecase** (Depends on Domain)
3.  **Infrastructure / Web** (Depends on Domain and Usecase)

---

## 1. Domain Layer (`internal/domain`)
The innermost layer. It contains the "soul" of the application.
-   **Entities**: Go structs like `Level`, `Trade`, and `Order`.
-   **Interfaces**: Abstractions that define what the system needs from the outside world.
    -   `Exchange`: How the bot talks to a crypto exchange.
    -   `LevelRepository`: How the bot saves and loads levels.
    -   `TradeRepository`: How the bot logs trade history.

## 2. Usecase Layer (`internal/usecase`)
Contains application-specific business rules.
-   **`LevelService`**: The main orchestrator. It listens to the `Exchange` (via callbacks), evaluates the price using the `SublevelEngine`, and calls the `TradeExecutor` to take action.
-   **Pure Logic**: Components like `LevelEvaluator` and `SublevelEngine` are designed to be "pure"—they take inputs (prices, levels) and return decisions (Buy/Sell/None) without knowing about databases or network connections. This makes them highly testable.

## 3. Infrastructure Layer (`internal/infrastructure`)
The outermost layer that talks to the real world.
-   **`exchange/bybit.go`**: Implements the `Exchange` interface using the Bybit API.
-   **`storage/sqlite.go`**: Implements the repository interfaces using SQL.
-   **`logger/`**: Implementation of structured logging.

## 4. Web Layer (`internal/web`)
The user interface.
-   Treats the `LevelService` as its primary dependency.
-   Provides an HTMX-powered dashboard to interact with the system.
-   Translates HTTP requests into calls to the Usecase layer.

---

## Benefits of this Architecture

-   **Testability**: We can test the `SublevelEngine` without a real exchange or database.
-   **Maintainability**: If we want to switch from Bybit to Binance, we only need to write a new `Exchange` adapter in the Infrastructure layer.
-   **Clarity**: New developers can easily understand the project by looking at the Domain entities and Usecases before diving into complex API integrations.
