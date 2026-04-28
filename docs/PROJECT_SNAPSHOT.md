# Project Snapshot: Crypto Trade Level Bot

This document provides a condensed summary of the entire project for quick "memorization" or AI context.

## 🤖 Core Purpose
An autonomous crypto trading bot specializing in **Counter-Trend Level Defense**. It buys at support and sells at resistance using a tiered scaling approach on Bybit perpetual futures.

## 🏗️ Architecture (Clean)
-   **Domain**: Stateless entities (`Level`, `Order`) and interfaces (`Exchange`, `LevelRepository`).
-   **Usecase**: Logic orchestrators (`LevelService`), pure engines (`SublevelEngine`), and executors (`TradeExecutor`).
-   **Infrastructure**: `BybitAdapter` (REST/WS), `SQLiteStore` (SQL persistence).
-   **Web**: HTMX-powered dashboard for real-time monitoring.

## 📈 Strategy Specs
-   **Scaling**: 3 tiers per level (T1, T2, T3).
-   **Boundaries**: 
    -   Long: `Level * (1 + Pct)`
    -   Short: `Level * (1 - Pct)`
-   **Actions**: `OPEN` (Tier 1), `ADD` (Tier 2/3), `CLOSE` (TP/SL/Base).
-   **Optimization**: Profit doubling (2x size after win), Cooldowns (to prevent churning).

## 💾 Data Schema
-   `levels`: ID, Symbol, Price, Side, Tiers, Cooldown.
-   `trades`: Execution log, Price, Size, realized_pnl.
-   `position_history`: Completed cycles (Open -> Close).
-   `wallet_balances`: Equity snapshots.

## 🔌 API Details
-   **Exchange**: Bybit V5.
-   **Data**: WebSocket `tickers` stream for low-latency ticks.
-   **Execution**: Market orders with `reduceOnly` for exits.

## 🖥️ Web UI
-   **Stack**: Go Templates + HTMX.
-   **Features**: Positions table, Active levels, Trade history, Wallet stats, RSI signals.

## 🛠️ Dev Stack
-   **Language**: Go 1.21+
-   **Database**: SQLite (via `mattn/go-sqlite3`).
-   **Logging**: Zap (structured).
-   **Frontend**: HTMX + CSS.
