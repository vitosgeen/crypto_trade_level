# Project Structure

The `crypto_trade_level` project follows a **Clean Architecture** pattern, ensuring a clear separation between business logic, external integrations, and the user interface.

## Directory Layout

### Core Directories

-   **`cmd/`**: Contains the entry points for the various components of the system.
    -   `cmd/bot/`: The main trading bot application.
    -   `cmd/analyzer/`: Tools for analyzing trade logs and performance.
    -   `cmd/check_exchange/`: Utility to verify exchange connectivity and account status.
    -   `cmd/debug_db/`: Script to inspect the SQLite database.
-   **`internal/`**: The core logic of the application, hidden from external packages.
    -   **`domain/`**: Contains the core business entities (`Level`, `Trade`, `Position`) and interface definitions (`Exchange`, `LevelRepository`). This layer has zero dependencies on other layers or external frameworks.
    -   **`usecase/`**: Implements the business logic.
        -   `level_service.go`: The main orchestrator that coordinates price updates, evaluation, and execution.
        -   `sublevel_engine.go`: Stateless engine that determines when specific trading tiers are triggered based on price movement.
        -   `level_evaluator.go`: Logic for calculating tier boundaries and determining the trading side.
        -   `trade_executor.go`: Handles the actual placement of orders on the exchange.
    -   **`infrastructure/`**: Contains concrete implementations of the domain interfaces.
        -   `exchange/`: Adapters for external exchanges (e.g., Bybit V5 API).
        -   `storage/`: SQLite implementation for data persistence.
        -   `logger/`: Structured logging using `zap`.
    -   **`web/`**: The web-based dashboard for monitoring and management.
        -   Includes HTTP handlers, templates (HTML), and HTMX for dynamic updates.

### Configuration and Data

-   **`config/`**: Holds configuration files (`config.yaml`).
-   **`docs/`**: Project documentation, specifications, and feedback logs.
-   **`logs/`**: Directory where the bot saves its runtime logs.
-   **`tests/`**: Integration and unit tests for various components.

### Other Files

-   **`Makefile`**: Useful commands for building, running, and testing the project.
-   **`go.mod` / `go.sum`**: Go module definitions and dependency management.
-   **`bot.db`**: The SQLite database file (created at runtime).
-   **`tech_spec.md`**: The original technical specification for the project.

## Summary of Layers

| Layer | Responsibility | Key Files |
| :--- | :--- | :--- |
| **Domain** | Entities & Interfaces | `level.go`, `interfaces.go` |
| **Usecase** | Business Logic | `level_service.go`, `sublevel_engine.go` |
| **Infrastructure** | Adapters & Persistence | `bybit.go`, `sqlite.go` |
| **Web** | User Interface | `server.go`, `handlers.go`, `templates/` |
