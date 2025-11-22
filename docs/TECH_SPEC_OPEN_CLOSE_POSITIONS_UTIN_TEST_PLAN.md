✅ FULL UNIT-TEST PLAN (34 Tests) — LevelService
LEGEND

BASE — базовий рівень

T1/T2/T3 — tier levels

SHORT — шорт

LONG — лонг

State — поточна позиція/множник/lastPrice

Action — що має зробити сервіс

🟥 A. INITIALIZATION — 3 tests
A1. Test_InitLastPrice_FirstTick_NoAction

First tick ever → sets lastPrice

No open/close should occur

A2. Test_IgnoreZeroOrNegativePrice

price = 0 / <0 → ignored

A3. Test_NoAction_When_PriceEqualsPrev

prev == curr → no triggers

🟦 B. SHORT ENTRY LOGIC (approach from below) — 6 tests
B1. Test_Short_Open_Tier1

Price crosses T1 upward from below → open SHORT (size ×1)

B2. Test_Short_Open_Tier2

Crosses T2 → open SHORT (size ×2)

B3. Test_Short_Open_Tier3

Crosses T3 → open SHORT (size ×3)

B4. Test_Short_Open_Tier_Bounce_NoDoubleTrigger

Price oscillates around tier within 0.05% → only one open

B5. Test_Short_MultipleEntries_When_Profitable

If SHORT is in profit → next short entry size = previous size ×2

B6. Test_Short_NoRepeatIfAlreadyOpened

If position already open and price crosses same tier → ignore

🟧 C. SHORT EXIT Logic (approach to BASE from below) — 5 tests
C1. Test_CloseShort_When_HitsBase

Price reaches BASE → close SHORT

C2. Test_CloseShort_When_CrossesBase

Price crosses BASE (e.g., 9999→10001) → close SHORT

C3. Test_CloseShort_Fails_ResetState

Close returns error → state reset → next trigger allowed

C4. Test_CloseShort_Then_ReopenShort_OnReverseDown

Close SHORT at BASE → price reverses down → T1 → open SHORT again

C5. Test_CloseShort_StopLossAtBase_Enabled

stopLossAtBase=true → close exactly on BASE even without crossing

🟩 D. SWITCH from SHORT → LONG (price continues above base) — 4 tests
D1. Test_ShortToLong_Switch_OnBaseBreak

Price rises: T1/T2/T3 SHORT entries → hit BASE → close → continue up → open LONG T1

D2. Test_Long_Open_Tier1_AfterShortClose

Price moves above BASE and hits LONG T1 → open LONG

D3. Test_NoShortReentry_WhenAboveBase

Once above BASE → only LONG logic allowed

D4. Test_Long_KeepOpen_While_Profitable_Uptrend

Price continues rising → LONG remains open

🟨 E. LONG ENTRY LOGIC (mirror reverse) — 5 tests
E1. Test_Long_Open_Tier1

Price crosses LONG T1 downward from above → open LONG

E2. Test_Long_Open_Tier2

Cross LONG T2 → open LONG (size ×2)

E3. Test_Long_Open_Tier3

Cross LONG T3 → open LONG (size ×3)

E4. Test_Long_MultipleEntries_When_Profitable

Same as short: if LONG profitable → next size doubled

E5. Test_Long_NoRepeatIfAlreadyOpened

Duplicated triggers ignored

🟫 F. LONG EXIT Logic (approach to BASE from above) — 5 tests
F1. Test_CloseLong_When_HitsBase

Mirror of C1

F2. Test_CloseLong_When_CrossesBase

Mirror of C2

F3. Test_CloseLong_Fails_ResetState

Mirror of C3

F4. Test_CloseLong_Then_ReopenLong_OnReverseUp

Mirror of C4

F5. Test_CloseLong_StopLossAtBase_Enabled

Mirror of C5

⬛ G. SWITCH from LONG → SHORT (price drops under base) — 4 tests
G1. Test_LongToShort_Switch_OnBaseBreak

Price above base → LONG → hit BASE → close → price drops → open SHORT

G2. Test_Short_Open_Tier1_AfterLongClose

After closing LONG, price hits SHORT T1 → open SHORT

G3. Test_NoLongReentry_WhenBelowBase

Once below base → only SHORT allowed

G4. Test_Short_KeepOpen_While_Profitable_Downtrend

Price continues falling → SHORT remains open

⚪ H. STATE MACHINE TESTS (internal logic) — 6 tests
H1. Test_Multiplier_Reset_OnClose

size multiplier resets → next entry = base size again

H2. Test_Multiplier_Double_OnProfit

profit condition triggers doubling

H3. Test_Multiplier_NoGrow_When_Loss

ensure no changes if position in loss

H4. Test_LastPrice_UpdatesEveryTick

after each tick

H5. Test_NoPanic_OnMissingTiers

null tiers → bot should safely ignore

H6. Test_NoPanic_OnMissingLevel

level repo returns nil → ignore tick

🟣 I. REPOSITORY / EXCHANGE ERROR HANDLING — 3 tests
I1. Test_GetSymbolTiers_Error_Handled

should not crash, no entry/exit

I2. Test_SaveLevel_Error_Handled

should not break state machine

I3. Test_ExchangeBuy_Fails_NoStateCorruption

buy fails → state remains correct