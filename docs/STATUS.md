# modern-agent status

Current `main` ships a working single-user local loop: the Go daemon and the
Electron/React frontend both drive a live daemon over HTTP/SSE/WebSocket. The
core GitHub flow works end-to-end: add project → spawn session/orchestrator →
attach terminal → observe PR → merge.

This file tracks progress. For what the product _is_ and how to run it, see the
top-level [`README.md`](../README.md); for the backend mental model see
[`architecture.md`](architecture.md).

## Build & test

The local gate is the backend Go build and race-enabled test suite:

```bash
cd backend && go build ./... && go test -race ./...
```

`npm run lint` (from the repo root) runs `go test ./...` plus golangci-lint.
Frontend checks live under `frontend/` (`npm run typecheck`, `npm run build`).
See [`AGENTS.md`](../AGENTS.md) for the regen workflow when touching the API
surface (`npm run sqlc`, `npm run api`).

## Shipped

### Backend (Go daemon)

- Loopback-only HTTP daemon (chi router, CORS, per-request timeout,
  `/healthz` / `/readyz` / `/shutdown`).
- SQLite store with goose migrations and sqlc-generated queries; DB
  trigger-based change-data-capture into `change_log`.
- CDC poller + broadcaster feeding in-process subscribers and the SSE stream
  at `GET /api/v1/events` (with `Last-Event-ID` replay).
- Full session lifecycle over HTTP: list, get, spawn, kill, restore, rename,
  rollback, cleanup, send, activity, PR claim/list. Orchestrator routes
  (list/spawn/get) are wired too.
- Project CRUD plus per-project config (`PUT /projects/{id}/config`).
- Director workboard auto-dispatch: per-project WIP limit default of 4, Todo →
  Running auto-start with asynchronous dispatch trigger, individual card spawn
  failures recorded as `dispatch_failed` events without blocking later eligible
  cards, and a manual `POST /projects/{id}/workboard/dispatch` retry action.
- Hermes Director orchestrator: the daemon constructs and wires a real
  orchestrator that subscribes to work-card CDC events and ticks every 30s,
  spawning a real agent session per phase through the session service.
  `POST /workboard/cards/{cardId}/events` is mounted, so the
  `ao workboard card set-verdict|set-finding|set-test-result|fail-attempt|transition`
  commands work end-to-end. Phase advancement is agent-initiated today via
  `ao workboard card transition` (and friends), not automatic — see "In
  flight" below for the not-yet-built automatic-advancement path. Verified
  safe under sustained live load (nine consecutive timeout-triggered session
  replacements, zero bookkeeping errors, every superseded session correctly
  terminated); see `memory-bank/progress.md` for the four respawn-collision/
  leak bugs found and fixed during verification, and one known, accepted
  limitation (a one-time, bounded double-spawn the first time a card enters
  `running`, from an unrelated pre-existing coordination gap between the
  auto-dispatcher above and the orchestrator).
- PR action engine wired into the API: `POST /prs/{id}/merge` and
  `/prs/{id}/resolve-comments`.
- Review routes registered: `GET /reviews`, `POST /reviews/execute`,
  `POST /reviews/{id}/send`.
- Durable dashboard notifications for `needs_input`, `ready_to_merge`,
  `pr_merged`, and `pr_closed_unmerged`: backend enrichment/persistence,
  unread list, live notification stream, and read acknowledgement API.
- SCM observer (`internal/observe/scm`) wired into the daemon: GitHub provider,
  lazy/non-blocking auth, per-PR polling with ETag guards and semantic diffing,
  feeding PR facts into lifecycle, which sends agent nudges for CI failures,
  review feedback, and merge conflicts
  ([#75](https://github.com/modernagent/modern-agent/issues/75),
  [#108](https://github.com/modernagent/modern-agent/issues/108),
  [#109](https://github.com/modernagent/modern-agent/issues/109)).
- Terminal mux over WebSocket (`/mux`): per-client `tmux attach` PTY on
  Darwin/Linux; conpty loopback pty-host on Windows.
- Lifecycle reducer plus reaper (`internal/observe/reaper`).
- Agent adapter platform under `internal/adapters/agent/` (23 adapters) with a
  registry and `ao hooks` activity dispatch.
- OpenAPI spec generated from Go DTOs; frontend TS types generated from it and
  drift-checked in CI.

### Frontend (Electron + React)

- Electron + React 19 + TanStack Router/Query + Tailwind + shadcn primitives.
- Real daemon wiring via the generated `openapi-fetch` typed client
  (`src/api/schema.ts`); mock data only in `VITE_NO_ELECTRON` web-preview mode.
- Electron main handles daemon discovery, launch, and status reporting.
- Shell: sidebar (projects + sessions, add/remove project), sessions board,
  session view + inspector, project settings, pull-requests page,
  spawn-orchestrator flow.
- Desktop status and SCM summary V1: session status comes from
  `GET /api/v1/sessions`; visible/active PR context comes from
  `GET /api/v1/sessions/{sessionId}/pr`; `GET /api/v1/events` is kept open as
  an invalidation stream rather than a full PR payload stream.
- Concise PR summaries include PR identity, CI state with failing check names
  and links, human reviewer IDs/counts/links for unresolved review comments,
  and mergeability reasons. Raw CI logs and review comment bodies are
  intentionally not part of the desktop V1 API/UI.
- Terminal pane (xterm) over the mux WebSocket, with a live SSE events
  connection and port-rebind on daemon restart.
- In-app notification center with unread catch-up over REST, live notification
  stream updates, explicit open-target actions, mark-read controls, and
  Electron app toasts while the app is running.

## In flight / not yet a runtime feature

- **Director Agent** (`feat/live-terminals`,
  `docs/superpowers/plans/2026-08-03-director-agent.md`): Tasks 1-12 are
  implemented. Its OpenAI, Anthropic, and OpenRouter model constructors are
  statically bundled, so it no longer depends on LangChain's dynamic provider
  import or the embedded flat `node_modules` archive at launch. Live provider
  verification remains pending.

- **Automatic, signal-driven phase advancement for the Hermes Director
  orchestrator** (PR/CI watcher, reviewer invoker, testing invoker): the
  orchestrator itself is shipped (see "Shipped" above), but today phase
  advancement is agent-initiated only, via `ao workboard card transition`
  and friends. The three `TestLifecycleDispatcherIsUnwired_*` tests in
  `internal/service/workboard/lifecycle_dispatcher_test.go` stay red by
  design until this lands.
- **Tracker lane**: GitHub tracker adapter exists, but there is no daemon
  observer loop or agent-lifecycle→issue mirroring yet, so the tracker does
  nothing at runtime ([#112](https://github.com/modernagent/modern-agent/issues/112)).
- **Full raw PR/tracker fact surfacing**: the SCM observer writes facts and the
  desktop consumes concise PR summaries, but exposing the full raw `pr_*` /
  `tracker_*` CDC events to live consumers
  ([#110](https://github.com/modernagent/modern-agent/issues/110)) and in
  `ao session get` ([#111](https://github.com/modernagent/modern-agent/issues/111))
  is still open.
- **CLI parity for PR/review actions**: merge, resolve-comments, and review are
  HTTP-only (frontend-driven); there are no `ao pr` / `ao review` commands.

## Planned (design + implementation plan ready, not started)

- **Hybrid approval gates for tracker-driven workflows** — closes the loop
  from `issue labeled agent-ready` through four sequential gates (CI → Agent
  review → Human approve → Agent final-pass) with a hybrid veto path that
  requires both a second-opinion agent and an explicit human confirmation to
  override a Gate-4 failure. Opt-in per project, every transition is a CDC
  event, every human override is auditable.
  - Design: [`docs/superpowers/specs/2026-07-14-hybrid-approval-gates-design.md`](superpowers/specs/2026-07-14-hybrid-approval-gates-design.md)
  - Plan: [`docs/superpowers/plans/2026-07-14-hybrid-approval-gates.md`](superpowers/plans/2026-07-14-hybrid-approval-gates.md)
  - Closes: #112 (tracker loop), #110/#111 (raw events), CLI parity gap
  - Phase: Phase 1 of a 3-phase rollout (tracker + gates → trust ladder → org defaults)

- **Worker pool (PM-orchestrated, subagent-specialized)** — extends the
  Hybrid Gates design with a level-3 execution layer where workers are
  subagents with specialties (BE/FE/DB/Test/Sec/Docs/Perf/Refactor) reused
  across jobs. Per-project pools for isolation, CEO sees aggregate
  capacity/cost. Mesh sync between workers reuses existing CDC events.
  Pool auto-collects success-rate/cost/time telemetry; CEO sets per-specialty
  trust tier (trusted/experimental/banned). Opt-in per project, pool size
  config-driven, no auto-scaling in Phase 1.
  - Included in: [`docs/superpowers/specs/2026-07-14-hybrid-approval-gates-design.md` §11-12](superpowers/specs/2026-07-14-hybrid-approval-gates-design.md) (top-down architecture + worker pool architecture)
  - Implementation: [`docs/superpowers/plans/2026-07-14-hybrid-approval-gates.md` Tasks 18-21](superpowers/plans/2026-07-14-hybrid-approval-gates.md) (pool registry, telemetry, dispatcher, CLI)
  - Phase: Phase 1 of Hybrid Gates rollout (5–6 months total)

Tracking milestone:
[`rewrite`](https://github.com/modernagent/modern-agent/milestone/1).
