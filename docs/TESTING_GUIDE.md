# Testing Guide

The **Crypto Trade Level Bot** is built with testability in mind, following Clean Architecture principles that allow for deep unit testing and scenario-based integration testing.

## Test Categories

### 1. Unit Tests
Unit tests focus on individual components in isolation, typically using mocks for dependencies like the database or exchange.
-   **Sublevel Engine**: Tested in `internal/usecase/sublevel_engine_test.go`. Verifies that price crosses correctly trigger tiers.
-   **Level Evaluator**: Tested in `internal/usecase/level_evaluator_test.go`. Verifies boundary calculations for Long and Short sides.
-   **Funding Bot**: Tested in `internal/usecase/funding_bot_service_test.go`.

### 2. Integration Tests
Located in the `tests/` directory, these tests verify the interaction between multiple components (e.g., `LevelService` + `SublevelEngine` + `SQLiteStore`).
-   **Scenario Tests**: `tests/scenarios_test.go` contains complex trading scenarios where simulated price movements are fed into the system to verify the expected series of trades and state changes.
-   **E2E Tests**: `tests/e2e_test.go` provides a high-level check of the bot's lifecycle.

## How to Run Tests

### Run All Tests
```bash
go test ./...
```

### Run Tests with Verbose Output
```bash
go test -v ./...
```

### Run a Specific Test
```bash
go test -v ./internal/usecase -run TestSublevelEngine_Evaluate
```

### Run Scenario Tests
```bash
go test -v ./tests -run TestTradingScenarios
```

## Creating New Tests

### Mocking the Exchange
When testing logic that involves trading, use a mock implementation of the `domain.Exchange` interface. This prevents real API calls during testing.

### Scenario DSL
The project uses a "Scenario" pattern in `tests/scenarios_test.go` where you can define a sequence of price ticks and assert the resulting actions. This is the preferred way to test new trading features.

```go
// Example Scenario Snippet
s.Tick(100.5) // Price hits Tier 1
s.AssertAction(usecase.ActionOpen)
s.Tick(100.2) // Price hits Tier 2
s.AssertAction(usecase.ActionAddToPosition)
```

## Best Practices
-   **Stateless Engines**: Keep engines like `SublevelEngine` stateless or easily resettable to simplify testing.
-   **Time Sensitivity**: Avoid `time.Sleep` in tests. Use mock clocks or event-driven synchronization where possible.
-   **Database**: For repository tests, use an in-memory SQLite database (`:memory:`) for speed and isolation.
