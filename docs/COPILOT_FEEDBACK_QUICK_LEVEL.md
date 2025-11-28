@Copilot commented on this pull request.
Pull request overview

This PR adds a "quick levels" feature that enables faster level creation through keyboard and mouse interactions. Users can hold Shift while hovering over the price chart to preview tier levels, and Shift+click to instantly add a level at the clicked price.

Key Changes:

    Shift+click on chart to auto-populate price input and submit the form
    Shift+hover displays preview lines showing where tier levels would be placed
    Real-time cursor percentage display showing distance from current price

💡 Add Copilot custom instructions for smarter, more guided reviews. Learn how to get started.

In internal/web/templates/index.html:

> +                            // Visual feedback
+                            const originalColor = priceInput.style.borderColor;
+                            priceInput.style.borderColor = '#00ff88';
+                            setTimeout(() => {
+                                priceInput.style.borderColor = originalColor;
+                            }, 500);

[nitpick] The visual feedback timeout doesn't store a reference to clear it if the user clicks multiple times rapidly. This could lead to the border color being reset prematurely or remaining in the highlighted state longer than expected.

Consider storing and clearing the timeout:

let borderResetTimeout;
if (borderResetTimeout) clearTimeout(borderResetTimeout);

const originalColor = priceInput.style.borderColor;
priceInput.style.borderColor = '#00ff88';
borderResetTimeout = setTimeout(() => {
    priceInput.style.borderColor = originalColor;
}, 500);

In internal/web/templates/index.html:

> +                            const form = document.getElementById('add-level-form');
+                            if (form) {
+                                htmx.trigger('#add-level-form', 'submit');
+                            }

[nitpick] The form retrieval check on line 656 is redundant since the form variable is declared but only checked afterward. If document.getElementById('add-level-form') returns null, the code will still attempt to use htmx.trigger with a selector that may not exist.

Simplify to:

// Submit form
htmx.trigger('#add-level-form', 'submit');

The htmx.trigger will handle the case where the element doesn't exist gracefully.
⬇️ Suggested change

-                            const form = document.getElementById('add-level-form');
-                            if (form) {
-                                htmx.trigger('#add-level-form', 'submit');
-                            }
+                            htmx.trigger('#add-level-form', 'submit');

In internal/web/templates/index.html:

> +            if (previewPriceLines.length > 0) {
+                previewPriceLines.forEach(line => candleSeries.removePriceLine(line));
+                previewPriceLines = [];
+            }

The clearPreview function doesn't check if candleSeries is null before calling removePriceLine. If this function is called before the chart is initialized (e.g., during the keyup event when Shift is released before chart initialization completes), it will cause a runtime error.

Add a null check:

function clearPreview() {
    if (!candleSeries || previewPriceLines.length === 0) return;
    previewPriceLines.forEach(line => candleSeries.removePriceLine(line));
    previewPriceLines = [];
}

⬇️ Suggested change

-            if (previewPriceLines.length > 0) {
-                previewPriceLines.forEach(line => candleSeries.removePriceLine(line));
-                previewPriceLines = [];
-            }
+            if (!candleSeries || previewPriceLines.length === 0) return;
+            previewPriceLines.forEach(line => candleSeries.removePriceLine(line));
+            previewPriceLines = [];

In internal/web/templates/index.html:

>              } catch (e) {
                 console.error("Error initializing chart:", e);
             }
         }
 
+        function clearPreview() {
+            if (previewPriceLines.length > 0) {
+                previewPriceLines.forEach(line => candleSeries.removePriceLine(line));
+                previewPriceLines = [];
+            }
+        }
+
+        function updatePreview(basePrice) {

The updatePreview function doesn't check if candleSeries is null before calling createPriceLine. If updatePreview is called before chart initialization (e.g., if a user presses Shift while hovering before the chart loads), this will cause a runtime error.

Add a null check at the beginning of the function:

function updatePreview(basePrice) {
    if (!candleSeries) return;
    clearPreview();
    if (!basePrice) return;
    // ... rest of function
}

⬇️ Suggested change

-        function updatePreview(basePrice) {
+        function updatePreview(basePrice) {
+            if (!candleSeries) return;

In internal/web/templates/index.html:

> +        document.addEventListener('keydown', (e) => {
+            if (e.key === 'Shift') {
+                isShiftPressed = true;
+                if (lastCrosshairPrice) {
+                    updatePreview(lastCrosshairPrice);
+                }
+            }
+        });
+
+        document.addEventListener('keyup', (e) => {
+            if (e.key === 'Shift') {
+                isShiftPressed = false;
+                clearPreview();
+            }
+        });

The Shift key event listeners will trigger the shift-hold behavior for any Shift key press on the page, even when the user is typing in input fields. This could cause the preview lines to appear unexpectedly while filling out the form.

Consider restricting the Shift key detection to only activate when the cursor is over the chart:

let chartHovered = false;

// In initChart(), add:
chart.chartElement().addEventListener('mouseenter', () => chartHovered = true);
chart.chartElement().addEventListener('mouseleave', () => {
    chartHovered = false;
    clearPreview();
});

// Then modify the keydown handler:
document.addEventListener('keydown', (e) => {
    if (e.key === 'Shift' && chartHovered) {
        isShiftPressed = true;
        if (lastCrosshairPrice) {
            updatePreview(lastCrosshairPrice);
        }
    }
});

In internal/web/templates/index.html:

> +            const t1Pct = t1Input ? parseFloat(t1Input.value) / 100 : 0.005;
+            const t2Pct = t2Input ? parseFloat(t2Input.value) / 100 : 0.003;
+            const t3Pct = t3Input ? parseFloat(t3Input.value) / 100 : 0.0015;

The parseFloat calls on the tier input values don't validate for NaN results. If a user clears an input field or enters invalid text, parseFloat will return NaN, leading to incorrect tier calculations (e.g., basePrice * (1 + NaN) results in NaN).

Add validation to handle invalid inputs:

const t1Pct = t1Input && !isNaN(parseFloat(t1Input.value)) ? parseFloat(t1Input.value) / 100 : 0.005;
const t2Pct = t2Input && !isNaN(parseFloat(t2Input.value)) ? parseFloat(t2Input.value) / 100 : 0.003;
const t3Pct = t3Input && !isNaN(parseFloat(t3Input.value)) ? parseFloat(t3Input.value) / 100 : 0.0015;

⬇️ Suggested change

-            const t1Pct = t1Input ? parseFloat(t1Input.value) / 100 : 0.005;
-            const t2Pct = t2Input ? parseFloat(t2Input.value) / 100 : 0.003;
-            const t3Pct = t3Input ? parseFloat(t3Input.value) / 100 : 0.0015;
+            const t1Pct = t1Input && !isNaN(parseFloat(t1Input.value)) ? parseFloat(t1Input.value) / 100 : 0.005;
+            const t2Pct = t2Input && !isNaN(parseFloat(t2Input.value)) ? parseFloat(t2Input.value) / 100 : 0.003;
+            const t3Pct = t3Input && !isNaN(parseFloat(t3Input.value)) ? parseFloat(t3Input.value) / 100 : 0.0015;
