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

- [ ] Change `DefaultWorkboardConfig().WIPLimit` from 3 to 4.
- [ ] Change the frontend default project config to `wipLimit: 4` so a newly created Hermes project persists the same value as the daemon default.
- [ ] Keep projects with an existing explicit value unchanged; zero still means “use default”.
- [ ] Update assertions that deliberately test defaults. Do not mass-rewrite fixtures whose WIP value exists only to test another behavior.

**Verification:** domain default test plus focused project configuration tests prove an unspecified project resolves to 4 and an explicit value still wins.

## Task 2 — Make Todo wake the dispatcher immediately

**Files:**

- `backend/internal/daemon/workboard_wiring.go`
- `backend/internal/service/workboard/service.go`
- `backend/internal/service/workboard/actions.go`
- `backend/internal/httpd/controllers/workboard.go` (only if a retry endpoint is required)
- relevant service/controller tests

- [ ] Extract the existing per-project `DispatchOnce` call behind a small daemon-owned trigger with two entry points: the existing periodic poll and an in-process `Kick(projectID)`.
- [ ] After a card is durably created or moved/returned to `todo`, call `Kick(projectID)` asynchronously. The database claim remains the only concurrency guard.
- [ ] Add `POST /api/v1/projects/{projectId}/workboard/dispatch` as an explicit **Retry dispatch** action. It invokes the same trigger and returns the normal API error envelope; it must not spawn a worker directly from the controller.
- [ ] Keep the immediate boot poll and the one-minute reconciliation tick as recovery mechanisms for missed events or a daemon restart.

**Acceptance:** creating a Todo card (or resolving a Redo back to Todo) attempts dispatch without waiting a minute. Repeated clicks, CDC delivery, and two app windows cannot exceed four because `ClaimReadyWorkCard` remains atomic.

## Task 3 — Let one failed card fail independently

**Files:**

- `backend/internal/service/workboard/dispatch.go`
- `backend/internal/service/workboard/dispatch_test.go`
- `backend/internal/storage/sqlite/store/workboard_store.go` if its existing event append capability needs exposing through the dispatch store interface

- [ ] Replace the “return on first spawn error” loop behavior with per-card failure handling: release that card’s durable claim, append a `dispatch_failed` event with a safe reason and timestamp, then continue considering the remaining candidates.
- [ ] Preserve fatal failures for project reads, card-list reads, or database claim/update failures; only an individual card’s inability to spawn is recoverable.
- [ ] For a Hermes project, do not terminate or replace Hermes when dispatching a card fails. Return the card to Todo and record whether Hermes was unavailable, a non-Hermes orchestrator blocked it, or briefing/spawn failed.
- [ ] If the WIP limit is reached, stop attempting additional candidates normally; this is queue pressure, not an error event.

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

- [ ] Add a read-only card event endpoint (or extend the existing card detail response) that returns the newest `dispatch_failed` event without exposing raw daemon internals or secrets.
- [ ] Add a project Director status response: daemon readiness, `runningCount`, resolved `wipLimit`, queued Todo count, and the most recent dispatch attempt/result held by the daemon process.
- [ ] Generate the OpenAPI spec and frontend schema from these Go DTOs; never hand-edit generated output.
- [ ] In the focus panel, show a compact status block for a Todo card that failed to start: reason, attempted time, and `Retry dispatch`.

**Acceptance:** a user can distinguish “waiting because 4/4 are running”, “daemon offline”, and “this card failed to start” without opening a terminal.

## Task 5 — Make Director report daemon health and logs honestly

**Files:**

- `frontend/src/renderer/components/Workboard.tsx`
- `frontend/src/renderer/components/Workboard.test.tsx`
- Electron daemon owner/launch files under `frontend/src/main.ts` and `frontend/src/shared/daemon-*.ts`
- daemon logging setup under `backend/internal/daemon/`

- [ ] Render a narrow status line in the Director header using the existing `useDaemonStatus` / Shell context: `Auto-dispatch online · 2/4 running`, or an offline/error state with the daemon message.
- [ ] Disable Retry while the daemon is not ready and state the remedy clearly: `Daemon offline — reconnect Modern Agent to resume Todo cards.`
- [ ] Capture daemon stderr from the Electron-owned process to a bounded rotating file under `~/.ao/data/`; retain recent lines sufficient to diagnose a failed dispatch.
- [ ] Provide a read-only “View daemon log” path in Director only when the daemon is unhealthy. Do not stream terminal output or reveal unrelated session logs by default.

**Acceptance:** `running.json` is never treated as proof that automation is healthy; the health/readiness status drives the UI.

## Task 6 — Documentation, integration checks, and manual smoke test

**Files:**

- `docs/` architecture/status notes that describe Director workflow
- `docs/superpowers/specs/code-redesign/code-manage.md` (Director design document; rename only in a separate documentation cleanup if desired)

- [ ] Document the final state machine: `Todo → Running → Review → Testing → Done`, WIP=4 per project, and the retry/failure behavior.
- [ ] Run `go test ./internal/service/workboard ./internal/daemon ./internal/httpd/...` followed by `go test ./...`.
- [ ] Run `npm run api`, `npm run frontend:typecheck`, and focused tests for Workboard, WorkCardFocusPanel, daemon status, and project settings.
- [ ] Smoke test in the desktop app: create five Todo cards for one Hermes project; verify four start, one waits, then finishes/retries a card and observes the waiting card start. Use `ao preview` from the affected session when demonstrating the frontend change.

## Out of scope

- Child-card schema or a separate worker-per-card UI hierarchy.
- Changing Review, Testing, or Redo gate semantics.
- Auto-restarting a crashed daemon from the renderer without verifying process ownership and `/healthz` identity.
- Deleting or forcibly terminating sessions to make WIP appear free.
