# Hermes Director Orchestrator Implementation Plan

> **For agentic workers:** implement in the listed order. The daemon owns the workflow state machine; Hermes and other agents execute phases and report verdicts. Keep `ValidateWorkflowTransition` server-authoritative; agents never bypass it.

**Goal:** Make the card lifecycle actually run end-to-end: `Todo → Running → Review → Testing → Redo → Done`. Hermes is the primary coding worker; reviewers/testers are selected from a project-configured agent chain with a final fallback to Hermes. The daemon is the orchestrator — it spawns the right agent per phase, monitors PR/CI signals, recovers orphan cards, and validates every transition.

**Current facts:** Auto-dispatch (`.hermes/plans/2026-08-02_183500-director-auto-dispatch.md`) ships and reliably moves cards from `todo → running` with WIP=4, `dispatch_failed` events, and `Retry dispatch`. The state machine (`backend/internal/domain/workboard.go:44-78`) validates `running → review → testing → redo → done` but nothing drives it. `dispatch.go:43` already documents that "Hermes commander is not currently reachable." Reviewer adapters exist for `backend/internal/adapters/reviewer/{claudecode,codex,opencode}` only — no `greptile` reviewer adapter exists today. `agy` and `kilocode` exist under `backend/internal/adapters/agent/` as generic coding-agent adapters but have **no** reviewer/tester adapter counterpart. No orchestrator invokes any of them. RedoCycle/Finding/Attempt tables exist in domain but have no producer. The Hermes adapter (`backend/internal/adapters/agent/hermes/hermes.go`) is a plain worker launcher with no commander loop.

## Locked decisions

### Orchestrator model

- **Daemon is the only orchestrator.** It owns the state machine, decides the next phase, validates transitions, and spawns the right agent.
- **Hermes is the coding worker**, not a commander. One Hermes session per card lifecycle. The session edits code, opens a PR, and reports completion. It does not decide next phases.
- **Reviewers and testers are other agents** drawn from a project-configured priority list, with Hermes as the final fallback. Default chain uses only agents with an existing reviewer/tester adapter today (Claude Code, Codex, OpenCode); Agy, Kilo, Greptile can be added to the chain once Task 3a builds their reviewer/tester adapters — not before.
- **Agents report verdicts via `ao` CLI** (universal) and via custom tool injection where supported (Claude Code, Hermes, Codex).
- **Critical events pushed to active agent session**; routine state via agent polling.

### Phase semantics

- **WIP=4 means 4 cards in any active phase** (`running`, `review`, `testing`, `redo`). Reviewer/tester sessions are children of the active card and do not count separately toward WIP.
- **One Hermes session per card lifecycle.** Hermes dies at `done` or when the card is permanently killed.
- **Redo cycle spawns a new Hermes session** for the same card with full cycle history attached as briefing.

### Failure and fallback

- **Fail triggers (any one switches agent):** timeout, non-zero exit, inconclusive verdict, structured error.
- **Fallback chain per project config:** ordered list of agents per phase; if the active agent fails, daemon tries the next; Hermes is always the last entry.
- **PR/CI monitoring owned by daemon** (webhook + polling). Reviewer is spawned after CI green; CI red is reported back to the active Hermes session for in-place fix (no Redo).

### Recovery

- **Orphan detection:** card in `running` with no active session for > N minutes → daemon spawns replacement Hermes session with card+cycle history as briefing.
- **Daemon state is the source of truth.** Agent sessions are stateless w.r.t. workflow; they read state via API and report back.

---

# Phase 0 — Plan contract and current-state capture

### Task 1: Record red tests proving the lifecycle is unwired today

**Objective:** Before any code, prove with tests that the orchestrator currently does not advance past `running`.

**Files:**

- New test: `backend/internal/service/workboard/lifecycle_dispatcher_test.go`
- Inspect: `backend/internal/service/workboard/dispatch.go`, `backend/internal/service/workboard/dispatch_test.go`

**Steps:**

1. Write a test that puts a card into `running` with a stub worker session and asserts: no reviewer is invoked, no transition to `review` happens, no PR monitoring is registered. Test must be red on `main`.
2. Write a test that asserts there is no current producer of `review`, `testing`, or `redo` transitions. Test must be red.
3. Add a test that asserts the Hermes adapter does not implement any commander interface. Test must be red.

**Verification:** `cd backend && go test ./internal/service/workboard/... -run TestLifecycleDispatcherIsUnwired`. All three tests fail on `main`.

### Task 2: Define orchestrator interface and port seams

**Objective:** Lock the boundary between daemon orchestrator and agent runtime before implementation.

**Files:**

- New: `backend/internal/commander/port.go`
- Modify: `backend/internal/ports/agent.go` (add spawn-phase capability flag)

**Interface shape:**

```go
type Phase string
const (
    PhaseCoding   Phase = "coding"
    PhaseReview   Phase = "review"
    PhaseTesting  Phase = "testing"
)

type Orchestrator interface {
    Tick(ctx context.Context, projectID string) error
    OnAgentFailed(ctx context.Context, cardID string, attempt AgentAttempt) error
    OnAgentCompleted(ctx context.Context, cardID string, result AgentResult) error
    OnSignal(ctx context.Context, cardID string, signal Signal) error
}

type Spawner interface {
    Spawn(ctx context.Context, spec SpawnSpec) (SessionHandle, error)
    Stop(ctx context.Context, sessionID string) error
    Inject(ctx context.Context, sessionID string, message string) error
}

type SpawnSpec struct {
    CardID       string
    ProjectID    string
    Phase        Phase
    Agent        string // harness id
    Briefing     string
    ParentCard   *WorkCard
    CycleHistory *RedoCycle // nil for first cycle
}
```

**Verification:** interface compiles; existing dispatch tests still pass.

---

# Phase 1 — Domain extensions for agent chains and briefing payloads

### Task 3: Replace single Reviewer/Testing agent with ordered agent chain

**Objective:** Move from `ReviewerAgent`/`TestingAgent` strings to ordered `ReviewerAgents`/`TestingAgents` arrays with Hermes at the end.

**Files:**

- Modify: `backend/internal/domain/workboard.go`
- Modify: `backend/internal/domain/projectconfig.go`
- Modify: `backend/internal/storage/sqlite/migrations/<next>_agent_chain.sql`
- Modify: `backend/internal/storage/sqlite/queries/workboard.sql`
- Regenerate: `backend/internal/storage/sqlite/gen/*` via `npm run sqlc`
- Modify: `backend/internal/httpd/controllers/dto.go`

**Schema additions:**

```sql
ALTER TABLE work_card ADD COLUMN reviewer_agents_json TEXT;     -- JSON array
ALTER TABLE work_card ADD COLUMN testing_agents_json TEXT;      -- JSON array
ALTER TABLE work_card ADD COLUMN coding_agent TEXT;            -- primary coding harness
ALTER TABLE project_config ADD COLUMN default_reviewer_agents_json TEXT;
ALTER TABLE project_config ADD COLUMN default_testing_agents_json TEXT;
```

**Domain updates:**

- `WorkCard.ReviewerAgents []string`
- `WorkCard.TestingAgents []string`
- `WorkCard.CodingAgent string`
- `WorkCard.ReviewerAgent`/`TestingAgent` deprecated but readable (kept for backward compat)
- `WorkboardConfig.DefaultReviewerAgents []string`
- `WorkboardConfig.DefaultTestingAgents []string`
- New helper: `ResolveAgentChain(card WorkCard, phase Phase, registry Registry) []string` returns the ordered chain with Hermes always last

**Default chain (when no project/card override) — limited to agents with a reviewer/tester adapter that exists today:**

```
ReviewerAgents = [claude-code, codex, opencode, hermes]
TestingAgents  = [claude-code, codex, opencode, hermes]
```

Agy, Kilo, Greptile are deliberately excluded from the default chain until Task 3a ships their adapters. A project can still reference them explicitly in a card/project override once built — `ResolveAgentChain` should not special-case defaults vs overrides, so this is purely a default-list decision, not a hardcoded restriction.

**Verification:** `cd backend && go test ./internal/domain/...` — chain resolution tests cover override semantics and Hermes-last invariant.

### Task 3a: Build reviewer/tester adapters for Agy and Kilo (optional, only if needed before ship)

**Objective:** Close the gap where Agy and Kilo exist as generic coding-agent adapters but have no reviewer/tester counterpart, so they can be added to a project's agent chain.

**Files:**

- New: `backend/internal/adapters/reviewer/agy/`, `backend/internal/adapters/reviewer/kilocode/`
- Mirror the existing `claudecode`/`codex`/`opencode` reviewer adapter shape.

**Note:** Skip this task entirely if the default chain (Task 3) is sufficient for the initial ship — do not build speculative adapters ahead of a project actually configuring them. Revisit when a project override references an agent with no adapter and `ResolveAgentChain` has nothing to spawn.

**Verification:** reviewer adapter registry test asserts the new adapters implement the same interface as `claudecode`.

### Task 4: Persist agent-attempt history on Redo cycles

**Objective:** Make per-attempt records durable so briefing can reference what already failed.

**Files:**

- Modify: `backend/internal/domain/workboard.go` (extend `RedoAttempt` with `Phase`, `Agent`, `FailureReason`)
- New migration: `backend/internal/storage/sqlite/migrations/<next>_attempt_phase.sql`
- Modify: `backend/internal/storage/sqlite/queries/workboard.sql`
- Regenerate: `backend/internal/storage/sqlite/gen/*` via `npm run sqlc`

**Schema additions:**

```sql
ALTER TABLE work_card_attempt ADD COLUMN phase TEXT;           -- coding|review|testing
ALTER TABLE work_card_attempt ADD COLUMN failure_reason TEXT;  -- timeout|error|inconclusive|spawn_failed
```

**Verification:** storage round-trip test inserts an attempt with phase=failure_reason and reads it back.

---

# Phase 2 — Orchestrator service and agent spawner

### Task 5: Implement the Hermes-aware spawner that launches coding sessions

**Objective:** Centralize how a coding/reviewer/testing session is launched, with briefing injected and tool injection enabled for supported harnesses.

**Files:**

- New: `backend/internal/commander/spawner/spawner.go`
- New: `backend/internal/commander/spawner/briefing.go`
- New: `backend/internal/commander/spawner/tool_inject.go`
- New tests: `backend/internal/commander/spawner/spawner_test.go`

**Behavior:**

- `Spawn(spec)` builds a launch command via the existing adapter registry.
- Briefing prompt includes:
  - Card title, notes, target path, latest cycle summary, current finding (if redo), allowed commands, fail history
  - Reference to AO CLI: `ao workboard card show <id>`, `ao workboard card transition <id> --to <status> --reason <text>`
  - Reminder that transitions are validated server-side
- Tool manifest injection for Hermes/Claude Code/Codex is deferred to Task 11 (see sequencing note there) — Task 5 ships CLI-only briefing first.
- Records `(card_id, session_id, phase, agent)` into a new `active_session` table.
- `Spawn` must be safe under concurrent calls for different cards: two cards finishing dispatch at the same time must not race on the WIP check or double-insert an `active_session` row for the same card. Use a per-card lock (or a unique constraint on `active_session.card_id` + insert-or-fail) — not a global mutex across all cards.

**Verification:** unit tests assert the launch argv for each harness; a concurrency test spawns for N cards in parallel and asserts exactly one `active_session` row per card; integration test via `ao spawn --dry-run` (if available) shows expected prompt.

### Task 6: Implement the orchestrator tick loop and signal handlers

**Objective:** Subscribe CDC card-state changes, decide the next action, and drive transitions.

**Files:**

- New: `backend/internal/commander/orchestrator/orchestrator.go`
- New: `backend/internal/commander/orchestrator/tick.go`
- New: `backend/internal/commander/orchestrator/fallback.go`
- New: `backend/internal/commander/orchestrator/recovery.go`
- New tests: `backend/internal/commander/orchestrator/orchestrator_test.go`

**Tick responsibilities:**

1. List cards in active phases (`running`, `review`, `testing`, `redo`) for the project.
2. For each, check whether `active_session` row exists and process is alive.
3. If `running` and no session: spawn the next coding agent (Hermes on first attempt).
4. If `review` and no session: spawn the next reviewer from chain (Hermes last).
5. If `testing` and no session: spawn the next tester from chain.
6. If `redo` and no session: spawn new Hermes session with cycle history briefing.
7. Honor WIP=4 limit before spawning anything.
8. On agent failed/completed event: update `active_session`, write RedoAttempt, transition card.

**Recovery responsibilities:**

- Card in `running` with `active_session` older than 30 min and no recent activity → spawn replacement session with cycle history.
- Same for `review`/`testing` with 10 min timeout (reviewers faster).

**Fallback responsibilities:**

- `OnAgentFailed(cardID, attempt)`:
  - Increment cycle attempt count for the phase
  - Append `RedoAttempt` row with `phase` + `failure_reason`
  - Pick next agent from chain
  - If exhausted → final fallback to Hermes with `redo` cycle
  - If current cycle already exhausted and reviewer/testing fails after Hermes too → emit structured finding, transition to `redo`, create new cycle

**Verification:** table tests for: spawn next coding, PR green spawns reviewer, reviewer verdict spawns tester, verdict=changes_requested creates RedoCycle, CI red reports to active Hermes, timeout triggers fallback, recovery spawns replacement.

---

# Phase 3 — PR/CI monitor and reviewer/tester invocation

### Task 7: Wire daemon PR/CI monitor into the orchestrator

**Objective:** Detect when the active Hermes (coding) worker has opened a PR and when CI is green, so the orchestrator can spawn the reviewer.

**Files:**

- New: `backend/internal/commander/scm/pr_watcher.go`
- New: `backend/internal/commander/scm/ci_watcher.go`
- Modify: `backend/internal/daemon/scm_wiring.go` (add hooks)
- Modify: `backend/internal/observe/` (add signal producers)

**Behavior:**

- `pr_watcher` polls registered SCM provider for open PRs linked to active `running` cards (by branch name pattern).
- When PR detected → register PR ID on `active_session`, mark card phase-internal `awaiting-ci`.
- `ci_watcher` polls check status. When all green → push `Signal: PRReady` to orchestrator.
- When any check red → push `Signal: CIFailed` to active Hermes session via `Spawner.Inject` (no Redo).
- When PR closed without merge → push `Signal: PRClosed` → trigger redo.

**Verification:** integration test with a fake SCM provider asserts: Hermes PR detected → reviewer spawned after green → CIFailed pushed to Hermes.

### Task 8: Implement reviewer invocation and verdict ingestion

**Objective:** Run the next reviewer from the project chain against the worker's PR and ingest its verdict.

**Files:**

- New: `backend/internal/commander/reviewer/invoker.go`
- New: `backend/internal/commander/reviewer/verdict.go`
- Modify: existing reviewer adapter wiring (`backend/internal/adapters/reviewer/`)
- New tests: `backend/internal/commander/reviewer/invoker_test.go`

**Behavior:**

- `Invoker.SpawnReview(cardID, prURL)` picks the first available agent from `card.ReviewerAgents` (or `project.DefaultReviewerAgents` → defaults chain → Hermes last).
- Spawns reviewer session with briefing containing: PR URL, diff stat, recent commits, fail history.
- Waits for reviewer verdict (CLI call, tool call, or timeout).
- Verdict shapes: `approved` | `changes_requested` | `inconclusive`.
- `approved` → orchestrator transitions `review → testing`.
- `changes_requested` → create RedoCycle with structured findings + transition `review → redo`.
- `inconclusive` or timeout → count as fail, fallback to next reviewer in chain.

**Verification:** table tests for each verdict shape and each fallback path; assertions that Hermes is the final fallback.

### Task 9: Implement testing invocation and result ingestion

**Objective:** Run the next tester from the project chain and ingest its results.

**Files:**

- New: `backend/internal/commander/testing/invoker.go`
- New: `backend/internal/commander/testing/result.go`
- New tests: `backend/internal/commander/testing/invoker_test.go`

**Behavior:**

- Same chain logic as reviewer, with `card.TestingAgents`.
- Briefing contains: PR URL, test commands (from project config), expected pass criteria.
- Test runner reports `pass` | `fail` with command, exit code, output, file refs.
- `pass` → orchestrator transitions `testing → done`.
- `fail` → create RedoCycle with testing findings + transition `testing → redo`.
- Timeout/error → fallback to next tester.

**Verification:** tests assert fallback chain, finding creation, and transition validation.

---

# Phase 4 — `ao` CLI surface and tool injection

### Task 10: Add CLI commands agents use to report back

**Objective:** Universal agent-to-daemon communication channel.

**Files:**

- New: `backend/internal/cli/workboard_card.go`
- Modify: `backend/internal/cli/root.go`
- New tests: `backend/internal/cli/workboard_card_test.go`

**Commands:**

```bash
ao workboard card show <cardId> [--project <projectId>]
ao workboard card transition <cardId> --to <status> --reason <text> [--finding-id <id>]
ao workboard card set-verdict <cardId> --verdict approved|changes_requested|inconclusive
ao workboard card set-finding <cardId> --severity critical|high|normal|low --title <t> --details <d> [--command <c>] [--file <path:line>]
ao workboard card set-test-result <cardId> --command <c> --exit <n> --output <text>
ao workboard card fail-attempt <cardId> --reason timeout|error|inconclusive|spawn_failed
```

All commands:

- Validate transition via `ValidateWorkflowTransition` (actor=`agent`).
- Validate verdict/finding/result shapes against existing domain types.
- Return existing API error envelope.
- Write `WorkCardEvent` audit row.

**Verification:** CLI table tests for each command covering happy path, validation failure, daemon error, invalid verdict shape.

### Task 11: Inject custom tool manifest into supported agents

**Sequencing note:** Defer this task until Task 10's CLI has run end-to-end (at minimum the acceptance scenarios in the Verification plan) for Hermes, Claude Code, and Codex. The CLI alone covers every harness including future ones; tool injection only helps these 3 and adds a second code path (manifest sync, two report mechanisms to test) for the same five actions. Build it only if the CLI proves clunky inside an agentic loop for these harnesses — not by default.

**Objective:** Structured alternative to CLI for agents that support tool injection.

**Files:**

- New: `backend/internal/commander/spawner/tool_inject.go`
- Modify: `backend/internal/adapters/agent/hermes/hermes.go` (add `InjectTools` capability)
- Modify: `backend/internal/adapters/agent/claudecode/` (mirror)
- Modify: `backend/internal/adapters/agent/codex/` (mirror)

**Tool manifest:**

```json
{
  "name": "ao_workboard",
  "tools": [
    {"name": "transition_card", "params": {"cardId": "string", "to": "string", "reason": "string"}},
    {"name": "set_verdict", "params": {"cardId": "string", "verdict": "approved|changes_requested|inconclusive"}},
    {"name": "set_finding", "params": {"cardId": "string", "severity": "string", "title": "string", "details": "string"}},
    {"name": "set_test_result", "params": {"cardId": "string", "command": "string", "exit": "number", "output": "string"}},
    {"name": "fail_attempt", "params": {"cardId": "string", "reason": "string"}}
  ]
}
```

**Verification:** spawn tests assert the manifest appears in launch argv/env for supported harnesses.

---

# Phase 5 — Wire orchestrator into daemon lifecycle

### Task 12: Register orchestrator with daemon CDC and periodic tick

**Objective:** Make the orchestrator part of the daemon's normal event loop.

**Files:**

- New: `backend/internal/daemon/orchestrator_wiring.go`
- Modify: `backend/internal/daemon/daemon.go`
- Modify: `backend/internal/daemon/cdc_wiring.go` (subscribe `work_card_changed`)
- New tests: `backend/internal/daemon/orchestrator_wiring_test.go`

**Behavior:**

- Daemon starts orchestrator goroutine on boot.
- Orchestrator subscribes to `change_log.work_card_changed` events.
- Orchestrator runs a periodic tick every 30s for orphan detection and PR/CI polling.
- On daemon shutdown, orchestrator gracefully stops all active sessions via `Spawner.Stop`.

**Verification:** daemon lifecycle test asserts orchestrator goroutine starts and stops cleanly; orphan test asserts replacement spawns after timeout.

### Task 13: Make WIP=4 effective across phases

**Objective:** Validate the existing WIP logic counts cards across `running`, `review`, `testing`, `redo` — not just `running`.

**Files:**

- Modify: `backend/internal/service/workboard/dispatch.go` (extend WIP counting)
- Modify: `backend/internal/service/workboard/service.go` (new `CountActiveCards` helper)
- Modify: `backend/internal/daemon/dispatch_trigger.go` (consume new helper)
- New tests: `backend/internal/service/workboard/wip_test.go`

**Behavior:**

- WIP query selects `running | review | testing | redo` (not just `running`).
- Orchestrator's pre-spawn check refuses to spawn if active count >= WIP.
- Card transition events automatically decrement count when leaving active phases.

**Verification:** tests assert: 4 cards in mixed phases blocks new dispatch; completion of one unblocks the next.

---

# Phase 6 — Frontend visibility

### Task 14: Show current phase, agent, and verdict in card detail

**Objective:** Make the lifecycle observable from Director.

**Files:**

- Modify: `backend/internal/httpd/controllers/workboard.go` (extend detail response)
- Modify: `backend/internal/httpd/controllers/dto.go` (add `phase`, `activeAgent`, `verdict` fields)
- Regenerate: `backend/internal/httpd/apispec/openapi.yaml`, `frontend/src/api/schema.ts` via `npm run api`
- Modify: `frontend/src/renderer/components/WorkCardFocusPanel.tsx`
- New tests: `frontend/src/renderer/components/WorkCardFocusPanel.test.tsx`

**Display:**

- Current phase badge (Running / Review / Testing / Redo).
- Active agent name + harness id.
- Last verdict and source.
- Last failure reason if any.
- Agent chain preview (collapsed): reviewers/testers ordered list.
- Redo cycle history with attempts and findings (already exists; extend with phase + failure_reason).

**Verification:** component tests assert rendering for each phase; backend tests assert DTO shape.

### Task 15: Render orchestrator health in Director header

**Objective:** Tell the user when the orchestrator is healthy vs stalled.

**Files:**

- Modify: `backend/internal/httpd/controllers/workboard.go` (add `/workboard/orchestrator/health`)
- Regenerate API + frontend schema
- Modify: `frontend/src/renderer/components/Workboard.tsx`
- Modify: `frontend/src/renderer/components/Workboard.test.tsx`

**Display:**

- `Orchestrator: healthy · last tick 4s ago`
- `Orchestrator: stalled · last tick 2m ago`
- `Orchestrator: offline` when daemon reports unhealthy.

**Verification:** header renders each state correctly; backend test asserts tick timestamp.

---

# Phase 7 — Migration and recovery

### Task 16: Migrate existing cards into the new chain model

**Objective:** Existing cards with old single `ReviewerAgent`/`TestingAgent` strings continue to work.

**Files:**

- New migration: `backend/internal/storage/sqlite/migrations/<next>_seed_agent_chains.sql`
- Modify: `backend/internal/storage/sqlite/store/workboard_store.go` (read both columns, prefer array)
- New tests: `backend/internal/storage/sqlite/store/migration_test.go`

**Migration policy:**

- If `reviewer_agents_json` is null and `reviewer_agent` exists → wrap into `[reviewer_agent]` array.
- If both null → use defaults `[claude-code, codex, agy, kilo, opencode, hermes]`.
- Same for testing.

**Verification:** migration test reads pre-migration fixture, applies migration, asserts chain resolves correctly.

### Task 17: Restart and orphan recovery

**Objective:** Daemon restart preserves card state and reconciles active sessions.

**Files:**

- Modify: `backend/internal/commander/orchestrator/recovery.go`
- New tests: `backend/internal/commander/orchestrator/recovery_test.go`

**Behavior:**

- On startup, orchestrator loads all cards in active phases.
- For each, check `active_session` and process liveness.
- Stale entries (no process, old timestamp) → spawn replacement with briefing that includes card state + last cycle history.
- Active entries with live process → resume monitoring.

**Verification:** restart simulation test: kill daemon mid-cycle, restart, assert all cards reconcile within 60s.

---

# Verification plan

## Focused backend checks

```bash
cd backend
go test ./internal/domain/... ./internal/commander/... ./internal/service/workboard/... ./internal/cli/... ./internal/httpd/...
go test -race ./internal/commander/...
go vet ./...
```

## Focused frontend checks

```bash
cd frontend
npm test -- --run src/renderer/components/WorkCardFocusPanel.test.tsx
npm test -- --run src/renderer/components/Workboard.test.tsx
npm run typecheck
npm run build
```

## Contract/generated checks

```bash
cd /Users/up-mac/wokrspace/mind/moden-agent
npm run api
cd backend && go test ./internal/httpd/...
```

## End-to-end acceptance scenarios

1. Card created → auto-dispatched → Hermes session spawned within 10s.
2. Hermes opens PR → CI green → reviewer (first in chain) spawned within 30s.
3. Reviewer verdict `approved` → tester spawned within 10s.
4. Tester reports `pass` → card transitions to `done` and audit event written.
5. Reviewer verdict `changes_requested` → RedoCycle #1 created → card moves to `redo`.
6. Card in `redo` → new Hermes session spawned with full cycle history in briefing.
7. First reviewer fails (timeout) → second reviewer in chain spawned; final fallback to Hermes.
8. CI check fails → active Hermes session receives inject message; no Redo cycle created.
9. Hermes session killed mid-phase → daemon detects orphan within 30 min, spawns replacement.
10. 5 cards in `running` → only 4 dispatch; 5th waits. Completing one unblocks the next.
11. Daemon restart mid-cycle → all cards reconcile, no duplicate sessions, no lost cycles.
12. User cannot drag a card between phases; the API rejects manual transitions with the existing error envelope.
13. Director header shows orchestrator health and ticks.

## Browser verification

Use MCP Chrome DevTools against the running frontend after rebuild:

1. Navigate with cache-busting query.
2. Open a card in `running`. Verify panel shows `Hermes` as active agent, recent activity, no verdict yet.
3. Open a card in `review`. Verify panel shows reviewer name, PR URL, verdict (if any), fallback chain preview.
4. Open a card in `redo`. Verify panel shows `Redo #N`, cycle history with attempts (phase + failure_reason), findings.
5. Take a screenshot of each phase's panel.

---

# Files map (overview)

```
backend/internal/
├── commander/                          # NEW
│   ├── port.go
│   ├── orchestrator/
│   │   ├── orchestrator.go
│   │   ├── tick.go
│   │   ├── fallback.go
│   │   └── recovery.go
│   ├── spawner/
│   │   ├── spawner.go
│   │   ├── briefing.go
│   │   └── tool_inject.go
│   ├── scm/
│   │   ├── pr_watcher.go
│   │   └── ci_watcher.go
│   ├── reviewer/
│   │   ├── invoker.go
│   │   └── verdict.go
│   └── testing/
│       ├── invoker.go
│       └── result.go
├── daemon/
│   ├── orchestrator_wiring.go          # NEW
│   └── dispatch_trigger.go             # MOD (consume new WIP helper)
├── cli/
│   └── workboard_card.go               # NEW
├── adapters/agent/
│   ├── hermes/hermes.go                # MOD (InjectTools)
│   ├── claudecode/                     # MOD (InjectTools)
│   └── codex/                          # MOD (InjectTools)
├── domain/
│   ├── workboard.go                    # MOD (chains, attempts)
│   └── projectconfig.go                # MOD (default chains)
├── httpd/controllers/
│   ├── workboard.go                    # MOD (orchestrator health, DTO)
│   └── dto.go                          # MOD
└── storage/sqlite/
    ├── migrations/<next>_*.sql         # NEW (3 migrations)
    ├── queries/workboard.sql           # MOD
    └── gen/                            # REGENERATED
```

---

# Out of scope

- Adaptive agent selection based on historical success (Phase B picks project-config priority; no learning yet).
- Cross-project agent pool sharing.
- Persistent agent chain per-task-type (e.g., different chains for `feat` vs `fix`).
- Changing the `dispatch_failed` semantics from the auto-dispatch plan.
- Changing the existing `ValidateWorkflowTransition` table; this plan adds drivers, not new transitions.
- Migrating `triage` / `backlog` / `scheduled` legacy cards (covered by the July 21 plan).
- Real-time bidirectional agent <-> daemon streaming beyond `Inject` for critical events.
- Web UI for editing agent chains (Phase 1 exposes data; UI editing is a separate task).
- Multi-Hermes orchestrator (per-project); each card gets exactly one Hermes session.

---

# Risks and decisions requiring implementation review

- **Hermes session lifetime.** A Hermes session must stay alive across PR creation, CI wait, and re-edit cycles. Confirm `--resume` semantics work after the briefing is updated; otherwise rebuild sessions per phase within a card.
- **Agent fallback could be expensive.** Falling back through 5 agents before Hermes may take long. Add a per-cycle timeout ceiling (default 60 min across all attempts) so cards do not stall indefinitely.
- **WIP counting changes user-visible behavior.** Cards in `review` now block new dispatch. Document this in the Director UI and in `docs/STATUS.md`.
- **PR detection by branch pattern.** Branch naming convention must be enforced when Hermes opens PRs (e.g., `ao/<card-id>-<slug>`); otherwise the daemon cannot link PR to card.
- **CI provider assumption.** Today the SCM wiring assumes GitHub-style checks. Other providers may need adapters before this plan ships end-to-end.
- **Concurrent reviewer/tester spawns.** Multiple cards in `running` may produce PRs simultaneously and need reviewer spawns. The orchestrator's WIP check prevents over-spawning, but the spawner itself must be safe under concurrent calls.
- **Hermes `Inject` reliability.** If Hermes cannot accept injected messages mid-run, fallback to `kill + resume` with updated briefing.