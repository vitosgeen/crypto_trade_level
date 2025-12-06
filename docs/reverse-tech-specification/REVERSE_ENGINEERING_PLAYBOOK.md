# Reverse Engineering Playbook

This document outlines the step-by-step process for reverse-engineering a codebase to create a comprehensive Technical Specification. This playbook is designed to be used on any project to extract "Truth" from "Code".

## 1. Preparation & Mindset

**Goal**: Understand *what* the software does and *how* it does it, without relying on outdated documentation.
**Mindset**: You are an archaeologist. The code is the artifact. Comments are hints (sometimes misleading). Tests are proofs.

### Tools
- **File Explorer**: `ls`, `tree`, or `find_by_name` to see the shape of the project.
- **Search**: `grep` or `ripgrep` to find usage of terms.
- **Diagramming**: Mermaid.js for flowcharts and sequence diagrams.
- **IDE**: For "Go to Definition" and "Find Usages".

---

## 2. Reconnaissance (The "10,000ft View")

Before diving into code, understand the landscape.

### Steps:
1.  **Map the Directory Structure**:
    - Run `ls -R` or `tree` (depth 2-3).
    - Identify standard patterns (e.g., Clean Architecture `internal/domain`, MVC `controllers/models`, or Flat structure).
2.  **Identify Entry Points**:
    - Look for `main.go`, `index.js`, `app.py`.
    - Look for `cmd/` directories.
    - **Action**: Note down where the application starts.
3.  **Identify Configuration**:
    - Look for `config.yaml`, `.env`, `settings.py`.
    - **Action**: This tells you what external systems it talks to (DBs, APIs).

---

## 3. Domain Discovery (The "Vocabulary")

Understand the business language before the logic.

### Steps:
1.  **Locate Domain Entities**:
    - Check `internal/domain`, `models`, or `types`.
    - Look for core structs/classes (e.g., `Level`, `User`, `Order`).
2.  **Create a Terminology Table**:
    - Define what each term means in *this* context.
    - *Example*: "Does 'Order' mean a user request or an exchange order?"
3.  **Map Relationships**:
    - Does a `Level` have many `Trades`?
    - Is `Position` ephemeral or persisted?

---

## 4. Architecture Mapping (The "Skeleton")

Determine how the pieces fit together.

### Steps:
1.  **Identify Layers**:
    - **Interface**: Web handlers, CLI commands (`internal/web`).
    - **Business Logic**: Services, Use Cases (`internal/usecase`).
    - **Data Access**: Repositories, SQL files (`internal/infrastructure/storage`).
    - **External**: API clients (`internal/infrastructure/exchange`).
2.  **Draw the Block Diagram**:
    - Create a Mermaid diagram showing dependencies.
    - *Rule*: Arrows point to dependencies (e.g., Handler -> Service -> Repository).

---

## 5. Tracing the Flows (The "Red Thread")

Follow the data from input to output.

### Steps:
1.  **Pick a Key Feature**: (e.g., "User adds a new Level").
2.  **Trace the Path**:
    - **Trigger**: HTTP POST `/levels`.
    - **Handler**: `handleCreateLevel` parses JSON.
    - **Service**: `LevelService.Create()` validates logic.
    - **Storage**: `SQLiteRepo.Save()` writes to DB.
3.  **Document the Sequence**:
    - Use a Sequence Diagram to show the call order.
4.  **Repeat for Core Loops**:
    - Find the "Heartbeat" (e.g., `PriceTicker`).
    - Trace: `WebSocket` -> `Channel` -> `StrategyEvaluator` -> `TradeExecutor`.

---

## 6. Data Modeling (The "State")

Code is ephemeral; data is persistent.

### Steps:
1.  **Analyze Storage**:
    - Read `schema.sql`, `migrations/`, or ORM definitions.
2.  **Map Structs to Tables**:
    - Note any discrepancies (e.g., struct has `ComputedVal` not in DB).
3.  **Identify Runtime State**:
    - Look for `map[string]...` or `channels` in Services.
    - **Critical**: This state is lost on restart. Document it!

---

## 7. Logic Extraction (The "Brain")

The hardest part: converting `if/else` back into Business Rules.

### Steps:
1.  **Find the "Brain" Files**:
    - Look for `Evaluator`, `Engine`, `Calculator`, `Strategy`.
2.  **Extract Formulas**:
    - *Code*: `if price > level * 1.05`
    - *Spec*: "Trigger when price exceeds Level + 5%."
3.  **Identify State Machines**:
    - Look for enums (`StatusPending`, `StatusActive`).
    - Draw a State Diagram (e.g., `Pending` -> `Filled` -> `Closed`).

---

## 8. Drafting the Specification

Assemble your findings into the final document.

### Template Structure:
1.  **Purpose**: One sentence summary.
2.  **Terminology**: The dictionary from Step 3.
3.  **Architecture**: Diagrams from Step 4.
4.  **Data Model**: DB Schema and Structs from Step 6.
5.  **Key Flows**: Sequence diagrams from Step 5.
6.  **Core Logic**: Business rules from Step 7.
7.  **API/Interface**: Endpoints and commands.
8.  **Configuration**: Parameters found in Step 2.

---

## 9. Verification (The "Sanity Check")

### Steps:
1.  **Read the Tests**:
    - Tests often reveal edge cases the code hides.
    - If a test says `expect_error_when_negative`, document that rule.
2.  **Compare**:
    - Does the code actually do what you wrote?
    - If unsure, add a `TODO` or `Question` in the spec.

---

## Example: Reverse Engineering `crypto_trade_level`

**1. Recon**: Found `main.go` in `cmd/bot`. Found `internal/web` (UI) and `internal/usecase` (Logic).
**2. Domain**: Identified `Level` (anchor price) and `Tier` (sub-levels).
**3. Flow**:
   - *Input*: WebSocket price tick.
   - *Logic*: `LevelEvaluator` checks `Price` vs `Level`.
   - *Action*: `TradeExecutor` calls `BybitAdapter`.
**4. Logic**: Found "Doubling Logic" in `SublevelEngine` (1x, 2x, 4x).
**5. Output**: Wrote `TECH_SPEC_LEVEL_TRADER_BOT.md`.
