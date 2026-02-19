# Technical Specification: RSI-Based Auto Level Creation

## 1. Objective
Enable the Crypto Trade Level Bot to automatically create support and resistance levels based on the Relative Strength Index (RSI) indicator. This reduces manual intervention and allows the bot to react to overbought/oversold conditions programmatically.

## 2. RSI Core Logic

### 2.1. Basic RSI Triggers
The system will monitor RSI for a configured set of symbols and timeframes (default: 15m).
*   **Oversold Trigger (RSI < 30)**:
    *   **Action**: Create a **Support (Long)** level at the current market price.
    *   **Parameters**: Uses default Tiers, Leverage, and Take Profit settings for the symbol.
*   **Overbought Trigger (RSI > 70)**:
    *   **Action**: Create a **Resistance (Short)** level at the current market price.
    *   **Parameters**: Uses default Tiers, Leverage, and Take Profit settings for the symbol.

### 2.2. RSI Trend Break Trading (Advanced)
This strategy identifies local peaks and troughs in the RSI indicator to draw trend lines.
*   **Bullish Break**: 
    1. Identify a descending trend line connecting recent RSI peaks.
    2. Trigger when RSI crosses ABOVE the descending trend line.
    3. **Action**: Create a **Support (Long)** level.
*   **Bearish Break**:
    1. Identify an ascending trend line connecting recent RSI troughs.
    2. Trigger when RSI crosses BELOW the ascending trend line.
    3. **Action**: Create a **Resistance (Short)** level.

## 3. Architecture Changes

### 3.1. MarketService Enhancements (`internal/usecase/market_service.go`)
*   Add `GetRSI(ctx context.Context, symbol, interval string, period int) (float64, error)`.
*   Implement standard RSI calculation using `GetCandles` from the `Exchange` interface.
*   Maintain a small cache of RSI values to avoid redundant exchange API calls.

### 3.2. New RSI Monitoring Service (`internal/usecase/rsi_monitor_service.go`)
A new background service that:
1. Orchestrates the scanning loop.
2. Stores configuration for which symbols to monitor.
3. Evaluates basic RSI triggers (30/70).
4. Implements trend detection logic for RSI trend breaks.
5. Calls `LevelService.CreateLevel` upon trigger.

### 3.3. Configuration / Settings
Add new configuration parameters (via `.env` or Database):
*   `RSI_AUTO_CREATE_ENABLED`: Global toggle.
*   `RSI_TIMEFRAME`: (e.g., 1m, 5m, 15m, 1h).
*   `RSI_PERIOD`: (default 14).
*   `RSI_OVERSOLD_THRESHOLD`: (default 30).
*   `RSI_OVERBOUGHT_THRESHOLD`: (default 70).
*   `RSI_TREND_BREAK_ENABLED`: Boolean toggle.

## 4. RSI Trend Break Implementation Details
To detect trend breaks programmatically:
1. **Pivot Finding**: Use a window of $N$ candles to find local maxima and minima in RSI values.
2. **Trendline Calculation**: Use the two most recent pivots of the same type (e.g., two most recent RSI highs).
3. **Crossover Detection**: Check if the current RSI value has crossed the value of the linear equation $y = mx + b$ formed by the pivots.

## 5. UI Requirements (`internal/web`)
*   **Settings Page**: Add sliders/inputs for RSI thresholds.
*   **Dashboard**: Display current RSI values for active symbols next to their current price.
*   **Logs**: Audit logs to indicate "Level created by RSI Trigger (Oversold: 28.5)".

## 6. Implementation Phases
1.  **Phase 1**: Implement RSI calculation in `MarketService`.
2.  **Phase 2**: Implement the `RSIMonitorService` with basic 30/70 logic.
3.  **Phase 3**: Add UI controls and visualizers.
4.  **Phase 4**: Implement advanced Trend Breaking logic.
