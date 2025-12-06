@Copilot commented on this pull request.
Pull request overview

This pull request implements an "Auto Levels" feature that automatically manages trading levels based on market conditions. The system can now split failed levels into new ones, adjust take profit targets dynamically, and provide enhanced market visualization.

Key changes include:

    Auto-level splitting: When a level fails (reaches max base closes), it automatically creates two new levels at observed high/low prices
    Dynamic Take Profit modes: Added "liquidity" and "sentiment" TP modes alongside the existing "fixed" mode
    Backend conclusion score: Moved market sentiment calculation from frontend to backend for consistency
    UI enhancements: Added RSI indicator to charts, conclusion score visualization, and level management buttons

Reviewed changes

Copilot reviewed 26 out of 27 changed files in this pull request and generated 18 comments.
Show a summary per file
File 	Description
tests/split_limit_test.go 	New test for level limit enforcement and split behavior
tests/split_level_test.go 	Test for auto-level splitting on max base closes
tests/sentiment_tp_test.go 	Test for sentiment-adjusted take profit logic
tests/limit_levels_test.go 	Test for level creation limits per symbol
tests/e2e_test.go 	Added GetTickers mock method and OrderBook support
tests/auto_level_test.go 	Comprehensive test for auto-level creation workflow
internal/web/templates/index.html 	Major UI updates: RSI chart, conclusion chart, duplicate form elements (bug), badge styling
internal/web/templates/funding_coins.html 	New page for high funding rate coins with sorting/filtering
internal/web/templates/coins.html 	Enhanced coin listing with market data and improved sorting
internal/web/templates/coin_detail.html 	Added RSI indicator and conclusion chart to detail view
internal/web/server.go 	Added two new endpoints for level management
internal/web/handlers.go 	Implements increment-closes and auto-create handlers, enhanced level views
internal/usecase/sublevel_engine.go 	Added RangeHigh/RangeLow tracking for split logic
internal/usecase/market_service_test.go 	Added GetTickers mock implementation
internal/usecase/market_service.go 	Implemented ConclusionScore calculation backend logic
internal/usecase/level_service_test.go 	Enhanced mocks with new repository methods
internal/usecase/level_service.go 	Core auto-level logic: SplitLevel, AutoCreateNextLevel, liquidity/sentiment TP
internal/infrastructure/storage/sqlite.go 	Added TakeProfitMode, IsAuto, AutoModeEnabled fields to schema
internal/infrastructure/exchange/bybit.go 	Implemented GetTickers API, increased order book limit to 200
internal/domain/level.go 	Added TakeProfitMode, IsAuto, AutoModeEnabled fields
internal/domain/interfaces.go 	Added GetTickers, GetLevelsBySymbol, CountActiveLevels methods
internal/domain/instrument.go 	Added Ticker type for market data
go.mod / go.sum 	Updated dependencies (testify added)
docs/ 	Added technical documentation and playbooks

💡 Add Copilot custom instructions for smarter, more guided reviews. Learn how to get started.

In internal/web/templates/coin_detail.html:

> +                    // Optimization: We could just calc the last point if we had state, but full recalc is safer/easier for now
+                    const rsiData = calculateRSI(candles);
+                    if (rsiData.length > 0) {
+                        // Update RSI series (Lightweight charts handles updates efficiently)
+                        // But setData might be heavy? update() is better for single point.
+                        // Let's just use setData for simplicity or update the last point.
+                        // For RSI, the previous values might change slightly as the moving average settles?
+                        // No, standard RSI only changes the current candle's RSI.
+                        // So we can just update the last point.

[nitpick] Verbose commented-out exploration code. Lines 729-742 contain extensive commented code with multiple approaches and thought process. While this might be helpful during development, it should be cleaned up before merging. Either remove it entirely or condense to a single line explaining why setData was chosen over update.
⬇️ Suggested change

-                    // Optimization: We could just calc the last point if we had state, but full recalc is safer/easier for now
-                    const rsiData = calculateRSI(candles);
-                    if (rsiData.length > 0) {
-                        // Update RSI series (Lightweight charts handles updates efficiently)
-                        // But setData might be heavy? update() is better for single point.
-                        // Let's just use setData for simplicity or update the last point.
-                        // For RSI, the previous values might change slightly as the moving average settles?
-                        // No, standard RSI only changes the current candle's RSI.
-                        // So we can just update the last point.
+                    // Use update() for RSI since only the latest value changes; setData would be heavier.
+                    const rsiData = calculateRSI(candles);
+                    if (rsiData.length > 0) {

In internal/web/templates/index.html:

> +                    <input type="checkbox" name="auto_mode_enabled" id="auto_mode_enabled" checked>
+                    <label for="auto_mode_enabled" style="margin: 0;">Auto-Level Mode (Recreate on Failure)</label>

Duplicate auto_mode_enabled checkbox elements with the same ID. This will cause HTML validation errors and JavaScript issues since IDs must be unique. The second checkbox at line 460 appears to be a duplicate of the one at line 448.
⬇️ Suggested change

-                    <input type="checkbox" name="auto_mode_enabled" id="auto_mode_enabled" checked>
-                    <label for="auto_mode_enabled" style="margin: 0;">Auto-Level Mode (Recreate on Failure)</label>
+                    <input type="checkbox" name="auto_mode_enabled" id="auto_level_mode_enabled" checked>
+                    <label for="auto_level_mode_enabled" style="margin: 0;">Auto-Level Mode (Recreate on Failure)</label>

In internal/web/templates/index.html:

> +        function calculateRSI(candles, period = 14) {
+            if (candles.length < period + 1) return [];
+            const rsiData = [];
+            let avgGain = 0;
+            let avgLoss = 0;
+            for (let i = 1; i <= period; i++) {
+                const change = candles[i].close - candles[i - 1].close;
+                if (change > 0) avgGain += change;
+                else avgLoss += Math.abs(change);
+            }
+            avgGain /= period;
+            avgLoss /= period;
+            let rs = avgGain / avgLoss;
+            let rsi = 100 - (100 / (1 + rs));
+            rsiData.push({ time: candles[period].time, value: rsi });
+            for (let i = period + 1; i < candles.length; i++) {
+                const change = candles[i].close - candles[i - 1].close;
+                let gain = change > 0 ? change : 0;
+                let loss = change < 0 ? Math.abs(change) : 0;
+                avgGain = (avgGain * (period - 1) + gain) / period;
+                avgLoss = (avgLoss * (period - 1) + loss) / period;
+                rs = avgGain / avgLoss;
+                rsi = 100 - (100 / (1 + rs));

Potential division by zero in RSI calculation. If avgLoss is zero on line 742 or 751, the division avgGain / avgLoss will cause a runtime panic. This can happen when all price changes in the period are positive. Consider adding a check: if avgLoss == 0 { rs = 0 } or handling this edge case appropriately.

In internal/usecase/market_service.go:

> +	var gliScore float64
+	if gli > 1 {
+		gliScore = -(gli - 1) // Bearish
+		if gliScore < -1 {
+			gliScore = -1
+		}
+	} else {
+		gliScore = 1 - gli // Bullish? Wait.
+		// If GLI = 0.5 (Sell=1, Buy=2). 1 - 0.5 = 0.5 (Bullish). Correct.
+		// If GLI = 0 (No Sell). 1 - 0 = 1 (Max Bullish). Correct.
+	}
+
+	// CVD: Cumulative Volume Delta.
+	// Normalize CVD?
+	// Frontend: const cvdRatio = stats.cvd / maxCvd;
+	// We need a MaxCVD.
+	maxCvd := 10000.0 // Arbitrary baseline
+	cvdScore := cvd / maxCvd
+	if cvdScore > 1 {
+		cvdScore = 1
+	} else if cvdScore < -1 {
+		cvdScore = -1
+	}
+
+	// TSI Direction?
+	// If we want TSI to be directional, we need to know if it's buy-heavy or sell-heavy.
+	// But TSI is just count.
+	// Maybe the formula intends TSI to just boost the signal?
+	// Or maybe I should check the frontend implementation I'm replacing.
+	// The user said "Move Conclusion Score calculation from Frontend".
+	// I'll assume the frontend logic was:
+	// (OBI + (TSI_Score * 2) + (GLI_Score * 2) + (CVD_Score * 2)) / 7
+	// Wait, TSI in frontend was likely just 0..1.
+	// If I add 0..1 to a -1..1 score, it shifts it to bullish.
+	// That seems wrong if TSI is just activity.
+	// Let's assume TSI should be weighted by the dominant side?
+	// Or maybe the user meant "Trade Velocity" or something else.
+	// Let's stick to a safe implementation:
+	// Score = (OBI + GLI_Score + CVD_Score) / 3
+	// And maybe weigh them.
+	// Let's use: (OBI + GLI_Score + CVD_Score) / 3 for now.
+	// It returns -1 to 1.
+
+	conclusionScore := (obi + gliScore + cvdScore) / 3.0

[nitpick] Potential division by zero in Conclusion Score calculation. At line 372, if both gli and obi are zero, and cvdScore is also zero, the division might not cause an error, but if the normalization logic results in division by zero for GLI calculation at line 330 when gli > 1 and becomes exactly 1, or at line 346 when maxCvd is 0, this could cause issues. More critically, the GLI score calculation at line 336 when gli = 0 will result in gliScore = 1 - 0 = 1, which is correct, but the comment suggests this is for "No Sell" scenario, which should be verified.

In internal/usecase/level_service.go:

> +// It limits auto-levels to 2 per symbol and handles overlap by prioritizing volume.
+// It deletes ALL previous auto-levels for the symbol.

Misleading comment - the function comment claims it "limits auto-levels to 2 per symbol" but the implementation deletes ALL auto-levels and can create 1 or 2 new ones. The limit is enforced through the selection logic (bid and/or ask), not a hard cap. Consider updating the comment to accurately reflect the behavior: "Replaces all existing auto-levels with up to 2 new levels based on best bid/ask liquidity."
⬇️ Suggested change

-// It limits auto-levels to 2 per symbol and handles overlap by prioritizing volume.
-// It deletes ALL previous auto-levels for the symbol.
+// Replaces all existing auto-levels for the symbol with up to 2 new levels based on best bid/ask liquidity.
+// Handles overlap by prioritizing volume.

In internal/web/templates/index.html:

> @@ -907,6 +1068,8 @@ <h2>Recent Trades</h2>
         }
 
         async function updateChart() {
+            console.log("updateChart called");

Debug console.log statement left in production code. Line 1071 contains console.log("updateChart called"); which should be removed before merging to production. Debug statements can clutter the browser console and may expose internal application behavior.
⬇️ Suggested change

-            console.log("updateChart called");

In internal/usecase/level_service.go:

>  		if l.DisableSpeedClose {
 			speedCloseDisabled = true
-			break
 		}

The break statement was removed from the loop checking if speed close is disabled. This means the loop will continue checking all levels even after finding one with DisableSpeedClose=true, which is unnecessary and slightly less efficient. While not a critical bug, the original break was intentional for short-circuit behavior.

In internal/web/templates/coin_detail.html:

> +        function calculateRSI(candles, period = 14) {
+            if (candles.length < period + 1) return [];
+
+            const rsiData = [];
+            let avgGain = 0;
+            let avgLoss = 0;
+
+            // First RSI value
+            for (let i = 1; i <= period; i++) {
+                const change = candles[i].close - candles[i - 1].close;
+                if (change > 0) avgGain += change;
+                else avgLoss += Math.abs(change);
+            }
+
+            avgGain /= period;
+            avgLoss /= period;
+
+            let rs = avgGain / avgLoss;
+            let rsi = 100 - (100 / (1 + rs));
+
+            rsiData.push({ time: candles[period].time, value: rsi });
+
+            // Subsequent values
+            for (let i = period + 1; i < candles.length; i++) {
+                const change = candles[i].close - candles[i - 1].close;
+                let gain = change > 0 ? change : 0;
+                let loss = change < 0 ? Math.abs(change) : 0;
+
+                avgGain = (avgGain * (period - 1) + gain) / period;
+                avgLoss = (avgLoss * (period - 1) + loss) / period;
+
+                rs = avgGain / avgLoss;
+                rsi = 100 - (100 / (1 + rs));

Same potential division by zero issue in the RSI calculation for the coin detail page. If avgLoss is zero (lines 471, 485), the division will cause a runtime error. Consider adding a guard condition to handle the case when all price movements are in one direction.

In internal/usecase/level_service.go:

> +		time.Sleep(1 * time.Nanosecond)
+		newLevel.ID = fmt.Sprintf("%d", time.Now().UnixNano())

Magic number for time offset without clear reasoning. Line 951 uses time.Sleep(1 * time.Nanosecond) to ensure unique IDs, but this is unreliable for generating unique timestamps. If two calls happen within the same nanosecond (unlikely but possible), IDs could still collide. Consider using a more robust ID generation strategy like UUID or an atomic counter combined with timestamp.

In internal/web/handlers.go:

> +		// Check if it's a limit error
+		if err.Error() == fmt.Sprintf("active levels limit reached for %s (max 2)", symbol) {
+			http.Error(w, err.Error(), http.StatusConflict) // 409 Conflict

[nitpick] Inconsistent error message format. Line 206 constructs an error message using fmt.Sprintf to check against, but this is fragile. If the error message format in the service layer changes, this check will break. Consider returning a typed error or error code from CreateLevel that can be checked more reliably.

In internal/web/templates/coins.html:

> @@ -115,7 +119,14 @@ <h1>Bybit Futures - All Coins</h1>
                         <td>{{.Symbol}}</td>
                         <td>{{.BaseCoin}}</td>
                         <td>{{.QuoteCoin}}</td>
-                        <td class="{{if eq .Status "Trading"}}status-trading{{else}}status-other{{end}}">{{.Status}}
+                        <td>{{printf "%.4f" .LastPrice}}</td>
+                        <td
+                            style="color: {{if gt .Price24hPcnt 0.0}}green{{else if lt .Price24hPcnt 0.0}}red{{else}}black{{end}}">
+                            {{printf "%.2f%%" .Price24hPcnt}}
+                        </td>
+                        <td>{{printf "%.0f" .Volume24h}}</td>
+                        <td>{{printf "%.0f" .OpenInterestValue}}</td>
+                        <td class="{{if eq .Status " Trading"}}status-trading{{else}}status-other{{end}}">{{.Status}}

Extra space in string comparison causing the condition to always be false. Line 129 has eq .Status " Trading" with a leading space, which will never match the actual status "Trading". Should be eq .Status "Trading".
⬇️ Suggested change

-                        <td class="{{if eq .Status " Trading"}}status-trading{{else}}status-other{{end}}">{{.Status}}
+                        <td class="{{if eq .Status "Trading"}}status-trading{{else}}status-other{{end}}">{{.Status}}

In internal/usecase/level_service.go:

> +
+					// Clamp multiplier to avoid negative or too small TP?
+					// If score is extreme, e.g. -1. Multiplier = 0.5. TP becomes half.
+					// Seems safe.
+
+					adjustedPct := baseTP * multiplier
+					if adjustedPct < 0.001 {
+						adjustedPct = 0.001 // Minimum 0.1% TP
+					}
+
+					if pos.Side == domain.SideLong {
+						tpPrice = pos.EntryPrice * (1 + adjustedPct)
+					} else {
+						tpPrice = pos.EntryPrice * (1 - adjustedPct)
+					}
+					// log.Printf("DEBUG: Sentiment TP for %s. Score: %f, Base: %f, Adj: %f, Price: %f", symbol, score, baseTP, adjustedPct, tpPrice)

[nitpick] Commented debug log line should be removed. Line 357 contains a commented log.Printf statement that should either be removed entirely or converted to proper debug logging if needed for troubleshooting.
⬇️ Suggested change

-					// log.Printf("DEBUG: Sentiment TP for %s. Score: %f, Base: %f, Adj: %f, Price: %f", symbol, score, baseTP, adjustedPct, tpPrice)

In internal/usecase/level_service.go:

> @@ -139,6 +146,19 @@ func (s *LevelService) UpdateCache(ctx context.Context) error {
 	return nil
 }
 
+// CreateLevel creates a new level
+func (s *LevelService) CreateLevel(ctx context.Context, level *domain.Level) error {
+	// Limit check removed for manual levels as per user request.

Removed limit enforcement comment contradicts implementation. The comment at line 151 says "Limit check removed for manual levels as per user request", but there's no actual limit check code visible in this function - it just saves the level directly. This comment is misleading and should either be removed or clarified to explain what was removed and when.
⬇️ Suggested change

-	// Limit check removed for manual levels as per user request.

In internal/web/templates/index.html:

>                  <div class="flex-row">
-                    <input type="number" name="cool_down_ms" placeholder="Cooldown (ms)" value="5000" required
-                        class="flex-1">
-                    <input type="number" step="0.1" name="take_profit_pct" placeholder="TP %" value="2.0" required
-                        class="flex-1">
+                    <div class="flex-1">
+                        <label style="margin-bottom: 2px;">TP %</label>
+                        <input type="number" step="0.1" name="take_profit_pct" placeholder="TP %" value="2.0" required>
+                    </div>
+                    <div class="flex-1">
+                        <label style="margin-bottom: 2px;">TP Mode</label>
+                        <select name="take_profit_mode">
+                            <option value="fixed">Fixed %</option>
+                            <option value="liquidity">Liquidity</option>
+                            <option value="sentiment">Sentiment</option>
+                        </select> </select>

Duplicate closing </select> tag. Line 418 has </select> </select> which will result in invalid HTML.
⬇️ Suggested change

-                        </select> </select>
+                        </select>

In internal/web/templates/funding_coins.html:

> +                        <th onclick="sortTable(5)">24h Change % ▼</th>
+                        <th onclick="sortTable(6)">24h Volume (USD) ▼</th>
+                        <th onclick="sortTable(7)">Open Interest (USD) ▼</th>
+                        <th>Action</th>
+                    </tr>
+                </thead>
+                <tbody>
+                    {{range .Instruments}}
+                    <tr>
+                        <td>{{.Symbol}}</td>
+                        <td>{{.BaseCoin}}</td>
+                        <td>{{.QuoteCoin}}</td>
+                        <td>{{printf "%.4f" .LastPrice}}</td>
+                        <td
+                            style="color: {{if gt .FundingRate 0.0}}green{{else if lt .FundingRate 0.0}}red{{else}}black{{end}}; font-weight: bold;">
+

Missing closing parenthesis in the Funding Rate table cell. The template expression for funding rate formatting is incomplete at line 125, which will cause a template rendering error.
⬇️ Suggested change

-
+                            {{printf "%.2f%%" (mul .FundingRate 100)}}

In tests/split_level_test.go:

> +			// Use a pointer to the loop variable copy? No, loop var `l` is a copy (struct) or pointer?
+			// ListLevels returns []*Level. So l is *Level.
+			// We need to be careful with loop variable capture if taking address, but l is already a pointer.
+			// Wait, ListLevels returns []*Level. So `l` is `*Level`.
+			// So `l` is safe to assign.

[nitpick] Long commented code block should be removed. Lines 114-118 contain a lengthy comment explaining loop variable semantics that's not directly relevant to the test logic. This adds noise and reduces readability. Consider removing or condensing to a brief note if the concern is about pointer safety.
⬇️ Suggested change

-			// Use a pointer to the loop variable copy? No, loop var `l` is a copy (struct) or pointer?
-			// ListLevels returns []*Level. So l is *Level.
-			// We need to be careful with loop variable capture if taking address, but l is already a pointer.
-			// Wait, ListLevels returns []*Level. So `l` is `*Level`.
-			// So `l` is safe to assign.

In internal/usecase/level_service.go:

> +		return 0, err
+	}
+
+	var bestCluster *LiquidityCluster
+	// We want to find a cluster that is at least some distance away to cover fees/profit.
+	// Let's say min 0.5% profit.
+	const minProfitPct = 0.005
+
+	for _, c := range clusters {
+		if side == domain.SideLong {
+			// Look for Asks above entry
+			if c.Type == "ask" && c.Price > entryPrice*(1+minProfitPct) {
+				if bestCluster == nil || c.Volume > bestCluster.Volume {
+					// We want the biggest wall
+					// But maybe we also want the closest big wall?
+					// CreateLevel creates a new level

Incorrect comment explaining the CreateLevel function context. Line 988 has a stray comment "// CreateLevel creates a new level" in the middle of the CalculateLiquidityTP function body, which seems like a copy-paste error or misplaced documentation.
⬇️ Suggested change

-					// CreateLevel creates a new level

—