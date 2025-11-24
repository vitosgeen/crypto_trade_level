Pull request overview

This pull request implements an "Order Book" feature that adds market sentiment analysis and liquidity visualization to the trading bot. The implementation introduces a new MarketService that tracks real-time trade volume and order book depth to provide sentiment-based entry filtering and exit triggers.

Key Changes:

    Added MarketService for liquidity cluster analysis and trade sentiment tracking
    Implemented sentiment-based trade filtering (blocks entries against strong market pressure)
    Added dynamic exit triggers based on sentiment reversals
    Introduced stop loss mode selection (exchange-based vs app-based)

Reviewed changes

Copilot reviewed 16 out of 17 changed files in this pull request and generated 12 comments.
Show a summary per file
File 	Description
internal/usecase/market_service.go 	New service for order book analysis, liquidity clustering, and trade sentiment calculation
internal/usecase/market_service_test.go 	Comprehensive test coverage for MarketService with time-mocked tests
internal/usecase/level_service.go 	Integrated sentiment logic for entry filtering and exit triggers with dynamic thresholds
internal/usecase/level_service_test.go 	Added tests for stop loss modes and sentiment-based trading logic
internal/domain/level.go 	Added StopLossMode field to Level entity
internal/domain/interfaces.go 	Extended Exchange interface with GetOrderBook and OnTradeUpdate methods
internal/infrastructure/exchange/bybit.go 	Implemented order book fetching and public trade WebSocket subscriptions
internal/infrastructure/storage/sqlite.go 	Added stop_loss_mode column with migration support
internal/web/server.go 	Added MarketService injection and new API routes for liquidity and market stats
internal/web/handlers.go 	Implemented handlers for liquidity clusters and market statistics endpoints
internal/web/templates/index.html 	Added three gauges for trade volume, order book depth, and sentiment divergence visualization
tests/scenarios_test.go 	Updated test helper to include MarketService and UpdateCache call
tests/e2e_test.go 	Added mock implementations for new Exchange methods and MarketService integration
cmd/bot/main.go 	Integrated MarketService into main application initialization
.gitignore 	Fixed bot binary ignore pattern to only match root directory

In internal/usecase/market_service.go:

> +		avgAsk = sumAsk / float64(len(history))
+	}
+
+	return &MarketStats{
+		SpeedBuy:  speedBuy,
+		SpeedSell: speedSell,
+		DepthBid:  avgBid,
+		DepthAsk:  avgAsk,
+	}, nil
+}
+
+func (s *MarketService) updateOrderBook(ctx context.Context, symbol string) {
+	// Check if latest snapshot is fresh (< 5s)
+	if history, ok := s.depthHistory[symbol]; ok && len(history) > 0 {
+		last := history[len(history)-1]
+		if time.Since(last.Time) < 5*time.Second {

The updateOrderBook function uses time.Since(last.Time) instead of s.timeNow() for consistency. This breaks the testability of the function since tests inject a custom timeNow function but this line bypasses it. Change to: if s.timeNow().Sub(last.Time) < 5*time.Second
⬇️ Suggested change

-		if time.Since(last.Time) < 5*time.Second {
+		if s.timeNow().Sub(last.Time) < 5*time.Second {

In internal/usecase/market_service.go:

> +		DepthAsk:  avgAsk,
+	}, nil
+}
+
+func (s *MarketService) updateOrderBook(ctx context.Context, symbol string) {
+	// Check if latest snapshot is fresh (< 5s)
+	if history, ok := s.depthHistory[symbol]; ok && len(history) > 0 {
+		last := history[len(history)-1]
+		if time.Since(last.Time) < 5*time.Second {
+			return // Fresh enough
+		}
+	}
+
+	// Fetch Linear (Futures) Order Book
+	// We use a separate context with timeout to avoid blocking too long
+	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)

The context is shadowed with a new timeout context, but if the original context is already cancelled or has a deadline, this should respect it. Consider using context.WithTimeoutCause or checking the parent context's deadline first to avoid extending an already-cancelled context's lifetime.
⬇️ Suggested change

-	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
+	var cancel context.CancelFunc
+	timeout := 2 * time.Second
+	if deadline, ok := ctx.Deadline(); ok {
+		// Parent context has a deadline, use the earlier of the two
+		earliest := time.Now().Add(timeout)
+		if deadline.Before(earliest) {
+			ctx, cancel = context.WithDeadline(ctx, deadline)
+		} else {
+			ctx, cancel = context.WithDeadline(ctx, earliest)
+		}
+	} else {
+		ctx, cancel = context.WithTimeout(ctx, timeout)
+	}

In internal/usecase/market_service.go:

> +	}
+	s.depthHistory[symbol] = validHistory
+}
+
+type LiquidityCluster struct {
+	Price  float64 `json:"price"`
+	Volume float64 `json:"volume"`
+	Type   string  `json:"type"`   // "bid" or "ask"
+	Source string  `json:"source"` // "spot", "linear", "combined"
+}
+
+func (s *MarketService) GetLiquidityClusters(ctx context.Context, symbol string) ([]LiquidityCluster, error) {
+	// Check Cache
+	s.mu.Lock()
+	if cached, ok := s.cache[symbol]; ok {
+		if time.Now().Before(cached.Expiry) {

The cache expiry check uses time.Now() directly instead of s.timeNow() for consistency with the testable time injection pattern used elsewhere in the service. This breaks testability. Change to: if s.timeNow().Before(cached.Expiry)

In internal/usecase/market_service.go:

> +		// Simple look-around (can be optimized but N=200 is small enough for brute-ish force)
+		// Look backwards
+		for k := i; k >= 0; k-- {
+			if rawPoints[k].Price < minP {
+				break
+			}
+			sumVol += rawPoints[k].Volume
+		}
+		// Look forwards
+		for k := i + 1; k < len(rawPoints); k++ {
+			if rawPoints[k].Price > maxP {
+				break
+			}
+			sumVol += rawPoints[k].Volume
+		}

The nested loop structure (lines 318-330) for calculating density has O(n²) worst-case complexity. While the comment mentions "N=200 is small enough", the actual orderbook can have up to 500 entries per side for futures (line 592 in bybit.go). With combined spot+futures, this could result in performance issues. Consider implementing a proper sliding window with two-pointer technique to achieve O(n) complexity.

In internal/web/templates/index.html:

> +                const parts = selector.value.split('-');
+                if (parts.length > 0) symbol = parts[0];

The symbol extraction logic (line 711) assumes the selector value format is "symbol-price" and uses a simple split on '-'. This will fail for symbols that contain hyphens (e.g., "BTC-PERP" would extract just "BTC"). Consider using a more robust parsing method, such as splitting from the last hyphen or using a different delimiter that's guaranteed not to be in symbol names.
⬇️ Suggested change

-                const parts = selector.value.split('-');
-                if (parts.length > 0) symbol = parts[0];
+                const lastHyphen = selector.value.lastIndexOf('-');
+                if (lastHyphen > 0) symbol = selector.value.substring(0, lastHyphen);

In internal/usecase/level_service.go:

> @@ -168,14 +169,97 @@ func (s *LevelService) ProcessTick(ctx context.Context, exchangeName, symbol str
 		return nil // No tiers, can't trade
 	}
 
+	// --- SENTIMENT LOGIC ---
+	sentiment, err := s.market.GetTradeSentiment(ctx, symbol)
+	if err != nil {
+		log.Printf("Error getting sentiment for %s: %v", symbol, err)
+		sentiment = 0 // Default to neutral
+	}
+
+	// Dynamic Threshold Logic
+	// Default Loose Threshold
+	sentimentThreshold := 0.6
+
+	// Check if we have an open position
+	pos, err := s.exchange.GetPosition(ctx, symbol)
+	if err == nil && pos.Size > 0 {

The error from GetPosition is silently ignored (line 184: if err == nil). If an error occurs, the position check is skipped and trading continues without the dynamic threshold adjustment or exit trigger logic. Consider logging the error and handling it appropriately, especially since position information is critical for managing open trades.
⬇️ Suggested change

-	if err == nil && pos.Size > 0 {
+	if err != nil {
+		log.Printf("Error getting position for %s: %v", symbol, err)
+		return nil
+	}
+	if pos.Size > 0 {

In internal/usecase/level_service.go:

> @@ -189,6 +273,19 @@ func (s *LevelService) processLevel(ctx context.Context, level *domain.Level, ti
 	action, size := s.engine.Evaluate(level, boundaries, prevPrice, currPrice, side)
 
 	if action != ActionNone {
+		// --- ENTRY FILTER ---
+		const sentimentThreshold = 0.6

The sentimentThreshold constant is redeclared in processLevel with the same value (0.6) that was already set as a variable in the calling ProcessTick function (line 181). This creates confusion and potential for inconsistency. The threshold should either be passed as a parameter or made a package-level constant to avoid duplication.

In internal/web/handlers.go:

> @@ -253,3 +258,37 @@ func (s *Server) handleGetCandles(w http.ResponseWriter, r *http.Request) {
 	w.Header().Set("Content-Type", "application/json")
 	json.NewEncoder(w).Encode(candles)
 }
+
+func (s *Server) handleLiquidity(w http.ResponseWriter, r *http.Request) {
+	symbol := r.URL.Query().Get("symbol")
+	if symbol == "" {
+		symbol = "BTCUSDT"
+	}
+
+	clusters, err := s.marketService.GetLiquidityClusters(r.Context(), symbol)
+	if err != nil {
+		s.logger.Error("Failed to get liquidity clusters", zap.Error(err))
+		http.Error(w, err.Error(), 500)

HTTP status codes should use named constants from the http package (e.g., http.StatusInternalServerError) instead of magic numbers like 500 for better readability and maintainability.
⬇️ Suggested change

-		http.Error(w, err.Error(), 500)
+		http.Error(w, err.Error(), http.StatusInternalServerError)

In internal/web/handlers.go:

> +	}
+
+	w.Header().Set("Content-Type", "application/json")
+	json.NewEncoder(w).Encode(clusters)
+}
+
+func (s *Server) handleMarketStats(w http.ResponseWriter, r *http.Request) {
+	symbol := r.URL.Query().Get("symbol")
+	if symbol == "" {
+		symbol = "BTCUSDT"
+	}
+
+	stats, err := s.marketService.GetMarketStats(r.Context(), symbol)
+	if err != nil {
+		s.logger.Error("Failed to get market stats", zap.Error(err))
+		http.Error(w, err.Error(), 500)

HTTP status codes should use named constants from the http package (e.g., http.StatusInternalServerError) instead of magic numbers like 500 for better readability and maintainability.

In internal/infrastructure/exchange/bybit.go:

> +				// We need a new callback type for trades or reuse existing?
+				// For now, let's just log or add a specific trade callback handler later.
+				// Actually, MarketService needs this.
+				// Let's add OnTradeUpdate to BybitAdapter.

The commented-out code on lines 507-510 should be removed as it's now implemented and these comments are outdated development notes that add no value and clutter the code.
⬇️ Suggested change

-				// We need a new callback type for trades or reuse existing?
-				// For now, let's just log or add a specific trade callback handler later.
-				// Actually, MarketService needs this.
-				// Let's add OnTradeUpdate to BybitAdapter.

In internal/usecase/market_service.go:

> +		totalAsk += e.Size
+	}
+
+	// Process Bids
+	clusters = append(clusters, s.processSide(linearOB.Bids, spotOB.Bids, "bid")...)
+
+	// Process Asks
+	clusters = append(clusters, s.processSide(linearOB.Asks, spotOB.Asks, "ask")...)
+
+	// Update Cache
+	s.mu.Lock()
+	s.cache[symbol] = CachedLiquidity{
+		Data:     clusters,
+		TotalBid: totalBid,
+		TotalAsk: totalAsk,
+		Expiry:   time.Now().Add(10 * time.Second),

The cache expiry is set using time.Now() directly instead of s.timeNow() for consistency with the testable time injection pattern used elsewhere in the service. This breaks testability. Change to: Expiry: s.timeNow().Add(10 * time.Second)
⬇️ Suggested change

-		Expiry:   time.Now().Add(10 * time.Second),
+		Expiry:   s.timeNow().Add(10 * time.Second),

In internal/web/templates/index.html:

> +                const id = `${symbol}-${levelPrice}`;
+                activeIds.push(id);
+                availableLevels.push({ id, text: `${symbol} @ ${levelPrice}` });
+
+                if (!currentOptions.includes(id)) {
+                    const option = document.createElement('option');
+                    option.value = id;

The option value format has changed between the server-side rendering (line 261: {{.ID}}) and the client-side JavaScript population (line 420: `${symbol}-${levelPrice}`). This creates inconsistency where server-rendered options use level IDs while dynamically-added options use "symbol-price" format. This can cause issues when comparing or selecting options. Ensure both use the same format.