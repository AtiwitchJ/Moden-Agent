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

- Task 8 (commit `cfa8327a`): fixed Task 7's blocking bug — removed the
  redundant `InsertActiveSession` re-insert in `tick.go`/`orchestrator.go`,
  simplified `isSessionLive` to a pure session-age check.

## In Progress

- Task 9 (re-verify the loop against a real daemon after Task 8's fix): run,
  confirmed Task 7's immediate runaway is fixed, and found a **second,
  still-blocking bug** — see Known Issues. Not yet re-attempted after a fix.

## Planned

- Fix the `active_session` stale-row-across-phase-transition bug found by
  Task 9 (see Known Issues), then re-run the live smoke test to confirm the
  full lifecycle (coding→review→testing→done) is actually safe before
  calling it done.
- Tasks 7-9 of the *original* plan referenced by this branch's out-of-scope
  note (PR/CI watcher, reviewer invoker, testing invoker — automatic,
  signal-driven phase advancement) remain unbuilt. Today, phase advancement
  is agent-initiated only, via `ao workboard card transition` /
  `ao workboard card set-verdict` etc. — this is also *why* Task 9's bug is
  reachable: `OnAgentCompleted`/`OnAgentFailed` (which clean up
  `active_session`) are only driven by those unbuilt signal paths, not by
  the manual transition route.

## Known Issues

- **Blocking, found in Task 9's live re-verification (Task 8's fix does not
  cover this case)**: a card's `active_session` row from a completed phase
  is never cleaned up when the card advances via `ao workboard card
  transition`, because `DeleteActiveSession` is only called from
  `OnAgentCompleted`/`OnAgentFailed` (not reachable from that route — see
  Planned). `active_session.card_id` is a bare `PRIMARY KEY` (one row per
  card, not per card+phase) and `InsertActiveSession` is a plain `INSERT`,
  never an upsert. So the stale row survives into the new phase;
  `isSessionLive` (correctly age-based since Task 8) treats it as the new
  phase's live session for as long as it's within that phase's timeout
  (silently suppressing the correct spawn), and once the timeout elapses,
  every 30s tick performs a real spawn whose bookkeeping insert collides
  with the still-stale row and fails with `UNIQUE constraint failed:
  active_session.card_id` — forever, with no sign of self-resolving.
  Empirically: 4-5 additional real sessions spawned, one per tick, in the
  minutes after a `--to review` transition's stale coding-phase row timed
  out. Full repro and root cause: `.superpowers/sdd/task-9-report.md`.
  **The orchestrator wiring must not be considered verified/working until
  this is fixed and the live smoke test is re-run clean through the full
  lifecycle.**
- **Fixed (Task 8, commit `cfa8327a`)**: Task 7's original bug — the
  orchestrator's tick loop redundantly double-wrote `active_session` on
  every successful spawn (always failing the second write) and compared
  `isSessionLive` against a `WorkCard.SessionID` field the orchestrator's own
  spawns never updated, guaranteeing it could never recognize its own
  successful spawn as live. Produced 6 real sessions for one card within 12
  seconds in Task 7's test. Re-verified fixed in Task 9: session count held
  flat at 2 across 90+ seconds / 3 tick intervals for a card's first phase,
  zero `UNIQUE constraint failed` errors in that window.
- **Non-blocking, unrelated to this plan, noted but not investigated**:
  `ao project set-config --config-json` CLI command reports success but does
  not persist to the `projects.config` column; `PUT /api/v1/projects/{id}/config`
  over HTTP directly works. Pre-existing, out of scope here.
- The three `TestLifecycleDispatcherIsUnwired_*` tests in
  `internal/service/workboard/lifecycle_dispatcher_test.go` remain red by
  design (Tasks 7-9 of the *original*, larger plan — automatic phase
  advancement — are not part of this branch's scope).

## Verification

- Task 9 live smoke (`.superpowers/sdd/task-9-report.md`), re-run after
  Task 8's fix: isolated daemon (`AO_DATA_DIR`/`AO_RUN_FILE`/`AO_PORT` under
  a scratch dir, never `~/.ao`), fresh throwaway git repo project, `command`
  harness (inert, avoids runaway spend). Confirmed PASS: daemon boots with
  the orchestrator wired, no panics; Todo→Running auto-dispatch produces a
  real generated session id; `ao workboard card transition` updates card
  status end-to-end; Task 7's immediate coding-phase runaway is fixed (flat
  session count across 90+ seconds / 3 ticks, zero constraint errors).
  Confirmed FAIL: transitioning to `review` leaves the coding-phase
  `active_session` row in place, blocking/then breaking the review-phase
  spawn once its timeout elapses — an unbounded, real-session-spawning
  runaway reappears, gated by ~10 minutes instead of ~1 second. Testing/Done
  phases not exercised further (mechanism already proven). Daemon stopped
  and all 7 spawned tmux sessions/processes killed afterward; confirmed
  nothing under `~/.ao` was touched; no data committed outside this
  memory-bank/STATUS update.
- Task 7 live smoke (`.superpowers/sdd/task-7-report.md`), superseded by
  Task 9 above: found the original (now-fixed) immediate-runaway bug —
  6 real sessions for one card within 12 seconds.
