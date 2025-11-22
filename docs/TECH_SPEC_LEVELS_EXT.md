# Level-Defense Strategy — Short Explanation (English)

## 1. Core Idea
You set a **base level L**.  
The bot trades around this level using **3 symmetric tiers** above and below it.

- **Above L → LONG zone**
- **Below L → SHORT zone**

## 2. Tier Structure (Symmetric)
Let:
- t1 = Tier1 percent (outer)
- t2 = Tier2 percent (middle)
- t3 = Tier3 percent (inner)

### LONG side (above level)
Tier1_above = L * (1 + t1)
Tier2_above = L * (1 + t2)
Tier3_above = L * (1 + t3)

### SHORT side (below level)
Tier1_below = L * (1 - t1)
Tier2_below = L * (1 - t2)
Tier3_below = L * (1 - t3)

## 3. Trigger Direction
- **LONG triggers only when price moves DOWN from the top toward L**
- **LONG triggers only when price moves UP from the base level L**
- **SHORT triggers only when price moves DOWN from the base level L**
- **SHORT triggers only when price moves UP from the bottom toward L**

## 4. Trigger Sequence
1. Tier1  
2. Tier2  
3. Tier3  
4. Level Touch → CLOSE position

Each tier triggers only once until reset.

## 5. Order Multipliers
If BaseSize = B:
Tier1 → +1B
Tier2 → +1B
Tier3 → +2B

Total exposure after Tier3 = **4 × BaseSize**.

## 6. Level Touch
When price crosses L:
- Full position CLOSE  
- Reset Tier1/2/3 state
- Or close position by stop loss if StopLossAtBase is enabled