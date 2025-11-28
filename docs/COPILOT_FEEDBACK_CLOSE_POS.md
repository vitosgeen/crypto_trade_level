@Copilot commented on this pull request.
Pull request overview

This PR implements comprehensive position history tracking, manual position closing functionality, and realized PnL display across trades and history views. The implementation includes database schema changes, service layer refactoring to consolidate position closing logic, and UI enhancements to display historical data.

    Centralizes position finalization logic into a reusable finalizePosition method
    Adds position history tracking with PnL calculations for closed positions
    Introduces manual close button for active positions with HTMX integration

Reviewed changes

Copilot reviewed 8 out of 8 changed files in this pull request and generated 3 comments.
Show a summary per file
File 	Description
internal/domain/position.go 	Adds RealizedPnL field to Order struct and new PositionHistory struct for tracking closed positions
internal/domain/interfaces.go 	Extends TradeRepository interface with position history methods
internal/infrastructure/storage/sqlite.go 	Creates position_history table, adds realized_pnl column to trades, implements history persistence
internal/usecase/level_service.go 	Refactors position closing logic into finalizePosition method, adds ClosePosition for manual closes
internal/usecase/level_service_test.go 	Adds tests for manual close, PnL calculation, and position history tracking
internal/web/server.go 	Adds routes for manual position close and history table retrieval
internal/web/handlers.go 	Implements handleClosePosition and handleHistoryTable handlers with history data fetching
internal/web/templates/index.html 	Adds positions history section, profit column to trades, close button to positions, updates tier parsing logic

💡 Add Copilot custom instructions for smarter, more guided reviews. Learn how to get started.

In internal/web/templates/index.html:

> @@ -711,23 +719,33 @@ <h2>Recent Trades</h2>
                 console.error("Failed to fetch candles", e);
             }
 
+            // Tiers
             // Tiers

Duplicate comment '// Tiers' on consecutive lines. Remove one of the duplicate comments.
⬇️ Suggested change

-            // Tiers

In internal/web/templates/index.html:

> +            <th>Side</th>
+            <th>Size</th>
+            <th>Entry</th>
+            <th>Exit</th>
+            <th>PnL</th>
+            <th>Lev</th>
+            <th>Margin</th>
+        </tr>
+    </thead>
+    <tbody>
+        {{ if . }}
+        {{ range . }}
+        <tr>
+            <td class="text-muted">{{ .ClosedAt.Format "15:04:05" }}</td>
+            <td><strong>{{ .Symbol }}</strong></td>
+            <td style="color: {{ if or (eq .Side " Buy") (eq .Side " Buy" ) }}var(--success){{ else }}var(--danger){{

Duplicate condition (eq .Side \" Buy\") in the template logic. The or expression checks the same condition twice, which is redundant and likely a copy-paste error. Remove the duplicate condition.
⬇️ Suggested change

-            <td style="color: {{ if or (eq .Side " Buy") (eq .Side " Buy" ) }}var(--success){{ else }}var(--danger){{
+            <td style="color: {{ if (eq .Side " Buy") }}var(--success){{ else }}var(--danger){{

In internal/usecase/level_service.go:

> +			// Note: finalizePosition resets state for ALL levels.
+			// But we want to update ConsecutiveWins for THIS level.
+			// Since we just reset it, we need to re-apply the win/loss to the fresh state?
+			// Or we should have updated it BEFORE reset.
+			// But finalizePosition resets it.
+			// This is a problem. finalizePosition resets state indiscriminately.
+
+			// Solution: Update state AFTER finalizePosition (which resets it).
+			// If we update it after reset, it will be clean state + wins.

These comments indicate uncertainty about the correctness of the state management logic. The comments suggest there's a potential race condition or ordering issue where finalizePosition resets state for all levels, but then ConsecutiveWins is updated only for the current level after the reset. This could lead to incorrect win streak tracking. Consider either: (1) updating the state before calling finalizePosition and preserving it, or (2) passing win/loss information to finalizePosition to handle it internally, or (3) removing these comments if the current approach is intentionally correct and documenting why.