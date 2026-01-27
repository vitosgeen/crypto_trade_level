# Reverse Engineering Playbook: Level Bot Ecosystem

> **Date:** January 27, 2026
> **Scope:** Level Bot, Dashboard, and Analysis Pages
> **Target System:** Localhost Trading Environment (Port 8078)

## 1. Executive Summary

The "Level Bot" ecosystem is a comprehensive trading support system designed for crypto perpetual futures (Linear). It consists of three primary interfaces:
1.  **Dashboard (`/dashboard`)**: The command center for managing active "Levels" (planned entry zones), monitoring open positions, and executing trades.
2.  **Level Bot (`/level-bot`)**: A market scanner that filters and identifies high-potential assets based on Volatility, Open Interest (OI), and Price Trends across multiple timeframes (10m, 1h, 4h, 24h).
3.  **Analysis (`/analysis`)**: A post-processing tool that ingests historical data logs to detect "Consistent Trends" in Open Interest, providing deep insights into capital flow anomalies.

---

## 2. Architecture Overview

### 2.1 Backend Stack
-   **Language**: Go (Golang)
-   **Web Framework**: Standard `net/http` with `ServeMux`.
-   **Template Engine**: Go `html/template` with custom FuncMap (`mul`, `div`, `abs`).
-   **Data Persistence**:
    -   **Runtime**: In-memory caching (`LevelBotWorker`).
    -   **Logs**: JSON Lines (`.jsonl`) file storage in `logs/level_bot/` for historical analysis.
    -   **State**: Repository pattern for Levels and Trades (likely SQL/SQLite based on `LevelRepository` interfaces).

### 2.2 Data Flow
1.  **Exchange API** (Bybit/Linear) → **LevelBotWorker** (Background Cron 1m)
2.  **LevelBotWorker** → **In-Memory Cache** (served to `/level-bot`) & **JSONL Logs** (disk).
3.  **LogAnalyzerService** → Reads **JSONL Logs** → Computes Trends → Served to `/analysis`.
4.  **Dashboard Handler** → Direct Repository Access (Levels/Nodes) + Service Layer (Prices/Tiers) → Served to `/dashboard`.

---

## 3. Component Deep Dive

### 3.1 Dashboard (`/dashboard`)
**Primary Function:** Execution and Management.

#### UI Components (from Screens/DOM)
-   **Add Level Form**: Inputs for Symbol, Price, Base Size, Leverage, Tiers (1-3), TP %, and Auto-Mode.
-   **Levels Table**: Active trade setups. Shows distance to level, current price, and calculated Tier boundaries.
-   **Gauges & Charts**: Real-time indicators (Volume, OBI, TSI) visualization (likely via partials/JS).

#### Technical Implementation
-   **Handler**: `handleDashboard` (GET)
-   **Backend Logic**:
    -   Fetches active **Levels** from `levelRepo`.
    -   Fetches **Position History** from `tradeRepo`.
    -   **Real-time Calculation**:
        -   `evaluator.DetermineSide(LevelPrice, CurrentPrice)`: Decides if the level is Long (Price > Level) or Short (Price < Level) context.
        -   `evaluator.CalculateBoundaries(...)`: Dynamic generation of Tier entry prices based on configured percentages.
        -   `service.GetLevelState(...)`: detailed runtime stats (e.g., consecutive closes).
-   **Endpoint**: `/levels` (POST/DELETE) for modifying state.

### 3.2 Level Bot (`/level-bot`)
**Primary Function:** Discovery and Scanning.

#### UI Components
-   **Filters**: "Good for Levels", "High OI", "Stable".
-   **Fast Search**: Pre-defined tickers (BTC, ETH).
-   **Data Grid**:
    -   **Columns**: Symbol, Price, Vol/OI, Range (10m, 1h, 4h, 24h).
    -   **Highlights**: Badges for "Near High/Low" (e.g., "MAX 4H").

#### Technical Implementation
-   **Handler**: `handleLevelBot` (GET)
-   **Backend Logic (Worker)**:
    -   **Service**: `LevelBotWorker` (`internal/usecase/level_bot_worker.go`).
    -   **Lifecycle**:
        -   **Ticker**: Runs every 1 minute.
        -   **Step 1**: Fetch all "Trading" status linear instruments.
        -   **Step 2**: Sort by **Open Interest Value** (Price * OI) to prioritize high-liquidity coins.
        -   **Step 3 (Heavy Lift)**: For the **Top 60** coins, spawn Goroutines (limit 10 concurrent) to fetch Candles.
            -   **Candles Fetched**: 1m candles for 10m range, 1m candles for 1h range, 60m candles for 4h range.
        -   **Stats Computed**:
            -   **Range %**: `(Max - Min) / Min`.
            -   **Trend**: Direction from Start to End of range.
            -   **Proximity**: `Near4hMax` / `Near4hMin` (within 0.1% of boundary).
    -   **Persistence**: Saves snapshot to `logs/level_bot/data_YYYY-MM-DD.jsonl` every minute.

### 3.3 Analysis (`/analysis`)
**Primary Function:** Trend Validation and Anomaly Detection.

#### UI Components
-   **Anomaly Monitor**: Configurable alerts for OI shifts.
-   **Analysis Table**:
    -   **Columns**: Symbol, Current OI, Changes (1m - 24h).
    -   **Indicators**: "C" Badge for **Consistent** trends.

#### Technical Implementation
-   **Handler**: `handleLogAnalysis` (GET).
-   **Backend Logic**:
    -   **Service**: `LogAnalyzerService` (`internal/usecase/log_analyzer.go`).
    -   **Input**: Reads the *latest* `.jsonl` log file created by `LevelBotWorker`.
    -   **Reconstruction**: Builds a time-series of `{Time, OI, Price, Volume}` for every symbol in the log.
    -   **Calculations**:
        -   **Deltas**: Computes absolute and percentage change for 1m, 10m, 1h, 4h, 24h.
        -   **Consistency Algorithm**:
            -   Iterates through all logged data points in the timeframe.
            -   Counts `upSteps` vs `downSteps`.
            -   **Rule**: If `> 60%` of steps match the overall direction, mark `IsConsistent = true`.
    -   **Filtering**: Ignores Dated Futures (e.g., `-27FEB26`) and `.PERP` suffixes.
    -   **Sorting**: Default sort by **1h Change %** (Absolute).

## 4. API & Interaction Map

| Endpoint | Method | Purpose | Source Handler |
| :--- | :--- | :--- | :--- |
| `/dashboard` | GET | Main UI load | `handleDashboard` |
| `/levels` | POST | Create new Level | `handleAddLevel` |
| `/level-bot` | GET | Scanner UI | `handleLevelBot` |
| `/analysis` | GET | OI Analysis UI | `handleLogAnalysis` |
| `/api/analysis/chart` | GET | Historical OI/Price JSON for charts | `handleGetLogChartData` |

## 5. Critical Files

-   `internal/web/server.go`: Route definitions.
-   `internal/web/handlers.go`: Main UI controllers.
-   `internal/usecase/level_bot_worker.go`: The "Brain" of the scanner; handles data fetching, multithreading, and logging.
-   `internal/usecase/log_analyzer.go`: The "Analyst"; reads logs to compute consistency metrics.
