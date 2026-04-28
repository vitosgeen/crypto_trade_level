# Glossary

This document defines key terms used within the **Crypto Trade Level Bot** project, covering both trading terminology and project-specific concepts.

## Trading Terms

-   **Level**: A specific price point on a chart where a reversal is expected (Support or Resistance).
-   **Support**: A level where price is expected to stop falling and start rising (Long entry).
-   **Resistance**: A level where price is expected to stop rising and start falling (Short entry).
-   **Perpetual Futures**: A type of crypto derivative that has no expiry date, allowing traders to hold positions indefinitely.
-   **Long**: A position that profits if the price goes up.
-   **Short**: A position that profits if the price goes down.
-   **Tier**: A specific sub-level or "step" used to scale into a position (e.g., Tier 1, Tier 2, Tier 3).
-   **Scaling In**: The process of entering a position in multiple steps rather than all at once.
-   **PnL (Profit and Loss)**: 
    -   **Realized PnL**: Profit or loss from a closed position.
    -   **Unrealized PnL (uPnL)**: Floating profit or loss of an open position based on current market price.
-   **Take Profit (TP)**: A target price to close a position and lock in profit.
-   **Stop Loss (SL)**: A price point where a position is closed to prevent further losses.
-   **Taker Fee**: The fee paid when an order "takes" liquidity from the book (e.g., Market Order).
-   **Isolated Margin**: Margin allocated to a specific position, limiting risk to that amount.
-   **Cross Margin**: Margin shared across all positions in the account.

## Project-Specific Terms

-   **Defending Strategy**: The core logic of the bot—entering positions as price approaches a level to "defend" it.
-   **Base Size**: The initial quantity (in units of the asset) used for the first tier of an entry.
-   **Sublevel Engine**: The internal component that tracks which tiers have been triggered for a level.
-   **Profit Doubling**: A feature that increases the position size for a level after a successful trade.
-   **CoolDown**: A mandatory waiting period after a trade or trigger before the same level can be used again.
-   **Base Close**: A condition where a position is closed at the core Level Price because the expected reversal did not happen.
-   **Bybit Adapter**: The infrastructure component that translates the bot's internal commands into Bybit API calls.
-   **Level Service**: The central orchestrator that connects the exchange data to the trading logic and storage.
-   **Level Evaluator**: A utility that calculates the exact price points for tiers based on percentages.
