# Crypto Trade Level Bot

An autonomous crypto trading bot written in Go. It implements a "Defending" strategy (Counter-Trend) to trade predefined price levels on perpetual futures (Bybit).

## Features

- **Clean Architecture**: Separated into Domain, Usecase, Infrastructure, and Web layers.
- **Real-time Trading**: Listens to WebSocket price updates.
- **Level Defense**:
  - **Long (Support)**: Buys when price drops to a support level.
  - **Short (Resistance)**: Sells when price rises to a resistance level.
- **Sublevels (Tiers)**: Scales into positions using 3 tiers per level.
- **Web UI**: Minimal dashboard to manage levels, view positions, and monitor trades.

## Getting Started

### Prerequisites

- Go 1.21+
- Bybit Account (Testnet or Mainnet)

### Configuration

1. Copy the example config:
   ```bash
   cp config/config.example.yaml config/config.yaml
   ```
2. Edit `config/config.yaml` and add your Bybit API keys.

### Running

```bash
# Run the bot
go run cmd/bot/main.go

# Or using Makefile
make run
```

Access the dashboard at `http://localhost:8080`.

## Development

### Running Tests

```bash
go test ./...
```

### Project Structure

- `cmd/`: Main entry points.
- `internal/domain/`: Core business entities and interfaces.
- `internal/usecase/`: Business logic (Trading engine, Level evaluation).
- `internal/infrastructure/`: External adapters (Exchange, Storage, Logger).
- `internal/web/`: HTTP handlers and templates.
