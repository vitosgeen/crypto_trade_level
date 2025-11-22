Quick summary of what changed

    internal/usecase/level_service.go
        After attempting exchange.ClosePosition, the code now fetches the engine state and chooses a closingSide from state.ActiveSide (falling back to the current side). Then it uses closingSide for the Close trade record.
        ResetState(level.ID) is still called after the ClosePosition attempt.
    tests/scenarios_test.go
        Many additional scenario tests were added (G3, G4, H1–H6, I1–I3, plus small changes/skips). Some tests are marked t.Skip and some construct levels with missing tiers or missing levels to assert no panics.

Potential issues I noticed

    Nil-state panic risk
        s.engine.GetState(level.ID) might return nil. The code references state.ActiveSide without a nil check, which will panic. You need to guard against a nil state before accessing ActiveSide.

    Type/field mismatch in tests
        In H5 the test builds a domain.Level with Exchange: h.exchange — ensure Exchange expects the same type (string vs mock object). If Exchange is a string, passing h.exchange (likely a mock struct) will compile-fail.

    Concurrency / ordering considerations
        You call exchange.ClosePosition, then read engine state, then reset state. Consider whether the engine state could change concurrently (e.g., by another goroutine) between ClosePosition and GetState. If the intended closingSide must reflect the state at the time of closing, consider capturing or locking state earlier or reading state before issuing close.

    Silent Close failure handling
        ClosePosition errors are logged but the flow proceeds to ResetState and create a Close trade with closingSide. That may be fine (you commented earlier about stop-loss scenarios) but ensure this aligns with expected behavior (e.g., do we want to record a Close trade if ClosePosition failed?).

    Tests asserting last trade on a Close use SideShort with size 0; ensure code that logs close trade uses the chosen closingSide and size/marker as tests expect.

Suggested minimal fix

    Add a nil-check and fallback for state.ActiveSide before using it. Example (Go-like pseudocode matching your style):

state := s.engine.GetState(level.ID) closingSide := side // fallback if state != nil && state.ActiveSide != "" { closingSide = state.ActiveSide }

    Optionally, decide whether to GetState before or after ClosePosition depending on concurrency semantics and document it in comment.

Offer

    I can prepare a small PR patch that:
        Adds the nil-check around state.ActiveSide,
        Adds a short comment about the ordering/concurrency and intent,
        Optionally adjusts any test code that looks like a type mismatch (if you want me to change tests too).
    Or I can run through the rest of the diffs and add more targeted suggestions (e.g., change logging, test assertions, or error handling policy).


Short note you can save for later (tasks / rationale)

    Problem: accessing state.ActiveSide after ClosePosition may panic if engine.GetState(...) returns nil.
    Fix: guard against nil and empty ActiveSide; prefer state.ActiveSide when present, otherwise fall back to the current side.
    Concurrency: decide whether to read state before or after calling exchange.ClosePosition. If you want the side as it was at the moment of close, read/capture it under the same synchronization/ordering as the close call (or document intended behavior).
    Error handling: decide whether a failed ClosePosition should still produce a Close trade record. If not, skip the trade record on error; if yes, record with a marker and ensure state reset is still safe.
    Tests: add a unit test verifying nil state is handled (no panic) and that Close trade uses ActiveSide when available. Check test code for any type mismatches (e.g., h.exchange vs expected string).
    Optional: add a short comment in code explaining the ordering decision and why you reset state even if ClosePosition returns an error.

Suggested PR title and description for later

    Title: "Guard engine state when recording Close trade; use ActiveSide if present"
    Description: "Avoid nil-pointer panic when engine state is missing; prefer state.ActiveSide for Close trade Side; add unit test to ensure behavior and document ordering/intent around ClosePosition and ResetState. Also adjust tests that assert Close trade side."

Next steps I can take now (pick one)

    Draft the PR with the nil-check and a small unit test.
    Create a GitHub issue with the summary and checklist above.
    Just save this note and wait until you tell me to implement.

File filter
			closingSide := state.ActiveSide
Comment on lines +146 to +147
Copilot AI
19 minutes ago

Potential race condition: GetState() returns a pointer to the state while holding a read lock, but the lock is released immediately. The code then accesses state.ActiveSide (line 147) without any synchronization, and later calls ResetState() (line 153). If another goroutine is processing a tick for the same level concurrently, it could modify or delete the state between these operations, leading to a race condition.

Consider either:

    Copying the ActiveSide value while still holding the lock in GetState(), or
    Having GetState() return a copy of the state instead of a pointer, or
    Adding a method like GetActiveSide(levelID string) domain.Side that returns the value safely.

Copilot uses AI. Check for mistakes.
@vitosgeen
