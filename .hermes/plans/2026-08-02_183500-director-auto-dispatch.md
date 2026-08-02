# Director Auto-dispatch Recovery Plan

> **For agentic workers:** implement in the listed order. Keep the daemon as the authority for all card transitions; the frontend only renders its facts and requests actions through the API.

**Goal:** Cards in **Todo** must automatically enter **Running** and be commanded by Hermes. Each project may run at most **4 cards** at once. When dispatch cannot start a card, Director must say why instead of leaving an apparently idle queue.

**Current facts:** `Dispatcher.DispatchOnce` already promotes `todo` to internal `ready`, claims cards atomically, and starts immediately when the daemon boots, then every minute. The default WIP is currently 3. A failed spawn returns from the whole dispatch pass, so one bad high-priority card can prevent later eligible cards from being considered until the next pass. The UI knows daemon readiness but only passes it to the focused card panel; the Director board itself does not explain an unavailable daemon or a failed dispatch.

## Locked decisions

- User-facing mode name is **Director**; keep route `/manage` and API/domain term `workboard` as compatibility names.
- `Todo` is an auto-start queue. Users do not need to drag it to `Running`.
- WIP is **4 per project**, not global. An explicit project WIP setting continues to override the default.
- A card that cannot start returns to Todo/Ready (the board renders it in Todo), never remains Running without a linked commander/worker session.
- Hermes-commanded projects keep their single Hermes commander. Four running cards means up to four active card claims, not four Hermes sessions.
- Store a card-level dispatch failure as an immutable work-card event. Do not add a second ad-hoc log database or let the frontend write SQLite.

## Task 1 — Align the WIP contract to four

**Files:**

- `backend/internal/domain/workboard.go`
- `backend/internal/domain/workboard_test.go`
- `frontend/src/renderer/lib/workboard-config.ts`
- affected project settings/create-project tests

- [x] Change `DefaultWorkboardConfig().WIPLimit` from 3 to 4.
- [x] Change the frontend default project config to `wipLimit: 4` so a newly created Hermes project persists the same value as the daemon default.
- [x] Keep projects with an existing explicit value unchanged; zero still means “use default”.
- [x] Update assertions that deliberately test defaults. Do not mass-rewrite fixtures whose WIP value exists only to test another behavior.

**Verification:** domain default test plus focused project configuration tests prove an unspecified project resolves to 4 and an explicit value still wins.

## Task 2 — Make Todo wake the dispatcher immediately

**Files:**

- `backend/internal/daemon/workboard_wiring.go`
- `backend/internal/service/workboard/service.go`
- `backend/internal/service/workboard/actions.go`
- `backend/internal/httpd/controllers/workboard.go` (only if a retry endpoint is required)
- relevant service/controller tests

- [x] Extract the existing per-project `DispatchOnce` call behind a small daemon-owned trigger with two entry points: the existing periodic poll and an in-process `Kick(projectID)`.
- [x] After a card is durably created or moved/returned to `todo`, call `Kick(projectID)` asynchronously. The database claim remains the only concurrency guard.
- [x] Add `POST /api/v1/projects/{projectId}/workboard/dispatch` as an explicit **Retry dispatch** action. It invokes the same trigger and returns the normal API error envelope; it must not spawn a worker directly from the controller.
- [x] Keep the immediate boot poll and the one-minute reconciliation tick as recovery mechanisms for missed events or a daemon restart.

**Acceptance:** creating a Todo card (or resolving a Redo back to Todo) attempts dispatch without waiting a minute. Repeated clicks, CDC delivery, and two app windows cannot exceed four because `ClaimReadyWorkCard` remains atomic.

## Task 3 — Let one failed card fail independently

**Files:**

- `backend/internal/service/workboard/dispatch.go`
- `backend/internal/service/workboard/dispatch_test.go`
- `backend/internal/storage/sqlite/store/workboard_store.go` if its existing event append capability needs exposing through the dispatch store interface

- [x] Replace the “return on first spawn error” loop behavior with per-card failure handling: release that card’s durable claim, append a `dispatch_failed` event with a safe reason and timestamp, then continue considering the remaining candidates.
- [x] Preserve fatal failures for project reads, card-list reads, or database claim/update failures; only an individual card’s inability to spawn is recoverable.
- [x] For a Hermes project, do not terminate or replace Hermes when dispatching a card fails. Return the card to Todo and record whether Hermes was unavailable, a non-Hermes orchestrator blocked it, or briefing/spawn failed.
- [x] If the WIP limit is reached, stop attempting additional candidates normally; this is queue pressure, not an error event.

**Tests:**

- five Todo cards on an otherwise healthy project claim exactly four and leave the fifth in Todo;
- one highest-priority card whose spawn fails returns to Todo with `dispatch_failed`, while later valid cards still start up to WIP;
- Hermes unavailable returns the card to Todo and never creates a direct worker;
- a Running card always has a linked session ID after a successful dispatch.

## Task 4 — Expose actionable dispatch history and status

**Files:**

- `backend/internal/httpd/controllers/dto.go`
- `backend/internal/httpd/controllers/workboard.go`
- `backend/internal/httpd/apispec/specgen/build.go`
- generated `backend/internal/httpd/apispec/openapi.yaml`
- generated `frontend/src/api/schema.ts`
- `frontend/src/renderer/hooks/useWorkboardQuery.ts`
- `frontend/src/renderer/components/WorkCardFocusPanel.tsx`

- [x] Add a read-only card event endpoint (or extend the existing card detail response) that returns the newest `dispatch_failed` event without exposing raw daemon internals or secrets.
- [x] Add a project Director status response: daemon readiness remains Electron-owned; the API returns `runningCount`, resolved `wipLimit`, and queued Todo count while the card failure endpoint supplies the latest attempt/result.
- [x] Generate the OpenAPI spec and frontend schema from these Go DTOs; never hand-edit generated output.
- [x] In the focus panel, show a compact status block for a Todo card that failed to start: reason, attempted time, and `Retry dispatch`.

**Acceptance:** a user can distinguish “waiting because 4/4 are running”, “daemon offline”, and “this card failed to start” without opening a terminal.

## Task 5 — Make Director report daemon health and logs honestly

**Files:**

- `frontend/src/renderer/components/Workboard.tsx`
- `frontend/src/renderer/components/Workboard.test.tsx`
- Electron daemon owner/launch files under `frontend/src/main.ts` and `frontend/src/shared/daemon-*.ts`
- daemon logging setup under `backend/internal/daemon/`

- [x] Render a narrow status line in the Director header using the existing `useDaemonStatus` / Shell context: `Auto-dispatch online · 2/4 running`, or an offline/error state with the daemon message.
- [x] Disable Retry while the daemon is not ready and state the remedy clearly: `Daemon offline — reconnect Modern Agent to resume Todo cards.`
- [x] Capture daemon stderr from the Electron-owned process to a bounded rotating file under `~/.ao/data/`; retain recent lines sufficient to diagnose a failed dispatch.
- [x] Provide a read-only “View daemon log” path in Director only when the daemon is unhealthy. Do not stream terminal output or reveal unrelated session logs by default.

**Acceptance:** `running.json` is never treated as proof that automation is healthy; the health/readiness status drives the UI.

## Task 6 — Documentation, integration checks, and manual smoke test

**Files:**

- `docs/` architecture/status notes that describe Director workflow
- `docs/superpowers/specs/code-redesign/code-manage.md` (Director design document; rename only in a separate documentation cleanup if desired)

- [x] Document the final state machine: `Todo → Running → Review → Testing → Done`, WIP=4 per project, and the retry/failure behavior.
- [x] Run `go test ./internal/service/workboard ./internal/daemon ./internal/httpd/...` followed by `go test ./...`.
- [x] Run `npm run api`, `npm run frontend:typecheck`, and focused tests for Workboard, WorkCardFocusPanel, daemon status, and project settings.
- [x] Smoke test in the desktop app: create five Todo cards for one Hermes project; verify four start, one waits, then finishes/retries a card and observes the waiting card start. Use `ao preview` from the affected session when demonstrating the frontend change.

### Smoke Test Steps

**Prerequisites:** Modern Agent desktop app running, daemon healthy (`/healthz` returns 200), at least one Hermes-configured project registered.

**Test 1 — Auto-dispatch fills WIP to 4 and holds the queue**

1. Open Director (`/manage`) for a Hermes project with no running cards.
2. Create five Todo cards in sequence (any title/notes; priority `normal`, agent `codex` or project default).
3. **Expected:** Within ~10 seconds, four cards transition to **Running** with linked session IDs. The fifth card remains in **Todo**.
4. Verify the Director header shows `Auto-dispatch online · 4/4 running`.

**Test 2 — Completing a card starts the queued card**

5. Mark one running card's session as done/terminated (e.g. via `Kill` in the session inspector, or let the agent complete naturally).
6. **Expected:** Within ~10 seconds, the queued Todo card auto-promotes to **Running** and the header updates to `4/4 running`.

**Test 3 — Dispatch failure records a `dispatch_failed` event without blocking**

7. Create a new Hermes project (or use an existing one) with WIP=1 for this test.
8. Create two Todo cards. Make the first card's agent one that will fail to spawn (e.g. a misconfigured harness ID), or temporarily break the runtime.
9. **Expected:** The first card returns to **Todo** with a `dispatch_failed` event visible in the focus panel. The second card still starts running up to WIP.
10. Focus the failed card: verify the UI shows the safe reason (`hermes_unavailable`, `non_hermes_orchestrator`, or `spawn_failed`), attempted time, and a **Retry dispatch** button.
11. After fixing the spawn condition, click **Retry dispatch**.
12. **Expected:** The card transitions to **Running**.

**Test 4 — Daemon offline disables retry and shows clear state**

13. Stop the daemon (close the app or `kill` the process).
14. In Director, observe the header: `Auto-dispatch is offline. Reconnect Modern Agent to resume Todo cards.`
15. **Expected:** The **Retry dispatch** button is disabled. No cards auto-transition while offline.

**Test 5 — Manual retry dispatches immediately**

16. Restart the daemon and reconnect.
17. Create a Todo card on a project with no running cards.
18. Click **Retry dispatch** on the card.
19. **Expected:** The card transitions to **Running** without waiting for the next 1-minute poll.

**Test 6 — Priority ordering**

20. With an empty project, create cards in this order: `low`, `urgent`, `normal`, `high`.
21. **Expected:** Cards start in priority order: `urgent`, `high`, `normal`, then `low` (until WIP=4 fills). FIFO breaks ties within the same priority.

**Test 7 — Non-Hermes orchestrator blocks dispatch with clear message**

22. On a Hermes project that has an active non-Hermes orchestrator session running, create a Todo card.
23. **Expected:** The card returns to **Todo** with reason `non_hermes_orchestrator`. Focus panel shows this reason clearly.

**Verification commands (alternative to UI):**

```bash
# Check daemon health
curl -s http://127.0.0.1:27151/healthz

# Check project Director status (once API is wired)
curl -s http://127.0.0.1:27151/api/v1/projects/{projectId}/workboard/status

# List cards for project
curl -s http://127.0.0.1:27151/api/v1/projects/{projectId}/workboard/cards | jq '.[] | {id, status, sessionId}'

# Get a card's dispatch failure event
curl -s http://127.0.0.1:27151/api/v1/workboard/cards/{cardId}/dispatch-failure

# Manually kick dispatch
curl -s -X POST http://127.0.0.1:27151/api/v1/projects/{projectId}/workboard/dispatch
```

## Out of scope

- Child-card schema or a separate worker-per-card UI hierarchy.
- Changing Review, Testing, or Redo gate semantics.
- Auto-restarting a crashed daemon from the renderer without verifying process ownership and `/healthz` identity.
- Deleting or forcibly terminating sessions to make WIP appear free.
