✅ Order Flow Indicators (Concise Definitions)
1. OBI — Order Book Imbalance

Measures the liquidity imbalance between BID and ASK sides in the order book.

Formula:
OBI = (BidDepth - AskDepth) / (BidDepth + AskDepth)

Interpretation:

OBI > 0 → bids dominate (bullish pressure)

OBI < 0 → asks dominate (bearish pressure)

2. CVD — Cumulative Volume Delta

Tracks the cumulative difference between aggressive buy and sell market orders over time.

Formula:
Delta_t = BuyMarketVolume_t - SellMarketVolume_t
CVD = Σ Delta_t

Interpretation:

CVD rising → aggressive buyers are in control

CVD falling → aggressive sellers dominate

Divergences between CVD and price are highly predictive


3. TSI — Trade Speed Index (Trade Velocity)

Measures how fast trades are coming in (trade frequency).

Formula:
TSI = NumberOfTrades(last X milliseconds) / X

Interpretation:

High TSI → fast market, high activity, possible breakout

Low TSI → slow market, low interest, ranging conditions


4. GLI — Grab Liquidity Imbalance

Measures which side of the order book is being actively “eaten” by market orders.

Formula:
GLI = ExecutedVolumeAtBid / ExecutedVolumeAtAsk

Interpretation:

GLI > 1 → sellers are hitting bids → bearish

GLI < 1 → buyers are lifting asks → bullish


5. Trade Velocity (Alternative TSI metric)

Measures the total traded volume per unit of time.

Formula:
TradeVelocity = TotalVolume(last X milliseconds) / X

Interpretation:

Spike in TradeVelocity = momentum burst (often right before moves)

Drop in TradeVelocity = exhaustion, consolidation





Indicators:
- OBI: Order Book Imbalance = (BidDepth - AskDepth) / (BidDepth + AskDepth).
- CVD: Cumulative Delta = cumulative(BuyMarketVol - SellMarketVol).
- TSI: Trade Speed Index = trades_count / time_window.
- GLI: Grab Liquidity Imbalance = ExecutedVolAtBid / ExecutedVolAtAsk.
- TradeVelocity: total_traded_volume / time_window.

Usage:
- OBI > 0 bullish, < 0 bearish.
- CVD rising = buyers aggressive; falling = sellers aggressive.
- High TSI/TradeVelocity = market is accelerating.
- GLI > 1 bearish (bids being eaten). GLI < 1 bullish (asks being lifted).
