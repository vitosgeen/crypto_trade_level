Pull request overview

This PR adds a "Speed Bot" feature that enables browsing and automated trading of Bybit futures based on real-time market sentiment. The bot analyzes trade volume, price changes, and order book depth to make entry and exit decisions.

Key changes:

    New Speed Bot service with automated trading based on market gauge confluence (volume, price change, divergence)
    Coins listing page and detailed coin analysis page with real-time market metrics
    Added DisableSpeedClose flag to levels to optionally bypass sentiment-based exits
    Safety monitor that closes positions if price crosses the base level in the wrong direction

Reviewed changes

Copilot reviewed 19 out of 19 changed files in this pull request and generated 10 comments.
Show a summary per file
File 	Description
internal/usecase/speed_bot_service.go 	New service managing automated speed bots per symbol with position and signal tracking
internal/usecase/market_service.go 	Added 60-second price change tracking and trade refresh logic
internal/usecase/level_service.go 	Added CheckSafety method and DisableSpeedClose conditional logic
internal/web/templates/coins.html 	New page listing all Bybit futures with search and sort functionality
internal/web/templates/coin_detail.html 	New detailed coin page with charts, gauges, and bot controls
internal/web/handlers.go 	New handlers for Speed Bot pages and API endpoints
internal/web/server.go 	Added Speed Bot service dependency and route registration
internal/domain/level.go 	Added DisableSpeedClose field to Level struct
internal/infrastructure/storage/sqlite.go 	Added disable_speed_close column to levels table
internal/infrastructure/exchange/bybit.go 	Added GetInstruments method and WebSocket ping/pong support
cmd/bot/main.go 	Initialize Speed Bot service and safety monitor goroutine

💡 Add Copilot custom instructions for smarter, more guided reviews. Learn how to get started.

In internal/usecase/speed_bot_service.go:

> +	b.mu.Lock()
+	defer b.mu.Unlock()
+

The mutex is held while making blocking calls to GetMarketStats and GetPosition. These methods involve external API calls that can take significant time, blocking other operations. Move the mutex lock to only protect shared state reads/writes, not the entire function body.
⬇️ Suggested change

-	b.mu.Lock()
-	defer b.mu.Unlock()
-

In internal/usecase/speed_bot_service.go:

> +	if b.running {
+		b.running = false
+		close(b.stopChan)

Closing a channel that is already closed will panic. If stop() is called multiple times concurrently, this could cause a panic. Consider checking if stopChan is already closed or using sync.Once to ensure close is only called once.

In internal/web/handlers.go:

> +		s.logger.Error("Template error", zap.Error(err))
+		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
+	}
+}
+
+// Speed Bot API Handlers
+
+func (s *Server) handleStartSpeedBot(w http.ResponseWriter, r *http.Request) {
+	var config usecase.SpeedBotConfig
+	if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
+		http.Error(w, err.Error(), http.StatusBadRequest)
+		return
+	}
+
+	// Convert cooldown from milliseconds to duration
+	config.Cooldown = time.Duration(config.Cooldown) * time.Millisecond

The config.Cooldown field is already a time.Duration, so this type conversion treats the nanosecond value as milliseconds. For example, if the JSON contains cooldown: 1000 (meaning 1000ms), it gets decoded as 1000 nanoseconds, then multiplied by 1ms, resulting in 1 microsecond instead of 1 second. The JSON tag should parse it as an integer, or use a separate integer field for the JSON input.
⬇️ Suggested change

-	config.Cooldown = time.Duration(config.Cooldown) * time.Millisecond
+	config.Cooldown = time.Duration(config.CooldownMs) * time.Millisecond

In internal/usecase/market_service.go:

> +	needsRefresh := len(s.trades[symbol]) == 0
+	if !needsRefresh {
+		// Check if we have any trades in the last 60 seconds
+		cutoff := s.timeNow().Add(-60 * time.Second)
+		hasRecentTrades := false
+		for _, t := range s.trades[symbol] {
+			if t.Time.After(cutoff) {
+				hasRecentTrades = true
+				break
+			}
+		}
+		needsRefresh = !hasRecentTrades
+	}

After unlocking at line 132 and re-locking at line 136, another goroutine could have already refreshed the trades. The code should check again after re-acquiring the lock to avoid redundant API calls and potential data races where fresh trades get overwritten.

In internal/usecase/speed_bot_service.go:

> +func (b *SpeedBot) getStatus(ctx context.Context) (*BotStatus, error) {
+	b.mu.Lock()
+	defer b.mu.Unlock()
+
+	status := &BotStatus{
+		Running:    b.running,
+		InCooldown: time.Since(b.lastCloseTime) < b.config.Cooldown,
+	}
+
+	// Get current position
+	position, err := b.exchange.GetPosition(ctx, b.config.Symbol)

The mutex is held while making blocking exchange API calls (GetPosition, GetMarketStats). This can cause deadlock if evaluate() is waiting for the lock while getStatus() holds it during a slow API call. Release the mutex before making external calls and re-acquire if needed to update shared state.

In internal/usecase/speed_bot_service.go:

> +	s.mu.Unlock()
+
+	if !exists {
+		return &BotStatus{Running: false}, nil
+	}
+
+	return bot.getStatus(ctx)

After unlocking, the bot reference is used without ensuring it's still valid. Another goroutine could call StopBot and delete the bot between unlock and bot.getStatus() call at line 111. Keep the lock held while calling bot.getStatus() or use a different synchronization mechanism.
⬇️ Suggested change

-	s.mu.Unlock()
-
-	if !exists {
-		return &BotStatus{Running: false}, nil
-	}
-
-	return bot.getStatus(ctx)
+	if !exists {
+		s.mu.Unlock()
+		return &BotStatus{Running: false}, nil
+	}
+	status, err := bot.getStatus(ctx)
+	s.mu.Unlock()
+	return status, err

In internal/web/handlers.go:

> +
+	if err := templates.ExecuteTemplate(w, "coin_detail.html", data); err != nil {
+		s.logger.Error("Template error", zap.Error(err))
+		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
+	}
+}
+
+// Speed Bot API Handlers
+
+func (s *Server) handleStartSpeedBot(w http.ResponseWriter, r *http.Request) {
+	var config usecase.SpeedBotConfig
+	if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
+		http.Error(w, err.Error(), http.StatusBadRequest)
+		return
+	}
+

Missing input validation for required fields before passing to StartBot. Fields like Symbol, PositionSize, Leverage, and MarginType should be validated at the API layer to provide clear error messages to users before attempting to start the bot.
⬇️ Suggested change

-
+
+	// Validate required fields
+	if config.Symbol == "" {
+		http.Error(w, "Symbol is required", http.StatusBadRequest)
+		return
+	}
+	if config.PositionSize <= 0 {
+		http.Error(w, "PositionSize must be greater than 0", http.StatusBadRequest)
+		return
+	}
+	if config.Leverage <= 0 {
+		http.Error(w, "Leverage must be greater than 0", http.StatusBadRequest)
+		return
+	}
+	if config.MarginType == "" {
+		http.Error(w, "MarginType is required", http.StatusBadRequest)
+		return
+	}

In internal/usecase/level_service.go:

>  				}
-			} else if pos.Side == domain.SideShort {
-				// Short Zone: [BaseLevel * (1 - MaxTierPct), BaseLevel]
-				lowerBound := l.LevelPrice * (1 - maxTierPct)
-				if price >= lowerBound && price <= l.LevelPrice {
+				if distance < 0.01 { // 1%

[nitpick] The magic number 0.01 representing 1% should be extracted as a named constant (e.g., strictZoneThreshold) to improve code readability and make it easier to adjust the threshold in the future.

In internal/web/templates/coins.html:

> +                <thead>
+                    <tr>
+                        <th onclick="sortTable(0)">Symbol ▼</th>
+                        <th onclick="sortTable(1)">Base Coin ▼</th>
+                        <th onclick="sortTable(2)">Quote Coin ▼</th>
+                        <th onclick="sortTable(3)">Status ▼</th>
+                        <th>Action</th>
+                    </tr>
+                </thead>
+                <tbody>
+                    {{range .Instruments}}
+                    <tr>
+                        <td>{{.Symbol}}</td>
+                        <td>{{.BaseCoin}}</td>
+                        <td>{{.QuoteCoin}}</td>
+                        <td class="{{if eq .Status " Trading"}}status-trading{{else}}status-other{{end}}">{{.Status}}

The status comparison has an extra space: ' Trading' instead of 'Trading'. This will cause the status-trading CSS class to never be applied. Remove the leading space in the comparison.
⬇️ Suggested change

-                        <td class="{{if eq .Status " Trading"}}status-trading{{else}}status-other{{end}}">{{.Status}}
+                        <td class="{{if eq .Status "Trading"}}status-trading{{else}}status-other{{end}}">{{.Status}}

In internal/usecase/speed_bot_service.go:

> +	// Start bot loop with background context (not request context!)
+	// The request context expires after the HTTP response is sent
+	go bot.run(context.Background())

[nitpick] Using context.Background() is correct for long-running goroutines, but the service should create and manage a cancellable context that can be properly cancelled during shutdown. Consider storing a parent context in SpeedBotService that can be cancelled when the service shuts down.