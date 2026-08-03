# Progress

## Completed

- Hermes Director Orchestrator wiring plan, Tasks 1-6: `OrchestratorWiring`
  test-panic fix, `getCard`/`OrchestratorStore.GetWorkCard` fix against real
  storage, `*sqlite.Store` `active_session` wrapper methods, a real
  session-spawning `AgentLauncher` (`SessionServiceLauncher`) replacing the
  fake-handle `RegistryLauncher`, the daemon constructing and wiring a real
  `orchestrator.New(...)` instead of `Orchestrator: nil`, and the
  `POST /workboard/cards/{cardId}/events` route (Task 10's server half)
  mounted so the `ao workboard card ...` CLI commands work.

## In Progress

- Task 7 (prove the loop end-to-end against a real daemon): run, and
  **blocked** — see Known Issues. Not yet re-attempted after a fix.

## Planned

- Fix the `active_session` double-insert / `WorkCard.SessionID` desync bug
  found by Task 7 (see Known Issues), then re-run Task 7's live smoke test
  to confirm the loop is actually safe before calling it done.
- Tasks 7-9 of the *original* plan referenced by this branch's out-of-scope
  note (PR/CI watcher, reviewer invoker, testing invoker — automatic,
  signal-driven phase advancement) remain unbuilt. Today, phase advancement
  is agent-initiated only, via `ao workboard card transition` /
  `ao workboard card set-verdict` etc. — confirmed working end-to-end in
  Task 7's live test.

## Known Issues

- **Blocking, found in Task 7's live verification**: the orchestrator's tick
  loop spawns a new real agent session for a card on every tick instead of
  recognizing its own already-spawned session as live, because (a)
  `commander/orchestrator/tick.go`'s `spawnSession` (and
  `orchestrator.go`'s `OnAgentFailed` fallback) redundantly calls
  `store.InsertActiveSession` a second time after `spawner.Spawn` already
  performed it — always failing on `active_session.card_id`'s bare
  `PRIMARY KEY` (no upsert) — and (b) the orchestrator never writes the
  spawned session id back onto `WorkCard.SessionID`, so `isSessionLive` can
  never match. Empirically: 6 real sessions spawned for one card within 12
  seconds in `running`, a 7th on transition to `review`; same helper is
  shared by the `testing` phase so it is presumed equally affected (not
  separately exercised, to avoid more leaked spawns). Full repro and root
  cause: `.superpowers/sdd/task-7-report.md`. **The orchestrator wiring must
  not be considered verified/working until this is fixed and Task 7 is
  re-run clean.**
- **Non-blocking, unrelated to this plan, noted but not investigated**:
  `ao project set-config --config-json` CLI command reports success but does
  not persist to the `projects.config` column; `PUT /api/v1/projects/{id}/config`
  over HTTP directly works. Pre-existing, out of scope here.
- The three `TestLifecycleDispatcherIsUnwired_*` tests in
  `internal/service/workboard/lifecycle_dispatcher_test.go` remain red by
  design (Tasks 7-9 of the *original*, larger plan — automatic phase
  advancement — are not part of this branch's scope).

## Verification

- Task 7 live smoke (`.superpowers/sdd/task-7-report.md`): isolated daemon
  (`AO_DATA_DIR`/`AO_RUN_FILE`/`AO_PORT` under a scratch dir, never
  `~/.ao`), throwaway git repo project, `command` harness (inert, avoids
  runaway spend against a real AI harness given the bug found). Confirmed
  PASS: daemon boots with the orchestrator wired, no panics; Todo→Running
  auto-dispatch produces a real generated session id (not the card id);
  `ao workboard card transition` works end-to-end. Confirmed FAIL: the
  orchestrator's own tick-driven spawn (both `coding` and `review` phases)
  reliably duplicates real sessions instead of settling — see Known Issues.
  Daemon stopped and all spawned tmux sessions/processes killed afterward;
  no data written outside the scratch dir.
