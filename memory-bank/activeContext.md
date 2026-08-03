# Active Context

## Current Focus

Hermes Director Orchestrator: Last-Mile Wiring
(`docs/superpowers/plans/2026-08-03-hermes-orchestrator-wiring.md`) is
**complete and verified**. All 15 tasks implemented, reviewed clean, and the
final live smoke test (Task 15) confirmed the full card lifecycle —
Todo → Running → Review → Testing → Done — works with the daemon spawning a
real agent session per phase, with zero bookkeeping errors, across a
~105-minute run including nine consecutive timeout-triggered replacement
cycles. **The orchestrator wiring loop is verified working and safe to use**
(with the one known, accepted limitation noted below). Next step is
`superpowers:finishing-a-development-branch` (merge/PR decision).

## Recent Changes

- Orchestrator is now constructed and wired into the daemon
  (`internal/daemon/daemon.go`) instead of `Orchestrator: nil`; the daemon
  boots cleanly with it, no panics.
- `commander/spawner.RegistryLauncher`'s fake-handle bug is fixed:
  `SessionServiceLauncher` spawns a real session through the session service
  and returns its real generated id.
- `POST /api/v1/workboard/cards/{cardId}/events` is mounted; the five
  `ao workboard card ...` CLI commands work end-to-end against a live daemon.
- Four respawn-collision/leak bugs were found by three rounds of live
  smoke testing plus one code review, and fixed:
  1. (Task 8, `cfa8327a`) Redundant `InsertActiveSession` re-insert in
     `tick.go`/`orchestrator.go` (always failed, `isSessionLive` compared
     against a field the orchestrator's own spawns never wrote) — caused an
     immediate, ~1-per-second runaway.
  2. (Task 10, `f73681f2`) `RecordAgentEvent`'s `agent_transition` handling
     never cleared the completed phase's stale `active_session` row — caused
     the same runaway, delayed by the stale row's timeout (~10 min).
  3. (Task 12, `a3fa4173`) Root cause: `InsertActiveSession` was a plain,
     non-upsert `INSERT` against a `card_id`-only `PRIMARY KEY`. Changed to
     `INSERT ... ON CONFLICT(card_id) DO UPDATE` — closes every future
     "replace the current session" collision at the source, not per-trigger.
  4. (Task 14, `6740615f`) The upsert stopped the crash-loop but never
     stopped the *superseded* session — wired `sessionsvc.Service.Kill` in as
     an optional `orchestrator.Config.Killer`, called (best-effort, failures
     swallowed) immediately before every timeout-triggered replacement spawn.
- Task 15's final live smoke test (isolated daemon, `command` harness)
  confirmed all four fixes hold together: full lifecycle completes, zero
  `UNIQUE constraint failed` anywhere, and superseded sessions are actually
  terminated (not just replaced in bookkeeping) — see
  `.superpowers/sdd/task-15-report.md`.

## Active Decisions

- Every live smoke test in this plan (Tasks 7, 9, 11, 15) used the `command`
  harness (an existing, already-shipped non-AI adapter that runs a
  project-configured argv) instead of `claude-code`/`codex`, specifically to
  avoid burning real API cost while a spawn-loop bug was suspected or under
  test. Real coding-agent harnesses exercise the identical, now-fixed spawn
  code path — no further harness-specific testing is needed before real use.
- Known, accepted, out-of-scope: `dispatch.go`'s Todo→Running auto-dispatch
  and the orchestrator's own tick both spawn a worker the first time a card
  enters `running`, because `dispatch.go` never writes to `active_session`.
  One-time, bounded, does not grow — confirmed in every one of Tasks 9, 11,
  and 15's live tests (session count settles at 2, never more). A real
  design decision (which system owns the initial spawn) is needed to resolve
  it; not part of this wiring plan.
- Phase advancement today is agent-initiated only, via
  `ao workboard card transition` (and the other four `ao workboard card ...`
  reporting commands, which are audit-only). Automatic, signal-driven
  advancement (PR/CI watcher, reviewer invoker, testing invoker — Tasks 7-9
  of the *original*, larger `.hermes/plans/2026-08-02_223800-hermes-director-orchestrator.md`)
  remains unbuilt and out of scope here; the three
  `TestLifecycleDispatcherIsUnwired_*` tests stay red by design until that
  separate effort lands.

## Blockers

None. All four respawn-collision/leak bugs found during this plan's own
verification passes are fixed and re-confirmed clean. The orchestrator
wiring is ready for `superpowers:finishing-a-development-branch`.

Separately noted, not investigated, unrelated to this plan:
`ao project set-config --config-json` reports success but does not persist —
the project's `config` column stayed empty after the CLI call; a direct
`PUT /api/v1/projects/{id}/config` HTTP call worked correctly. Worked around
in Tasks 7, 9, 11, and 15's live tests, not fixed.
