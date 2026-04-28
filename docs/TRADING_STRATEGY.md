# Trading Strategy: Level Defense

The bot implements a **Counter-Trend (Defending)** strategy. It identifies key price levels (Support or Resistance) and "defends" them by scaling into positions as the price approaches the level, expecting a reversal.

## Core Concepts

### 1. Level Identification
-   **Support Level**: A price point where the bot expects buying pressure. If the price falls to this level, the bot opens a **Long** position.
-   **Resistance Level**: A price point where the bot expects selling pressure. If the price rises to this level, the bot opens a **Short** position.

### 2. Tiered Entry (Scaling)
Instead of entering the full position at once, the bot uses **3 Tiers** to scale in. This improves the average entry price and reduces risk if the level is slightly breached before reversing.

#### Tier Calculations (Support / Long)
Tiers are calculated as a percentage *above* the Level Price:
-   **Tier 1 (Outermost)**: `LevelPrice * (1 + Tier1Pct)`
-   **Tier 2 (Middle)**: `LevelPrice * (1 + Tier2Pct)`
-   **Tier 3 (Innermost)**: `LevelPrice * (1 + Tier3Pct)`

*Typical Pcts: Tier 1 = 0.01 (1%), Tier 2 = 0.005 (0.5%), Tier 3 = 0.002 (0.2%).*

#### Tier Calculations (Resistance / Short)
Tiers are calculated as a percentage *below* the Level Price:
-   **Tier 1 (Outermost)**: `LevelPrice * (1 - Tier1Pct)`
-   **Tier 2 (Middle)**: `LevelPrice * (1 - Tier2Pct)`
-   **Tier 3 (Innermost)**: `LevelPrice * (1 - Tier3Pct)`

### 3. Execution Logic
The `SublevelEngine` monitors price ticks and triggers actions based on "crossings":

-   **Long Entry**: Triggered when price falls **downwards** through a tier boundary.
-   **Short Entry**: Triggered when price rises **upwards** through a tier boundary.

#### Action Types
-   **OPEN**: Triggered by Tier 1. Opens the initial position.
-   **ADD**: Triggered by Tier 2 or Tier 3. Increases the existing position size.
-   **CLOSE**: Triggered when the exit condition is met (Take Profit or Stop Loss).

### 4. Position Sizing
-   **Tier 1**: `BaseSize`
-   **Tier 2**: `BaseSize`
-   **Tier 3**: `2 * BaseSize` (Aggressive defense near the core level)

### 5. Exit Strategy
-   **Take Profit (TP)**: Defined as a percentage from the average entry price or fixed price.
-   **Stop Loss (SL)**: Usually triggered if the level is decisively broken (price moves significantly past the Level Price).
-   **Base Close**: A special condition where the position is closed at the "base" level if the strategy fails to catch a reversal.

## Profit Doubling
The bot tracks `ConsecutiveWins`. If a level has successfully defended and closed in profit, the bot may double the `BaseSize` for the next entry on that same level to capitalize on its strength.

## Cooldowns
After a tier is triggered or a position is closed, the level enters a **Cooldown** period to prevent "churning" (opening and closing repeatedly in a choppy market).
-   `CoolDownMs`: General cooldown after any trigger.
-   `BaseCloseCooldownMs`: Long cooldown if the position was closed at the base level (indicating the level is being heavily tested).
