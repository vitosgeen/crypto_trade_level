# LevelService — Test Scenarios Table

| ID | Category | Scenario Description | Initial State | Input (Price Movement) | Expected Behavior | Notes |
|----|-----------|----------------------|----------------|-------------------------|--------------------|--------|
| A1 | Init | First tick initializes lastPrice | No price history | price = X | lastPrice = X, no actions | Critical baseline |
| A2 | Init | Ignore zero/negative price | Any | price ≤ 0 | No actions | Validation |
| A3 | Init | Equal price produces no action | lastPrice = X | price = X | No tier trigger | Stability |

| B1 | Short Entry | Trigger SHORT Tier1 from below | No position | price crosses T1 upward | Open SHORT ×1 | Start short ladder |
| B2 | Short Entry | Trigger SHORT Tier2 | SHORT opened | price crosses T2 | Open SHORT ×2 | Add to ladder |
| B3 | Short Entry | Trigger SHORT Tier3 | SHORT opened | price crosses T3 | Open SHORT ×3 | Highest tier |
| B4 | Short Entry | Bounce around tier triggers only once | No position | price oscillates around T1 | Only one SHORT | Hysteresis protection |
| B5 | Short Entry | SHORT profit → next entry double size | SHORT in profit | rising to tier | Open SHORT with doubled size | Multiplier logic |
| B6 | Short Entry | Do not re-trigger tier if already opened | SHORT active | price crosses same tier | No new SHORT | Prevent duplicates |

| C1 | Short Exit | Close SHORT when touching BASE | SHORT active | price hits BASE | Close SHORT | stopLossAtBase = true |
| C2 | Short Exit | Close SHORT when crossing BASE | SHORT active | price X→BASE+ | Close SHORT | Pivot behavior |
| C3 | Short Exit | Close SHORT fails → reset state | SHORT active | Close returns error | State reset → allow new entry | Critical resilience |
| C4 | Short Exit | After close, price goes down → reopen SHORT | Close done | price reverses down to T1 | Open SHORT | Resume cycle |
| C5 | Short Exit | stopLossAtBase disabled → no close | SHORT active | price hits BASE | Do nothing | Configurable behavior |

| D1 | Switch Short→Long | Close SHORT at base, then open LONG tiers | SHORT active | price hits BASE then rises | Close SHORT → LONG T1 | Direction change |
| D2 | Switch Short→Long | Open LONG T1 after short close | No position | price reaches LONG T1 | Open LONG ×1 | Mirror logic |
| D3 | Switch Short→Long | No SHORT entries allowed above base | Above BASE | upward movement | Only LONG allowed | State consistency |
| D4 | Switch Short→Long | LONG profit → HOLD state | LONG active | rising more | Keep LONG, no action | Spiral uptrend |

| E1 | Long Entry | Trigger LONG Tier1 from above | No position | price crosses T1 downward | Open LONG ×1 | Ladder start |
| E2 | Long Entry | Trigger LONG Tier2 | LONG active | crosses T2 | Open LONG ×2 | Ladder continues |
| E3 | Long Entry | Trigger LONG Tier3 | LONG active | crosses T3 | Open LONG ×3 | Highest tier |
| E4 | Long Entry | LONG profit → next entry double size | LONG active profitable | downward retrace | Open LONG double size | Multiplier logic |
| E5 | Long Entry | Do not re-trigger same LONG tier | LONG active | oscillation at tier | No new LONG | Prevent duplicates |

| F1 | Long Exit | Close LONG when touching BASE | LONG active | price hits BASE | Close LONG | stopLossAtBase = true |
| F2 | Long Exit | Close LONG when crossing BASE | LONG active | price X→BASE- | Close LONG | Pivot behavior |
| F3 | Long Exit | Close LONG fails → reset state | LONG active | Close error | Reset → allow new entry | Robustness |
| F4 | Long Exit | After close, price goes up → reopen LONG | Close done | price reverses up to T1 | Open LONG | Cycle continue |
| F5 | Long Exit | stopLossAtBase disabled → no close | LONG active | hits BASE | Do nothing | Configurable |

| G1 | Switch Long→Short | Close LONG at base, then open SHORT | LONG active | hits BASE then falls | Close LONG → SHORT T1 | Direction shift |
| G2 | Switch Long→Short | Open SHORT T1 after long close | No position | price reaches SHORT T1 | Open SHORT | Mirror scenario |
| G3 | Switch Long→Short | No LONG entries allowed below BASE | Below BASE | downward movement | Only SHORT allowed | State consistency |
| G4 | Switch Long→Short | SHORT profit → HOLD state | SHORT active | falling more | No action (hold) | Downtrend hold |

| H1 | Multiplier | Reset multiplier after close | any position | close event | multiplier=1 | Required |
| H2 | Multiplier | Double multiplier on profitable entry | profitable pos | new tier trigger | size ×2 | Strategy rule |
| H3 | Multiplier | No multiplier increase if losing | losing pos | new tier | size stays same | Protect capital |

| H4 | Internal | lastPrice updates every tick | any | price sequence | lastPrice updated | Core mechanic |
| H5 | Internal | Safe ignore missing tiers | tiers=nil | any price | no action | Safe behavior |
| H6 | Internal | Safe ignore missing level | level=nil | any price | no action | Avoid nil panic |

| I1 | Repo Errors | GetSymbolTiers error handled | any | repo returns error | no panic, skip tick | Must test |
| I2 | Repo Errors | SaveLevel error handled | any | save error | no panic | Resilience |
| I3 | Exchange Errors | Buy fails → state not corrupted | any | MarketBuy error | state intact | Critical safety |

