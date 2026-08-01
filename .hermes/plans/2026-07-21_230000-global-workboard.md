# Global Workboard Implementation Plan

> **For Hermes:** Use subagent-driven-development skill to implement this plan task-by-task.

**Goal:** Make a single global Workboard the application home, where every new Work Card must be assigned to a user-selected existing Project, enters `Todo`, and is autonomously processed through Coding → Review → Testing → Done with unlimited, numbered Redo cycles.

**Architecture:** Keep the daemon as the source of truth for cards, workflow transitions, project paths, agent configuration, attempts, findings, and audit events. Add a global Workboard read/create surface while preserving project-scoped APIs as compatibility boundaries where useful. The frontend becomes a global Kanban home; Project/Path and agent selection are explicit in the create dialog, and cards display project, path, status, priority, assigned agents/session, latest update, and Redo progress. Workflow transitions are server-authoritative and idempotent; agents emit structured findings and the daemon advances cards only after the relevant gate passes.

**Tech Stack:** Go daemon, SQLite/sqlc/goose, chi HTTP controllers, OpenAPI-generated TypeScript, React 19, TanStack Router/Query, React Query mutations, existing agent/session manager and Workboard UI.

---

## Product contract (locked decisions)

### Global home

- The root authenticated route renders the **global Workboard**, not CEO Dashboard and not the last project board.
- The global board includes cards from all Projects.
- A project filter/search may be added, but it must not replace the global default.
- Existing Project Board remains available for project-scoped inspection/configuration unless implementation proves it should become a filtered view of the global board.

### Creating a Work Card

- `New Work` opens a dialog.
- Title is required.
- Project is required and must be selected by the User; no last-project/default-project auto-selection.
- The selected Project's persisted Path is shown read-only for confirmation.
- Create is clickable without a Project, but submission returns inline validation: `กรุณาเลือก Project ของงานนี้` (or the app's established English equivalent if UI copy is standardized elsewhere).
- Do not create an unassigned card.
- Do not silently create a Project.
- New cards enter `Todo` immediately.
- Agent selection supports all of:
  1. User-selected Coding Agent at creation;
  2. User selection when the card is about to start, if not selected at creation;
  3. Project default;
  4. Automatically selected available Agent.
- If no Agent is available, card remains `Todo` with a structured start error.

### Workflow

```text
New Work → Todo → Running → Review → Testing → Done
                         ↘ error → Redo #N → Todo → Running
```

- Remove `Triage` from the user-facing workflow and default new-card state. Existing legacy/ingested triage cards require an explicit migration/normalization policy; do not silently reinterpret them without tests.
- Agent/daemon controls transitions; User does not manually drag cards between workflow states in the global board.
- `Todo` starts Coding Agent automatically.
- Successful coding completion moves to `Review`.
- Review uses either a separate Reviewer Agent or the Coding Agent, configured per Project.
- Passing Review moves to `Testing`.
- Testing must run `test + lint + build`.
- Passing all Testing gates moves to `Done`.
- Any Review/Testing/start failure creates a new Redo cycle and returns the card to `Todo` only after the cycle's findings are resolved.

### Redo

- Redo cycles are unlimited and numbered monotonically: `Redo #1`, `Redo #2`, etc.
- Never overwrite prior Redo history.
- Each Redo stores source Agent, summary, priority-ranked findings, command that failed, error output, file/line references, timestamps, and attempt history.
- Findings are processed one at a time in `Critical → High → Normal → Low` order.
- The current finding must pass focused validation before the next finding starts.
- If the same finding fails again, repeat attempts on that finding until it passes; show `Attempt N`.
- After all findings pass, return to `Todo` and restart the normal workflow.
- Reviewer and Testing findings use the same structured shape. Testing additionally records failed command(s), raw output, and affected files.

### Agent configuration

- Project configuration supports Coding Agent, Review mode (`same` or `separate`), Reviewer Agent, Testing Agent, and defaults/fallback behavior.
- Separate Reviewer Agent is supported; same-agent mode remains supported.
- Testing Agent is independently configurable, with fallback to Project default/available Agent according to the same safe resolution rules.
- No hardcoded agent fixture names or fake successful results in source.

---

## Current repository context

- Existing backend Workboard domain/API already has project-scoped cards and statuses including `triage`, `backlog`, `todo`, `scheduled`, `ready`, `running`, `review`, `blocked`, and `done`.
- Existing frontend `Workboard.tsx` renders a project-scoped nine-column board and currently permits drag/move through the API.
- Existing root route `frontend/src/renderer/routes/_shell.index.tsx` redirects only to `ao.lastProjectId`; this is the direct reason the first Workboard is not visible on first launch.
- Existing `ProjectBoard.tsx` writes `ao.lastProjectId` and mounts Workboard.
- Existing API/controller/service/storage seams must be extended rather than bypassed.
- Existing tests currently show a mismatch: `frontend/src/renderer/lib/workboard-home.test.ts` expects a payload without `sessionId`, while the implementation returns it. Fix/update this as part of the relevant test task; do not ignore a red test.

---

# Phase 0 — Baseline and contract tests

### Task 1: Capture current behavior and establish a red global-home test

**Objective:** Prove the current root route is project/CEO-dashboard oriented and define the new root behavior before implementation.

**Files:**
- Test/modify: `frontend/src/renderer/lib/workboard-home.test.ts`
- Modify: `frontend/src/renderer/lib/workboard-home.ts`
- Inspect: `frontend/src/renderer/routes/_shell.index.tsx`

**Steps:**
1. Add tests for `globalWorkboardRedirectTarget()` (or equivalent): always returns `/workboard`/the chosen global route, independent of `ao.lastProjectId`.
2. Add tests that an existing last-project value does not redirect the root to a project board.
3. Correct the existing `sessionId` expectation mismatch in the same test file if the current product behavior still requires session linking.
4. Run the focused Vitest test and record the expected red state before route changes.

**Verification:** `cd frontend && npm test -- --run src/renderer/lib/workboard-home.test.ts`.

### Task 2: Define the global API contract before storage changes

**Objective:** Add explicit request/response shapes for global card listing and create operations, including project identity/path and workflow metadata.

**Files:**
- Modify: `backend/internal/httpd/controllers/dto.go`
- Modify: `backend/internal/httpd/apispec/specgen/build.go`
- Regenerate: `backend/internal/httpd/apispec/openapi.yaml`, `frontend/src/api/schema.ts`
- Add/modify tests: `backend/internal/httpd/controllers/*workboard*_test.go`

**Contract requirements:**
- `GET /api/v1/workboard/cards` returns all cards visible to the current organization/user, with Project name and Path hydrated or represented by stable IDs plus a reliable project lookup.
- `POST /api/v1/workboard/cards` requires `projectId` and title; accepts optional priority, notes, labels, coding agent, reviewer mode/agent, testing agent.
- Missing/blank `projectId` returns a validation error; no card is written.
- New card status is `todo`, not `triage`.
- Response includes `redoCount`/latest cycle summary, workflow agents, session ID, updated timestamp, and project metadata needed by the card UI.

**Verification:** controller tests assert HTTP status, error envelope, and response shape; run `npm run api` and `cd backend && go test ./internal/httpd/...`.

---

# Phase 1 — Domain, schema, and service invariants

### Task 3: Replace user-facing legacy statuses with the locked workflow model

**Objective:** Make the domain represent `todo`, `running`, `review`, `testing`, `redo`, and `done` with explicit transition validation.

**Files:**
- Modify: `backend/internal/domain/workboard.go`
- Add/modify: `backend/internal/domain/workboard_test.go`
- Modify: `backend/internal/storage/sqlite/migrations/<next_workboard_migration>.sql`
- Modify: `backend/internal/storage/sqlite/queries/workboard.sql`
- Regenerate: `backend/internal/storage/sqlite/gen/*` via `npm run sqlc`

**Implementation details:**
- Add `CardStatusTesting` and `CardStatusRedo`.
- Decide compatibility handling for existing statuses: preserve historical values for reads/migration, but reject new user-created `triage`, `backlog`, `scheduled`, `ready`, and manual `blocked` transitions unless an explicit legacy path requires them.
- Add a transition function such as `ValidateWorkflowTransition(from, to, actor)`; it must reject User drag attempts that bypass daemon/agent transitions.
- New-card defaults must be `todo`.
- Add durable `redo_count` and latest-cycle reference rather than deriving the count from a lossy status field.

**Tests:** valid/invalid transitions, new-card defaults, monotonic Redo numbering, no accidental reset on reload.

### Task 4: Add durable Redo cycle, finding, and attempt entities

**Objective:** Store every Redo cycle and every finding/attempt without overwriting previous history.

**Files:**
- Create/modify: `backend/internal/storage/sqlite/migrations/<next_workboard_migration>.sql`
- Modify: `backend/internal/storage/sqlite/queries/workboard.sql`
- Modify: `backend/internal/domain/workboard.go`
- Create/modify: `backend/internal/storage/sqlite/store/workboard_store.go`
- Tests: `backend/internal/storage/sqlite/store/*workboard*_test.go`

**Suggested tables/fields:**
- `work_card_redo_cycles`: `id`, `card_id`, `cycle_number`, `source` (`review`/`testing`/`start`), `summary`, `created_at`, `completed_at`.
- `work_card_findings`: `id`, `cycle_id`, `sequence`, `severity`, `title`, `details`, `command`, `error_output`, `file_refs_json`, `status`, `attempt_count`, `created_at`, `updated_at`.
- `work_card_attempts`: `id`, `finding_id`, `attempt_number`, `agent`, `started_at`, `finished_at`, `result`, `output`, `validation_json`.
- Unique `(card_id, cycle_number)` and unique `(cycle_id, sequence)` constraints.

**Verification:** storage tests prove cycle #1/#2 are both readable, finding order is stable, attempts append, and duplicate cycle creation cannot silently overwrite data.

### Task 5: Extend Work Card aggregate with project and workflow metadata

**Objective:** Make service/API reads provide all fields required by the global card and agents.

**Files:**
- Modify: `backend/internal/domain/workboard.go`
- Modify: `backend/internal/service/workboard/service.go`
- Modify: `backend/internal/httpd/controllers/dto.go`
- Modify: `backend/internal/httpd/controllers/workboard.go`
- Tests: `backend/internal/service/workboard/service_test.go`

**Required fields:** project ID/name/path, priority, current status, coding/reviewer/testing agent assignment, session/terminal ID, latest update, Redo count/latest cycle, current finding index/attempt, and structured error summary.

**Validation:** Project must exist and path must be read from the Project record; client-supplied path must not become authoritative.

---

# Phase 2 — Global API and project/agent selection

### Task 6: Implement global list and create service methods

**Objective:** Add service methods that list cards across projects and create a Todo card only with a valid Project.

**Files:**
- Modify: `backend/internal/service/workboard/service.go`
- Add/modify: `backend/internal/service/workboard/service_test.go`
- Modify: `backend/internal/httpd/controllers/workboard.go`
- Modify: router registration file where controllers are mounted

**Behavior:**
- Global list returns cards from all projects, stable ordering by status/position/updated time.
- Create validates title and `projectId` before any store write.
- Missing project returns 4xx with the established API error envelope.
- Create writes status `todo`, position, selected optional agent config, and an audit event.
- No global create may accept arbitrary `targetPath` as a substitute for Project.

**Verification:** service/controller tests cover successful create, missing project, deleted project, blank title, and response metadata.

### Task 7: Add agent configuration and resolution policy

**Objective:** Persist per-card/per-project Coding, Reviewer, and Testing Agent preferences and resolve fallbacks deterministically.

**Files:**
- Modify: `backend/internal/domain/projectconfig.go` and/or `backend/internal/domain/workboard.go`
- Modify: project service/config DTOs
- Modify: `backend/internal/service/workboard/*`
- Modify: project settings controller/DTO/API spec
- Tests: service and controller tests

**Resolution order:**
- Card explicit selection → selection captured at Todo start → Project configured default → available registered Agent.
- For Review: separate Reviewer Agent when configured; otherwise same Coding Agent when Project mode is `same`; otherwise safe fallback.
- For Testing: explicit Testing Agent → Project default → available Agent.
- If none is available, return a structured `NO_AGENT_AVAILABLE` failure and keep the card in `todo` or `redo` according to phase.

**No fixture rule:** enumerate registered/available agents from the existing registry/service; never hardcode fake agent names or use random success.

### Task 8: Add agent configuration UI and create-dialog validation

**Objective:** Let User choose Project and optional agents while enforcing Project selection.

**Files:**
- Modify: `frontend/src/renderer/components/CreateWorkCardDialog.tsx`
- Modify/create: `frontend/src/renderer/components/WorkboardProjectPicker.tsx`
- Modify/create: `frontend/src/renderer/components/WorkboardAgentPicker.tsx`
- Modify: `frontend/src/renderer/components/Workboard.tsx`
- Tests: `frontend/src/renderer/components/*workboard*.test.tsx`, relevant lib tests

**UI behavior:**
- Global dialog loads Projects and displays each Project's persisted Path read-only.
- Project has no default selection.
- Create button submits and shows inline validation if Project is absent.
- Optional agent fields support Coding, Reviewer mode, Reviewer, Testing.
- Successful create invalidates global cards query and closes dialog.
- Error responses are rendered, not swallowed.

**Verification:** component tests assert no API call/no card when Project is missing, correct payload when selected, and selected path display.

---

# Phase 3 — Global Workboard frontend

### Task 9: Add the global route and mount it as the application home

**Objective:** Ensure the first screen is the global Workboard on every launch.

**Files:**
- Create/modify: `frontend/src/renderer/routes/_shell.workboard.tsx` or the repository's chosen global route path
- Modify: `frontend/src/renderer/routes/_shell.index.tsx`
- Modify: route tree generation file if required by TanStack Router
- Modify: `frontend/src/renderer/lib/workboard-home.ts`
- Tests: route/home redirect tests

**Rules:**
- Root route redirects to the global Workboard route, not `ao.lastProjectId`.
- Remove or repurpose last-project redirect logic; retaining the local-storage write is allowed only for optional convenience, never as the global-home decision.
- Direct navigation to `/workboard` works after reload.

**Verification:** frontend route tests plus MCP browser check of the actual Electron/web surface; verify no CEO Dashboard flash is visible before redirect if possible.

### Task 10: Convert Workboard to global data and locked columns

**Objective:** Render one board for every Project and remove user-controlled workflow dragging.

**Files:**
- Modify: `frontend/src/renderer/components/Workboard.tsx`
- Modify: `frontend/src/renderer/hooks/useWorkboardQuery.ts`
- Modify: `frontend/src/renderer/components/WorkCard.tsx`
- Add/modify: global Workboard component tests

**Columns:**
- `Todo`, `Running`, `Review`, `Testing`, `Redo`, `Done`.
- No visible `Triage`, `Backlog`, `Scheduled`, `Ready`, or generic `Blocked` column for new workflow cards.
- Remove/drop-disable manual drag for agent-controlled transitions; display processing state and disabled movement affordance instead.
- Add Project filter/search as a view-only filter if useful; global view remains default.

**Card display:** title, Project, Path, status, priority, Coding/Reviewer/Testing Agent, Session/Terminal, latest update, `Redo #N`, current finding progress and attempt.

### Task 11: Add Redo detail and attempt UI

**Objective:** Make Redo work understandable and auditable from the card.

**Files:**
- Modify/create: `frontend/src/renderer/components/WorkCardFocusPanel.tsx`
- Modify/create: `frontend/src/renderer/components/RedoCyclePanel.tsx`
- Modify/create: `frontend/src/renderer/components/RedoFindingList.tsx`
- Tests: frontend component tests

**UI:**
- Show all cycles, never only the latest.
- Show current cycle number, source Agent, summary, ordered findings, severity, current finding, attempt number, commands, output, files/lines.
- Clearly distinguish `Pending`, `In Progress`, `Fixed`, `Failed`.
- Preserve accessible labels and keyboard navigation.

---

# Phase 4 — Daemon workflow orchestration

### Task 12: Implement Todo claim/start and Coding Agent completion

**Objective:** Automatically start eligible Todo cards and move successful cards to Review.

**Files:**
- Modify/create: `backend/internal/service/workboard/dispatch.go`
- Modify: existing observer/dispatcher integration under `backend/internal/observe/` and `backend/internal/session_manager/`
- Modify: `backend/internal/service/workboard/service.go`
- Tests: dispatch/service integration tests with fakes

**Rules:**
- Claim is atomic and idempotent; no double-start on repeated CDC/tick events.
- Resolve Coding Agent using the locked fallback order.
- Persist session ID and agent assignment before/with launch result.
- Start failure creates Redo cycle or structured Todo error according to the phase contract; never falsely mark Running.
- Successful coding completion advances to `review` and emits an audit event.

### Task 13: Implement Reviewer Agent gate

**Objective:** Run Review using separate or same Agent according to Project config and produce structured findings.

**Files:**
- Modify/create: `backend/internal/service/workboard/review.go`
- Modify: agent adapter/dispatch boundary under `backend/internal/terminal/agent/` or existing service seam
- Tests: reviewer orchestration tests

**Rules:**
- Reviewer receives the card's Project Path and relevant diff/session context.
- Pass → `testing`.
- Fail → create numbered Redo cycle with priority-ranked findings and return to `redo`.
- Findings must identify title/details/severity/file-line references; no free-form-only result is accepted.
- Same-Agent mode must be explicit and tested.

### Task 14: Implement Testing Agent gate

**Objective:** Run `test + lint + build`, record every result, and advance or create Redo.

**Files:**
- Modify/create: `backend/internal/service/workboard/testing.go`
- Modify: command execution boundary using existing workspace/runtime abstractions
- Tests: testing orchestration tests with fake command runner

**Rules:**
- Commands are resolved from Project/repository configuration; never hardcode a universal command that ignores the repository.
- Required gate categories are test, lint, build.
- Persist command, exit code, stdout/stderr, timing, and affected file references where available.
- All pass → `done`.
- Any fail → structured Testing findings, new Redo cycle, and card returns to `redo`.

### Task 15: Implement Redo finding loop and unlimited cycle counter

**Objective:** Let Coding Agent fix one finding at a time, retry the same finding until focused validation passes, then restart the workflow.

**Files:**
- Modify/create: `backend/internal/service/workboard/redo.go`
- Modify: dispatch integration
- Tests: Redo state-machine tests

**State machine:**
1. Enter `redo`, atomically create `cycle_number = previous + 1`.
2. Select the highest-priority pending finding.
3. Set it `in_progress`, increment/persist attempt number.
4. Launch Coding Agent with only the current finding as the immediate objective plus full Project Path/context.
5. Run focused validation chosen by finding type.
6. Failure: append attempt output and repeat same finding.
7. Success: mark finding fixed and continue to next finding.
8. All findings fixed: set `todo`, emit event, let normal Todo dispatcher restart.

**Safety:** no cycle limit; protect against duplicate workers with durable leases/claims and idempotency keys, not an arbitrary retry cap.

### Task 16: Add event/audit and recovery behavior

**Objective:** Make workflow transitions observable and recoverable after daemon restart.

**Files:**
- Modify: change-log/event integration and Workboard store
- Modify: dispatcher startup/reconciliation path
- Tests: restart/recovery integration tests

**Verification:** restart with cards in every active state; reconcile sessions/leases without duplicating work, losing Redo history, or resetting counters.

---

# Phase 5 — Migration, compatibility, and documentation

### Task 17: Migrate existing cards and define legacy status behavior

**Objective:** Prevent old `triage` cards and old project-scoped cards from disappearing or entering an invalid state.

**Files:**
- Create: next SQLite migration under `backend/internal/storage/sqlite/migrations/`
- Modify: migration tests and Workboard service compatibility logic
- Modify: docs/status/design docs as appropriate

**Migration policy to implement and document:**
- Existing cards remain queryable.
- Existing `triage` cards are either explicitly migrated to `todo` with an audit event or remain legacy-only and are surfaced with a clear migration state; choose one and test it before coding.
- Existing cards with active sessions must not be moved blindly.
- Existing project-scoped endpoints remain compatible or return a clear deprecation response.

### Task 18: Update docs and generated artifacts

**Objective:** Document the final global workflow and keep generated API artifacts synchronized.

**Files:**
- Modify: `README.md` or relevant user guide
- Modify: `docs/STATUS.md` and Workboard design/spec document
- Regenerate: `backend/internal/httpd/apispec/openapi.yaml`, `frontend/src/api/schema.ts`

Document: global home, mandatory Project selection, agent fallback, workflow gates, Redo semantics, unlimited attempts, and no-manual-transition rule.

---

# Verification plan

## Focused backend checks

```bash
cd backend
go test ./internal/domain/... ./internal/service/workboard/... ./internal/httpd/controllers/...
go test -race ./internal/service/workboard/...
go vet ./...
```

## Focused frontend checks

```bash
cd frontend
npm test -- --run src/renderer/lib/workboard-home.test.ts
npm test -- --run src/renderer/components
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

1. Fresh launch with no `ao.lastProjectId` opens Global Workboard, not CEO Dashboard.
2. Global list displays cards from at least two Projects, including Project name and persisted Path.
3. Create with no Project: submit is accepted by the UI but shows validation error and creates no card.
4. Create with Project: card is created as `Todo`; no `Triage` card is produced.
5. Explicit Coding Agent is honored; absent selection follows start-time/project/available fallback order.
6. Todo automatically starts Coding Agent; successful coding enters Review.
7. Separate Reviewer Agent passes → Testing; fails → Redo #1 with structured findings.
8. Redo fixes Critical before High, retries a failed finding in place, and displays Attempt N.
9. Testing runs test + lint + build; one failure creates the next Redo cycle with command/output/files.
10. All Testing gates pass → Done.
11. Daemon restart preserves card state, cycle count, findings, attempts, and session identity.
12. User drag/drop cannot bypass or manually advance agent-controlled statuses.

## Browser verification

Use MCP Chrome DevTools against the actual running frontend after rebuild:

1. Navigate with a cache-busting query.
2. Snapshot the root route and verify `Workboard` is visible.
3. Open New Work; verify Project has no default selection and Path appears only after selection.
4. Submit without Project; verify validation error and no card.
5. Select a real Project, create a card, and verify it appears in `Todo` with project/path metadata.
6. Inspect console/network for failed API calls.
7. Take a screenshot of the final global board.

If the frontend is served by Vite on macOS, verify its bind address and use `--host 127.0.0.1` when IPv4-based browser/API checks are required; verify the loaded bundle hash after rebuild to avoid stale UI conclusions.

---

# Risks and decisions requiring implementation review

- **Existing Workboard already has broad OpenClaw columns and manual drag.** The implementation must migrate deliberately; merely hiding columns would leave invalid backend transitions.
- **Agent orchestration may overlap existing dispatcher/intake behavior.** Reuse existing session/adapter boundaries and add idempotent claims; do not create a second competing scheduler.
- **Project Path authority.** Always resolve Path server-side from Project; never trust a card's arbitrary client path.
- **Unlimited Redo.** Unlimited cycles must not mean unbounded duplicate processes. Use durable claims/leases and append-only attempts.
- **Testing commands vary by repository.** Store/configure commands per Project or detect them through the existing workspace abstraction; do not invent hardcoded fixtures.
- **Global authorization/query scope.** Confirm the existing API's organization/user boundary before exposing all projects through one endpoint.
- **CEO Dashboard remains reachable.** Make it a secondary route, not the default home, unless product requirements later remove it.
- **No hardcoded data.** No fake Projects, Agent names, paths, sensor fixtures, random success, or synthetic command output in production code.

---

# Definition of Done

- [ ] Global Workboard is the first visible page on fresh launch.
- [ ] Global API lists all authorized Projects' cards.
- [ ] New Work requires explicit Project selection and starts in Todo.
- [ ] Card shows all required Project/Path/agent/session/status/priority/update/Redo metadata.
- [ ] Agent-controlled state machine is enforced server-side.
- [ ] Coding, separate/same Review, Testing, and Done gates work with real agent/session boundaries.
- [ ] Testing requires test + lint + build and stores evidence.
- [ ] Redo is unlimited, numbered, append-only, priority ordered, and retries current finding until it passes.
- [ ] Existing data is migrated or surfaced by a tested compatibility policy.
- [ ] Backend tests, frontend tests, typecheck, build, API generation, and MCP browser verification pass.
- [ ] Documentation and generated artifacts are synchronized.
