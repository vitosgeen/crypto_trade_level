@startuml
title LevelService State Machine (Short/Long Around Base Level)

[*] --> Idle

state Idle {
    [*] --> BelowBase

    state BelowBase {
        [*] --> WaitingForShort
        WaitingForShort --> Short_T1 : price crosses Tier1 from below
        Short_T1 --> Short_T2 : price crosses Tier2 from below
        Short_T2 --> Short_T3 : price crosses Tier3 from below

        Short_T1 --> Short_Hold : price falls (profit)
        Short_T2 --> Short_Hold : price falls (profit)
        Short_T3 --> Short_Hold : price falls (profit)

        Short_Hold --> WaitingForShort : price returns below tiers

        WaitingForShort --> CloseShort : price reaches or crosses BASE
        Short_T1 --> CloseShort : price reaches BASE
        Short_T2 --> CloseShort : price reaches BASE
        Short_T3 --> CloseShort : price reaches BASE
    }

    CloseShort --> BelowBase : price reverses down
    CloseShort --> AboveBase : price continues up
}

state AboveBase {
    [*] --> WaitingForLong

    WaitingForLong --> Long_T1 : price crosses Tier1 from above
    Long_T1 --> Long_T2 : price crosses Tier2
    Long_T2 --> Long_T3 : price crosses Tier3

    Long_T1 --> Long_Hold : price rises (profit)
    Long_T2 --> Long_Hold : price rises (profit)
    Long_T3 --> Long_Hold : price rises (profit)

    Long_Hold --> WaitingForLong : price returns above tiers

    WaitingForLong --> CloseLong : price reaches or crosses BASE
    Long_T1 --> CloseLong : price reaches BASE
    Long_T2 --> CloseLong : price reaches BASE
    Long_T3 --> CloseLong : price reaches BASE
}

CloseLong --> AboveBase : price reverses up
CloseLong --> BelowBase : price continues down

@enduml



📝 Chart Explanation
🔻 BLow Base (below the base)

⬆️ When the price moves from bottom to top:

Tier1 → open SHORT ×1

Tier2 → SHORT ×2

Tier3 → SHORT ×3

If the price is in the positive and went down → Short_Hold

If the price touched BASE → CloseShort

🔺 Above Base (above the base)

⬇️ When the price moves from top to bottom:

Tier1 → open LONG ×1

Tier2 → LONG ×2

Tier3 → LONG ×3

If the price is in the positive and went up → Long_Hold

If the price touched BASE → CloseLong

🔁 Switching modes

CloseShort → if the price went down → SHORT mode

CloseShort → if the price went up → LONG mode

CloseLong → if the price went up → LONG mode

CloseLong → if the price went down → SHORT mode