# Take Profit Strategies (Proposed)

This document outlines potential strategies for implementing "Smart" Take Profit logic in the trading bot.

## 1. Liquidity-Based TP (Recommended)
**Concept**: Automatically place Take Profit orders at the next major **Liquidity Cluster** (resistance for Longs, support for Shorts).

*   **Why**: Liquidity clusters represent natural price barriers where price often reverses or stalls. Since the system already fetches this data for Auto-Levels, it can be reused for exit targeting.
*   **Logic**:
    *   **Long Position**: Scan the order book *above* the entry price. Identify the cluster with the highest volume within a reasonable range (e.g., 5-10%). Set TP slightly below this price (e.g., -0.1%).
    *   **Short Position**: Scan the order book *below* the entry price. Identify the cluster with the highest volume. Set TP slightly above this price.
*   **Pros**: Adapts to real-time market structure.
*   **Cons**: Requires reliable order book data; clusters can move or be pulled (spoofing).

## 2. Sentiment-Adjusted TP
**Concept**: Adjust the static `TakeProfitPct` based on the **Conclusion Score** (Market Sentiment).

*   **Why**: In a strong trend, fixed targets often leave money on the table. In a weak trend, they might never be hit.
*   **Logic**:
    *   `TargetTP = BaseTP * (1 + (ConclusionScore * Factor))`
    *   **Bullish Example**: BaseTP = 2%. Score = 0.8 (Strong Bull). Factor = 0.5.
        *   `TargetTP = 2% * (1 + 0.4) = 2.8%`.
    *   **Bearish Example (for Long)**: BaseTP = 2%. Score = -0.5 (Bearish).
        *   `TargetTP = 2% * (1 - 0.25) = 1.5%`.
*   **Pros**: Dynamic and responsive to overall market health.
*   **Cons**: Relies on the accuracy of the Conclusion Score.

## 3. Tiered Scaling Out
**Concept**: Exit the position in fractional tiers, similar to the entry strategy.

*   **Why**: Secures profit along the way (reducing risk) while keeping a portion of the position open for larger moves ("runners").
*   **Logic**:
    *   **TP 1**: Close 33% of position at +1% profit.
    *   **TP 2**: Close 33% of position at +2% profit.
    *   **TP 3**: Close remaining 34% at +5% profit (or trail stop).
*   **Pros**: Psychologically easier to manage; guarantees some profit on smaller moves.
*   **Cons**: Reduces maximum potential profit if the price goes straight to the highest target.

## 4. Hybrid Approach
**Concept**: Combine Liquidity-Based targeting with Tiered exits.

*   **Logic**:
    *   **TP 1 (50%)**: Fixed conservative percentage (e.g., 1%) to cover fees and bank profit.
    *   **TP 2 (50%)**: Dynamic target based on the nearest Liquidity Cluster.
