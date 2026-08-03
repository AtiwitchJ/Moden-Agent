# Active Context

## Current Focus

Hermes Director Orchestrator: Last-Mile Wiring
(`docs/superpowers/plans/2026-08-03-hermes-orchestrator-wiring.md`). Tasks
1-6 (test fix, `getCard` fix, `active_session` store, real launcher, daemon
wiring, agent-events route) are implemented and reviewed clean. Task 7 found
a blocking immediate-runaway bug; Task 8 (commit `cfa8327a`) fixed it. Task 9
re-verified against a live daemon and confirmed the immediate runaway is
gone, but found a second, related bug when a card changes phase — **the
orchestrator wiring is still not verified end-to-end and must not be treated
as working.**

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
- Task 8 (commit `cfa8327a`) removed `tick.go`'s/`orchestrator.go`'s
  redundant `InsertActiveSession` re-insert (the primary Task 7 bug) and
  simplified `isSessionLive` to a pure session-age check instead of the
  `WorkCard.SessionID` comparison that could never succeed.
- Task 9's live smoke test (same isolated-daemon setup as Task 7, `command`
  harness) confirmed the fix holds for a card's first phase: 2 sessions
  total (1 expected new-real-spawn + 1 pre-existing, documented
  `dispatch.go`/orchestrator double-spawn — see Active Decisions), flat
  across 90+ seconds / 3 tick intervals, zero `UNIQUE constraint failed`
  errors. It then found a **second, still-blocking bug** when the card
  transitions to `review` — see Blockers.

## Active Decisions

- Task 7/9 both used the `command` harness (an existing, already-shipped
  non-AI adapter that runs a project-configured argv) instead of
  `claude-code`/`codex` for the live smoke, specifically to avoid burning
  real API cost while verifying spawn-loop behavior.
- Known, accepted, out-of-scope (not the bug tracked below): `dispatch.go`'s
  Todo→Running auto-dispatch and the orchestrator's own tick both spawn a
  worker the first time a card enters `running`, because `dispatch.go` never
  writes to `active_session`. One-time, bounded, does not grow — confirmed
  again in Task 9 (session count settles at 2, not more, across the full
  90s+ window). A real design decision (which system owns the initial spawn)
  is needed to resolve it; not part of this wiring plan.

## Blockers

**Task 9 found a second respawn-runaway bug, distinct from Task 7's (which
Task 8 fixed): a card's `active_session` row from a completed phase is never
cleaned up when the card advances via `ao workboard card transition`, so
once that stale row's phase-appropriate timeout elapses, the orchestrator
spawns a brand-new real session on every 30s tick, forever, and every one of
those spawns' bookkeeping insert fails with the same
`UNIQUE constraint failed: active_session.card_id` Task 7 originally
reported.** Full detail, root cause, and evidence:
`.superpowers/sdd/task-9-report.md`.

Summary: `active_session` is keyed by a bare `card_id PRIMARY KEY` (one row
per **card**, not per **card+phase**); `InsertActiveSession` is a plain
`INSERT`, never an upsert. `DeleteActiveSession` — the only code that clears
that row — is called solely from `OnAgentCompleted`/`OnAgentFailed`
(`orchestrator.go` lines 108, 195), which are driven by real agent-signal
events (`/events` route wiring) that this plan explicitly does not build.
The only phase-advance mechanism actually reachable today,
`ao workboard card transition`, never touches `active_session`. So: a
card's coding-phase row survives the transition to `review`; `isSessionLive`
(correctly age-based since Task 8) treats it as the review phase's live
session for as long as it's within the review timeout (10 min) — silently
suppressing the correct spawn — and once that timeout elapses, every tick
performs a real spawn whose bookkeeping insert collides with the still-stale
row and fails, forever. In a live test this produced 4-5 additional real
sessions, one every 30 seconds, with no sign of stopping, before the daemon
was manually stopped.

**Do not mark the Hermes orchestrator wiring loop verified.** Likely fix
directions (not implemented — verification-only tasks): make
`InsertActiveSession` an upsert keyed on `card_id`, and/or have
`checkActiveSession`/`isSessionLive` also compare the stored row's `phase`
against the card's current status, and/or wire `DeleteActiveSession` into
whatever route legitimately advances a card's phase today. Needs its own
implementation task before this can be re-verified and pass.

Separately noted, not investigated further (unrelated to this plan):
`ao project set-config --config-json` reports success but does not persist —
the project's `config` column stayed empty after the CLI call; a direct
`PUT /api/v1/projects/{id}/config` HTTP call worked correctly. Worked around
in both Task 7 and Task 9, not fixed.
