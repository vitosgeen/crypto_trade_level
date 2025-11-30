Pull request overview

This PR introduces advanced order flow indicators and a base close cooldown mechanism to prevent excessive losses when level defense strategies fail repeatedly.
Key Changes

    Order Flow Indicators: Adds OBI (Order Book Imbalance), CVD (Cumulative Volume Delta), TSI (Trade Speed Index), GLI (Grab Liquidity Imbalance), and Trade Velocity metrics to analyze market microstructure
    Base Close Cooldown: Implements a cooldown mechanism that temporarily disables levels after consecutive base closes to prevent rapid loss accumulation
    UI Enhancements: Adds 5 new gauges to visualize the order flow indicators in real-time

Reviewed changes

Copilot reviewed 14 out of 14 changed files in this pull request and generated 11 comments.
Show a summary per file
File 	Description
internal/domain/level.go 	Adds MaxConsecutiveBaseCloses and BaseCloseCooldownMs fields to Level struct
internal/domain/interfaces.go 	Adds Subscribe method to Exchange interface for real-time data subscriptions
internal/infrastructure/storage/sqlite.go 	Updates database schema and queries to support new level configuration fields
internal/usecase/market_service.go 	Implements calculation logic for 5 new order flow indicators and subscription management
internal/usecase/sublevel_engine.go 	Adds DisabledUntil and ConsecutiveBaseCloses tracking to LevelState
internal/usecase/level_service.go 	Implements base close tracking and cooldown trigger logic in position management
internal/web/handlers.go 	Handles new form parameters for base close configuration
internal/web/templates/index.html 	Adds form inputs for cooldown config and 5 new indicator gauges with JavaScript visualization
internal/web/templates/coin_detail.html 	Adds 3 new indicator gauges (OBI, TSI, GLI) to coin detail page
tests/e2e_test.go 	Adds Subscribe method stub to MockExchange
internal/usecase/market_service_test.go 	Adds Subscribe method stub to MockExchange
internal/usecase/level_service_test.go 	Adds Subscribe method stubs to mock exchanges
internal/usecase/level_service_cooldown_test.go 	New test file validating base close cooldown behavior
docs/TECH_SPEC_LEVELS_NEW_FEATURES_1.md 	Documents formulas and interpretations for all order flow indicators

💡 Add Copilot custom instructions for smarter, more guided reviews. Learn how to get started.

In internal/web/templates/coin_detail.html:

> @@ -610,6 +661,37 @@ <h2>Quick Actions</h2>
                 const divValue = document.getElementById('div-value');
                 if (divValue) divValue.textContent = divergence.toFixed(2);
 
+                // Update OBI
+                // OBI is -1 to 1. Map to -90 to 90 degrees.
+                const obiDeg = stats.obi * 90;
+                const obiNeedle = document.getElementById('obi-needle');
+                if (obiNeedle) obiNeedle.style.transform = `rotate(${obiDeg}deg)`;
+                const obiValue = document.getElementById('obi-value');
+                if (obiValue) obiValue.textContent = stats.obi.toFixed(2);
+
+                // Update TSI
+                // Assume max 1000 trades/min for full scale?

The comment states "Assume max 1000 trades/min for full scale?" but the TSI calculation in market_service.go actually produces trades per second (not per minute), making this comment inaccurate. The comment should be corrected to "trades/sec" or the implementation should be adjusted to match the comment.
⬇️ Suggested change

-                // Assume max 1000 trades/min for full scale?
+                // Assume max 1000 trades/sec for full scale?

In internal/usecase/market_service.go:

> +	if !s.subscribed[symbol] {
+		// Subscribe to real-time updates
+		if err := s.exchange.Subscribe([]string{symbol}); err == nil {
+			s.subscribed[symbol] = true
+		} else {
+			// Log error? For now just continue, maybe next time it succeeds
+		}
+	}

The subscription logic silently ignores errors from s.exchange.Subscribe(). While there's a comment saying "Log error? For now just continue", this could lead to missing real-time updates without any visibility. Consider at minimum logging the error so operators can detect subscription failures:

if err := s.exchange.Subscribe([]string{symbol}); err != nil {
    log.Printf("WARNING: Failed to subscribe to %s: %v", symbol, err)
} else {
    s.subscribed[symbol] = true
}

In internal/usecase/level_service.go:

> +					// Update State for Base Close
+					s.engine.UpdateState(activeLevel.ID, func(ls *LevelState) {
+						ls.ConsecutiveBaseCloses++
+						ls.ConsecutiveWins = 0 // Reset wins on loss
+						log.Printf("AUDIT: Stop Loss (Base) recorded for Level %s. Count: %d", activeLevel.ID, ls.ConsecutiveBaseCloses)
+
+						if activeLevel.MaxConsecutiveBaseCloses > 0 && ls.ConsecutiveBaseCloses >= activeLevel.MaxConsecutiveBaseCloses {
+							ls.DisabledUntil = time.Now().Add(time.Duration(activeLevel.BaseCloseCooldownMs) * time.Millisecond)
+							ls.ConsecutiveBaseCloses = 0
+							log.Printf("AUDIT: Level %s disabled until %v due to max base closes (SL).", activeLevel.ID, ls.DisabledUntil)
+						}
+					})

There's a potential issue with duplicate tracking of base closes. The code increments ConsecutiveBaseCloses in two separate places:

    In ProcessTick when "Stop Loss (Base)" is triggered (lines 314-325)
    In processLevel when ActionClose is triggered with a loss (lines 444-451)

However, if the engine's ActionClose logic is triggered when price crosses back to the level, AND the StopLossAtBase check in ProcessTick also fires, this could lead to double-counting of the same base close event or inconsistent cooldown triggers.

The two paths should be coordinated to ensure that a single base close event is only counted once. Consider adding a flag or restructuring the logic to prevent double-counting.

In internal/usecase/market_service.go:

> +	// TSI = NumberOfTrades / TimeWindow (60s)
+	tsi := float64(tradeCount) / 60.0

The TSI (Trade Speed Index) calculation unit is inconsistent with the documentation. The documentation states:

    TSI = NumberOfTrades(last X milliseconds) / X

However, the implementation divides by 60 (seconds), not 60,000 (milliseconds). This means the result is trades per second, not trades per millisecond as specified in the documentation.

Either the implementation should divide by 60,000 (for trades per millisecond) or the documentation/comment should be updated to reflect that this is trades per second.
⬇️ Suggested change

-	// TSI = NumberOfTrades / TimeWindow (60s)
-	tsi := float64(tradeCount) / 60.0
+	// TSI = NumberOfTrades(last 60,000 ms) / 60,000 (trades per millisecond)
+	tsi := float64(tradeCount) / 60000.0

In internal/web/templates/index.html:

>                  const divergence = speedRatio - depthRatio;
                 const divDeg = divergence * 90;
                 const divNeedle = document.getElementById('div-needle');
                 if (divNeedle) divNeedle.style.transform = `rotate(${divDeg}deg)`;
                 const divValue = document.getElementById('div-value');
                 if (divValue) divValue.textContent = divergence.toFixed(2);
 
+                // 5. OBI
+                const obiDeg = stats.obi * 90;
+                const obiNeedle = document.getElementById('obi-needle');
+                if (obiNeedle) obiNeedle.style.transform = `rotate(${obiDeg}deg)`;
+                const obiValue = document.getElementById('obi-value');
+                if (obiValue) obiValue.textContent = stats.obi.toFixed(2);
+
+                // 6. TSI
+                const maxTsi = 1000;

The TSI gauge scaling logic uses a magic number maxTsi = 1000 which appears arbitrary. This value should either be:

    Derived from the actual data or market conditions
    Made configurable
    Documented with an explanation of why 1000 was chosen

Additionally, since TSI represents trades per second (as implemented), this value means the gauge maxes out at 1000 trades/second, which may or may not be appropriate depending on the market.

In internal/usecase/market_service.go:

> +	// 4. Calculate Indicators
+	// OBI = (BidDepth - AskDepth) / (BidDepth + AskDepth)
+	var obi float64
+	if avgBid+avgAsk > 0 {
+		obi = (avgBid - avgAsk) / (avgBid + avgAsk)
+	}
+
+	// CVD (60s) = SpeedBuy - SpeedSell
+	cvd := speedBuy - speedSell
+
+	// TSI = NumberOfTrades / TimeWindow (60s)
+	tsi := float64(tradeCount) / 60.0
+
+	// GLI = ExecutedVolumeAtBid / ExecutedVolumeAtAsk = SpeedSell / SpeedBuy
+	var gli float64 = 1.0
+	if speedBuy > 0 {
+		gli = speedSell / speedBuy
+	} else if speedSell > 0 {
+		gli = 10.0 // Max cap if no buys
+	}
+
+	// TradeVelocity = TotalVolume / TimeWindow (60s)
+	tradeVelocity := (speedBuy + speedSell) / 60.0
+
 	return &MarketStats{
 		SpeedBuy:       speedBuy,
 		SpeedSell:      speedSell,
 		DepthBid:       avgBid,
 		DepthAsk:       avgAsk,
 		PriceChange60s: priceChange60s,
+		OBI:            obi,
+		CVD:            cvd,
+		TSI:            tsi,
+		GLI:            gli,
+		TradeVelocity:  tradeVelocity,

The new indicator calculations (OBI, CVD, TSI, GLI, TradeVelocity) added to GetMarketStats lack test coverage. Given that the existing market_service_test.go file has tests for the depth average functionality, these new indicators should also have unit tests to verify:

    Correct calculation formulas
    Edge cases (e.g., division by zero, empty trade data)
    Boundary conditions for clamping/capping values

In internal/usecase/market_service.go:

>  	s.mu.Lock()
+	if !s.subscribed[symbol] {
+		// Subscribe to real-time updates
+		if err := s.exchange.Subscribe([]string{symbol}); err == nil {
+			s.subscribed[symbol] = true
+		} else {
+			// Log error? For now just continue, maybe next time it succeeds
+		}
+	}
 

The Subscribe call is made while holding the s.mu mutex (line 123). If the Subscribe method blocks or takes a long time (e.g., network operation), this will block all other operations on the MarketService that need the mutex, including:

    Other calls to GetMarketStats
    The handleTrade callback which updates trade data

Consider moving the Subscribe call outside the mutex lock, or using a separate mutex for the subscribed map to avoid blocking critical data operations.
⬇️ Suggested change

-	s.mu.Lock()
-	if !s.subscribed[symbol] {
-		// Subscribe to real-time updates
-		if err := s.exchange.Subscribe([]string{symbol}); err == nil {
-			s.subscribed[symbol] = true
-		} else {
-			// Log error? For now just continue, maybe next time it succeeds
-		}
-	}
-
+	needSubscribe := false
+	s.mu.Lock()
+	if !s.subscribed[symbol] {
+		needSubscribe = true
+	}
+	s.mu.Unlock()
+
+	if needSubscribe {
+		// Subscribe to real-time updates
+		if err := s.exchange.Subscribe([]string{symbol}); err == nil {
+			s.mu.Lock()
+			s.subscribed[symbol] = true
+			s.mu.Unlock()
+		} else {
+			// Log error? For now just continue, maybe next time it succeeds
+		}
+	}
+	s.mu.Lock()

In internal/usecase/market_service.go:

> +	// CVD (60s) = SpeedBuy - SpeedSell
+	cvd := speedBuy - speedSell

The CVD (Cumulative Volume Delta) calculation is incorrect. According to the documentation, CVD should be a cumulative sum (Σ Delta_t) over time, but the current implementation only calculates the delta for the last 60 seconds (speedBuy - speedSell), which is just a simple delta, not a cumulative value.

The implementation should track and sum deltas across multiple time periods, not just calculate the difference for a single 60-second window. This would require storing historical CVD values or maintaining a running sum.

In internal/usecase/market_service.go:

> @@ -223,12 +240,41 @@ func (s *MarketService) GetMarketStats(ctx context.Context, symbol string) (*Mar
 		}
 	}
 
+	// 4. Calculate Indicators
+	// OBI = (BidDepth - AskDepth) / (BidDepth + AskDepth)
+	var obi float64
+	if avgBid+avgAsk > 0 {
+		obi = (avgBid - avgAsk) / (avgBid + avgAsk)
+	}
+
+	// CVD (60s) = SpeedBuy - SpeedSell
+	cvd := speedBuy - speedSell
+
+	// TSI = NumberOfTrades / TimeWindow (60s)
+	tsi := float64(tradeCount) / 60.0
+
+	// GLI = ExecutedVolumeAtBid / ExecutedVolumeAtAsk = SpeedSell / SpeedBuy

The GLI (Grab Liquidity Imbalance) formula implementation appears to be inverted. According to the documentation:

    GLI = ExecutedVolumeAtBid / ExecutedVolumeAtAsk
    ExecutedVolumeAtBid = volume executed at the bid price (sellers hitting bids) = speedSell
    ExecutedVolumeAtAsk = volume executed at the ask price (buyers lifting asks) = speedBuy

However, the current implementation calculates gli = speedSell / speedBuy, which is correct.

But the comment on line 256 is misleading - it states the formula is "ExecutedVolumeAtBid / ExecutedVolumeAtAsk = SpeedSell / SpeedBuy" without clearly explaining the mapping. This should clarify that speedSell represents sellers hitting bids (ExecutedVolumeAtBid) and speedBuy represents buyers lifting asks (ExecutedVolumeAtAsk).
⬇️ Suggested change

-	// GLI = ExecutedVolumeAtBid / ExecutedVolumeAtAsk = SpeedSell / SpeedBuy
+	// GLI = ExecutedVolumeAtBid / ExecutedVolumeAtAsk
+	// ExecutedVolumeAtBid: volume executed at bid price (sellers hitting bids) = speedSell
+	// ExecutedVolumeAtAsk: volume executed at ask price (buyers lifting asks) = speedBuy
+	// Therefore, GLI = speedSell / speedBuy

In internal/usecase/market_service.go:

> +	} else if speedSell > 0 {
+		gli = 10.0 // Max cap if no buys

The GLI calculation uses an arbitrary max cap of 10.0 when speedBuy is zero but speedSell is positive. This magic number lacks justification and could lead to inconsistent behavior. Consider either:

    Using a more principled approach (e.g., a maximum based on typical market conditions)
    Documenting why 10.0 was chosen
    Using a named constant instead of a magic number

In internal/usecase/level_service.go:

> +					ls.ConsecutiveBaseCloses++
+					log.Printf("AUDIT: Base Close recorded for Level %s. Count: %d", level.ID, ls.ConsecutiveBaseCloses)

The logic for tracking base closes is problematic. When realizedPnL <= 0 (loss), the code increments ConsecutiveBaseCloses and assumes this is always a "base close" scenario. However, not all losses are base closes - a position could also be closed at a loss due to other reasons like "Sentiment Exit" or "Safety Exit".

The code should only increment ConsecutiveBaseCloses when the close reason is specifically a base close (e.g., "Level Cross" or "Stop Loss (Base)"), not for all losses. Consider passing the close reason to this callback or checking it explicitly.


@Copilot commented on this pull request.
Pull request overview

Copilot reviewed 16 out of 16 changed files in this pull request and generated 7 comments.

💡 Add Copilot custom instructions for smarter, more guided reviews. Learn how to get started.

In internal/usecase/market_service.go:

> +		if err := s.exchange.Subscribe([]string{symbol}); err == nil {
+			s.mu.Lock()
+			s.subscribed[symbol] = true
+			s.mu.Unlock()
+		} else {
+			// Log error so operators can detect subscription failures
+			// I need to add "log" to imports.
+			// For now, I will skip logging or add it if I can update imports.
+			// I will update imports in a separate step or assume it's there.
+			// Wait, previous file view showed:
+			// import (
+			// 	"context"
+			// 	"sort"
+			// 	"sync"
+			// 	"time"
+			// 	"github.com/vitos/crypto_trade_level/internal/domain"
+			// )
+			// So "log" is missing. I should add it.
+		}

The subscription error handling contains extensive inline TODO comments that should be removed. The code should either import the log package and implement proper error logging, or remove these comments. These internal development notes reduce code readability.

In internal/usecase/market_service.go:

> @@ -223,12 +270,45 @@ func (s *MarketService) GetMarketStats(ctx context.Context, symbol string) (*Mar
 		}
 	}
 
+	// 4. Calculate Indicators
+	// OBI = (BidDepth - AskDepth) / (BidDepth + AskDepth)
+	var obi float64
+	if avgBid+avgAsk > 0 {
+		obi = (avgBid - avgAsk) / (avgBid + avgAsk)
+	}
+
+	// CVD = Cumulative Volume Delta (Running Sum)
+	cvd := s.cvdAccumulator[symbol]

The CVD calculation is inconsistent with documentation and the test expectations. According to the specification, CVD should be a cumulative sum over time (Σ Delta_t). However, the code updates cvdAccumulator in handleTrade (lines 84-89) using size * price, while the test expects it to reflect SpeedBuy - SpeedSell. The cvdAccumulator should accumulate (size * price) for buys and subtract (size * price) for sells, which is what's implemented in handleTrade. But this makes the CVD value grow unbounded over the lifetime of the service, which may not be the intended behavior. Consider whether CVD should reset periodically or be calculated over a 60s window like other metrics.

In internal/usecase/market_service.go:

> +	// CVD = Cumulative Volume Delta (Running Sum)
+	cvd := s.cvdAccumulator[symbol]
+
+	// TSI = Trade Speed Index (Trades per Second)
+	// NumberOfTrades / TimeWindow (60s)
+	tsi := float64(tradeCount) / 60.0
+
+	// GLI = ExecutedVolumeAtBid / ExecutedVolumeAtAsk
+	// ExecutedVolumeAtBid: volume executed at bid price (sellers hitting bids) = speedSell
+	// ExecutedVolumeAtAsk: volume executed at ask price (buyers lifting asks) = speedBuy
+	// Therefore, GLI = speedSell / speedBuy
+	var gli float64 = 1.0
+	if speedBuy > 0 {
+		gli = speedSell / speedBuy
+	} else if speedSell > 0 {
+		gli = MaxGLI // Max cap if no buys

[nitpick] The MaxGLI constant value of 10.0 is used as a cap when speedBuy is zero but lacks justification. Consider documenting why 10.0 was chosen as the maximum value, or making it configurable based on market conditions.

In internal/usecase/level_service.go:

> +				if reason == "Stop Loss (Base)" || reason == "Level Cross" {
+					ls.ConsecutiveBaseCloses++
+					log.Printf("AUDIT: Base Close recorded for Level %s. Count: %d", levelID, ls.ConsecutiveBaseCloses)

The logic increments ConsecutiveBaseCloses for any loss with reason 'Level Cross', but not all 'Level Cross' events represent base closes. A 'Level Cross' could occur at a profitable exit or when closing at tiers. The condition should be more specific - only increment when the close is at a loss AND specifically at the base level. Consider checking if the close price is at or near the level's base price.
⬇️ Suggested change

-				if reason == "Stop Loss (Base)" || reason == "Level Cross" {
-					ls.ConsecutiveBaseCloses++
-					log.Printf("AUDIT: Base Close recorded for Level %s. Count: %d", levelID, ls.ConsecutiveBaseCloses)
+				if (reason == "Stop Loss (Base)" || reason == "Level Cross") && activeLevel != nil {
+					// Only increment if close price is at/near base price
+					const epsilon = 0.001 // 0.1% tolerance
+					if closePrice >= activeLevel.BasePrice*(1-epsilon) && closePrice <= activeLevel.BasePrice*(1+epsilon) {
+						ls.ConsecutiveBaseCloses++
+						log.Printf("AUDIT: Base Close recorded for Level %s. Count: %d", levelID, ls.ConsecutiveBaseCloses)
+					}

In internal/usecase/level_service.go:

> +				if reason == "Stop Loss (Base)" || reason == "Level Cross" {
+					ls.ConsecutiveBaseCloses++
+					log.Printf("AUDIT: Base Close recorded for Level %s. Count: %d", levelID, ls.ConsecutiveBaseCloses)
+
+					// Check Cooldown
+					// We need to fetch the level to check config.
+					// Since we are in a callback, we can't easily fetch async?
+					// We can assume we have the level config in cache.
+					// But UpdateState is sync.
+					// Let's try to get level from cache.
+					// Warning: s.levelsCache requires lock. We are not holding lock here.
+					// But s.engine.UpdateState locks the engine state, not the service.
+					// So we can acquire service lock.
+
+					// However, calling s.levelsCache here might be expensive or complex if we don't have symbol.
+					// We have symbol.
+
+					// Let's find the level in cache.
+					s.mu.RLock()
+					levels := s.levelsCache[symbol]
+					s.mu.RUnlock()
+
+					var activeLevel *domain.Level
+					for _, l := range levels {
+						if l.ID == levelID {
+							activeLevel = l
+							break
+						}
+					}
+

Acquiring s.mu.RLock() inside the engine.UpdateState callback creates potential for deadlock or race conditions. The UpdateState callback already holds the engine's internal lock, and now acquires the service lock. If another goroutine holds the service lock and tries to call an engine method, a deadlock could occur. Consider passing the level configuration to finalizePosition as a parameter to avoid this nested locking pattern.
⬇️ Suggested change

-				if reason == "Stop Loss (Base)" || reason == "Level Cross" {
-					ls.ConsecutiveBaseCloses++
-					log.Printf("AUDIT: Base Close recorded for Level %s. Count: %d", levelID, ls.ConsecutiveBaseCloses)
-
-					// Check Cooldown
-					// We need to fetch the level to check config.
-					// Since we are in a callback, we can't easily fetch async?
-					// We can assume we have the level config in cache.
-					// But UpdateState is sync.
-					// Let's try to get level from cache.
-					// Warning: s.levelsCache requires lock. We are not holding lock here.
-					// But s.engine.UpdateState locks the engine state, not the service.
-					// So we can acquire service lock.
-
-					// However, calling s.levelsCache here might be expensive or complex if we don't have symbol.
-					// We have symbol.
-
-					// Let's find the level in cache.
-					s.mu.RLock()
-					levels := s.levelsCache[symbol]
-					s.mu.RUnlock()
-
-					var activeLevel *domain.Level
-					for _, l := range levels {
-						if l.ID == levelID {
-							activeLevel = l
-							break
-						}
-					}
-
+				var activeLevel *domain.Level
+				// Find the activeLevel before entering the callback to avoid nested locking
+				s.mu.RLock()
+				levels := s.levelsCache[symbol]
+				for _, l := range levels {
+					if l.ID == levelID {
+						activeLevel = l
+						break
+					}
+				}
+				s.mu.RUnlock()
+
+				if reason == "Stop Loss (Base)" || reason == "Level Cross" {
+					ls.ConsecutiveBaseCloses++
+					log.Printf("AUDIT: Base Close recorded for Level %s. Count: %d", levelID, ls.ConsecutiveBaseCloses)

In internal/web/templates/index.html:

>                  const divergence = speedRatio - depthRatio;
                 const divDeg = divergence * 90;
                 const divNeedle = document.getElementById('div-needle');
                 if (divNeedle) divNeedle.style.transform = `rotate(${divDeg}deg)`;
                 const divValue = document.getElementById('div-value');
                 if (divValue) divValue.textContent = divergence.toFixed(2);
 
+                // 5. OBI
+                const obiDeg = stats.obi * 90;
+                const obiNeedle = document.getElementById('obi-needle');
+                if (obiNeedle) obiNeedle.style.transform = `rotate(${obiDeg}deg)`;
+                const obiValue = document.getElementById('obi-value');
+                if (obiValue) obiValue.textContent = stats.obi.toFixed(2);
+
+                // 6. TSI
+                const maxTsi = 100;

[nitpick] The maxTsi value of 100 appears to be a magic number without explanation. Since TSI represents trades per second, this caps the gauge at 100 trades/second. Consider either documenting why this value was chosen, making it configurable, or dynamically calculating it based on observed values.
⬇️ Suggested change

-                const maxTsi = 100;
+                // Dynamically track the maximum observed TSI value to scale the gauge appropriately.
+                window.maxObservedTsi = Math.max(window.maxObservedTsi || 100, stats.tsi);
+                const maxTsi = window.maxObservedTsi;

In internal/web/templates/coin_detail.html:

> @@ -610,6 +682,90 @@ <h2>Quick Actions</h2>
                 const divValue = document.getElementById('div-value');
                 if (divValue) divValue.textContent = divergence.toFixed(2);
 
+                // Update OBI
+                // OBI is -1 to 1. Map to -90 to 90 degrees.
+                const obiDeg = stats.obi * 90;
+                const obiNeedle = document.getElementById('obi-needle');
+                if (obiNeedle) obiNeedle.style.transform = `rotate(${obiDeg}deg)`;
+                const obiValue = document.getElementById('obi-value');
+                if (obiValue) obiValue.textContent = stats.obi.toFixed(2);
+
+                // Update TSI
+                // Assume max 100 trades/sec for full scale?
+                const maxTsi = 100;

[nitpick] The maxTsi value of 100 is duplicated from index.html without explanation. This magic number should either be documented or extracted to a shared configuration to maintain consistency across pages.