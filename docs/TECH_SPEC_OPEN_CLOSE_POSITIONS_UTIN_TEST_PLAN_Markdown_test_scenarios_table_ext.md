| ID | Category | Scenario Description | Initial State | Input (Price Movement) | Expected Behavior | Notes |
|----|-----------|----------------------|----------------|-------------------------|--------------------|--------|
| T1 | Tier Logic | Tier triggers only once until Base Level is reached | No position, Below Base | price crosses Tier1 upward multiple times without reaching Base | Only the first Tier1 SHORT entry executes; all further Tier1 triggers ignored | Prevents duplicate entries |
| T2 | Tier Logic | After Base Level touch, tiers reset and can trigger again | After SHORT entries, Base touched (position closed) | price rises or falls and recrosses Tier1 | Tier1 triggers again normally | Tier state resets only on Base |
