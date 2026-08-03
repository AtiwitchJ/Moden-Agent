# Active Context

## Current Focus

Hermes Director Orchestrator: Last-Mile Wiring
(`docs/superpowers/plans/2026-08-03-hermes-orchestrator-wiring.md`). Tasks
1-6 (test fix, `getCard` fix, `active_session` store, real launcher, daemon
wiring, agent-events route) are implemented and reviewed clean. Task 7 (prove
the loop against a real running daemon) ran and found a blocking bug in the
Task 1-6 code — **the orchestrator wiring is not yet verified end-to-end and
must not be treated as working.**

## Recent Changes

- Orchestrator is now constructed and wired into the daemon
  (`internal/daemon/daemon.go`) instead of `Orchestrator: nil`; the daemon
  boots cleanly with it, no panics.
- `commander/spawner.RegistryLauncher`'s fake-handle bug (Task 4) is fixed:
  `SessionServiceLauncher` now spawns a real session through the session
  service and returns its real generated id.
- `POST /api/v1/workboard/cards/{cardId}/events` is mounted (Task 6); the
  five `ao workboard card ...` CLI commands work end-to-end against a live
  daemon, including `ao workboard card transition`.
- Task 7's live smoke test (isolated daemon, `AO_DATA_DIR`/`AO_RUN_FILE`
  under a scratch dir, throwaway git repo project, the built-in `command`
  harness standing in for a real coding-agent harness to avoid runaway
  spend) confirmed: Todo→Running auto-dispatch still produces a real,
  non-card-id session id (Task 4's original bug fixed for that path), and
  `ao workboard card transition` works end-to-end.

## Active Decisions

- Task 7 used the `command` harness (an existing, already-shipped non-AI
  adapter that runs a project-configured argv) instead of `claude-code`/
  `codex` for the live smoke, specifically because the bug found below makes
  the orchestrator spawn a new process roughly once per second once it
  starts — unacceptable with a real, credentialed AI harness.

## Blockers

**The orchestrator's per-tick spawn path double-writes `active_session` and
never syncs `WorkCard.SessionID`, causing unbounded duplicate real session
spawning for every card that reaches an active phase.** Full detail, root
cause, and evidence: `.superpowers/sdd/task-7-report.md`.

Summary: `commander/spawner.Spawner.Spawn` already calls
`store.InsertActiveSession`; `commander/orchestrator/tick.go`'s
`spawnSession` (and `orchestrator.go`'s `OnAgentFailed` fallback branch) call
it again right after with the same card/session — `active_session.card_id`
is a bare `PRIMARY KEY` with no upsert, so the second write always fails.
That failure, combined with `isSessionLive` comparing against
`WorkCard.SessionID` (which the orchestrator's spawn path never updates),
means the orchestrator can never observe its own successful spawn as "live"
and retries every tick — spawning a brand-new real session each time. In a
12-second live test this produced 6 real sessions for one card sitting in
`running`, and a single `--to review` transition produced a 7th, confirming
the same helper (`spawnSession`) is shared and equally broken for `review`
(and, by code inspection, `testing`).

**Do not mark the Hermes orchestrator wiring loop verified.** The fix
(remove the redundant `InsertActiveSession` call; sync `WorkCard.SessionID`
from the orchestrator's own spawns, or change `isSessionLive`'s comparison)
is a code change and out of scope for Task 7 (verification-only); it needs
its own implementation task before Task 7 can be re-run and pass.

Separately noted, not investigated further (unrelated to this plan):
`ao project set-config --config-json` reports success but does not persist —
the project's `config` column stayed empty after the CLI call; a direct
`PUT /api/v1/projects/{id}/config` HTTP call worked correctly. Worked around
for Task 7, not fixed.
