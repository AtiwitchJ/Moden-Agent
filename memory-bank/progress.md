# Progress

## Completed

- Director Agent Tasks 1-12 (`feat/live-terminals`,
  `docs/superpowers/plans/2026-08-03-director-agent.md`): All implemented,
  unit-test-clean (except the three known-red `TestLifecycleDispatcherIsUnwired_*`
  tests which are by design unrelated to this feature).
  - Task 11 `buildProjectConfig` DTO gap fix verified: `director` config
    block correctly persists to the database when using `--director-agent`
    and `--director-model` flags. (The `--config-json` path for the director
    config was also fixed as part of the same DTO gap fix.)
  - Tasks 1-8: Pure module unit tests pass (config, budget, tools, prompt).
  - Task 6: DeepAgents loop wired with ao-backed tools.
  - Tasks 7-9: Go adapter, domain, bundle embedding.
  - Tasks 10-12: Dispatch, CLI plumbing, frontend predicate fix.

- Hermes Director Orchestrator wiring plan
  (`docs/superpowers/plans/2026-08-03-hermes-orchestrator-wiring.md`), all 15
  tasks, verified end-to-end:
  - Tasks 1-6: `OrchestratorWiring` test-panic fix, `getCard`/
    `OrchestratorStore.GetWorkCard` fix against real storage, `*sqlite.Store`
    `active_session` wrapper methods, a real session-spawning `AgentLauncher`
    (`SessionServiceLauncher`) replacing the fake-handle `RegistryLauncher`,
    the daemon constructing and wiring a real `orchestrator.New(...)` instead
    of `Orchestrator: nil`, and the `POST /workboard/cards/{cardId}/events`
    route mounted so the `ao workboard card ...` CLI commands work.
  - Tasks 8, 10, 12, 14: fixed four compounding respawn-collision/leak bugs
    found across three live smoke tests plus one code review — see Known
    Issues (now resolved) for the full mechanism of each.
  - Task 15: final live smoke test confirmed the full lifecycle
    (Todo→Running→Review→Testing→Done) works cleanly, including nine
    consecutive timeout-triggered replacement cycles with zero errors and
    confirmed termination of every superseded session.

## In Progress

- **Director Agent Task 13 (`feat/live-terminals`): Module resolution BLOCKED.**
  `esbuild --bundle` + `deepagents@1.8.8` fixed the Director's own bare imports,
  but `@langchain/anthropic`'s internal dynamic import of `@anthropic-ai/sdk`
  fails to resolve via `NODE_PATH` with a flat `node_modules`. The Director
  harness cannot start as designed. Fix options: (a) fully bundle ALL deps via
  esbuild, or (b) use `@langchain/core`'s `ChatAnthropic` directly without
  `langchain`'s `universal.js` wrapper. Steps 7-9 completed. See
  `.superpowers/sdd/task-13-report.md`.

## Planned

- Tasks 7-9 of the *original* plan referenced by this branch's out-of-scope
  note (PR/CI watcher, reviewer invoker, testing invoker — automatic,
  signal-driven phase advancement) remain unbuilt. Today, phase advancement
  is agent-initiated only, via `ao workboard card transition` /
  `ao workboard card set-verdict` etc.
- The `dispatch.go`/orchestrator double-spawn-on-first-entry coordination gap
  (see Known Issues) needs a real design decision before it can be resolved.
- Moden Work product definition and implementation (pre-existing, unrelated).

## Known Issues

- **P0 Blocker — Director Agent (INCOMPLETE):** `esbuild --bundle` fixed the
  Director's own imports but `@langchain/anthropic`'s transitive dynamic import of
  `@anthropic-ai/sdk` remains unresolvable via `NODE_PATH` with a flat
  `node_modules`. The Director harness cannot start as designed. See
  `.superpowers/sdd/task-13-report.md`.

- **Fixed (Task 8, `cfa8327a`)**: the orchestrator's tick loop redundantly
  double-wrote `active_session` on every successful spawn (always failing
  the second write) and compared `isSessionLive` against a `WorkCard.SessionID`
  field the orchestrator's own spawns never updated, guaranteeing it could
  never recognize its own successful spawn as live. Produced 6 real sessions
  for one card within 12 seconds in Task 7's test.
- **Fixed (Task 10, `f73681f2`)**: `RecordAgentEvent`'s `agent_transition`
  handling never cleared the completed phase's `active_session` row, so a
  stale row survived every manual transition; once its timeout elapsed the
  same runaway recurred, delayed instead of immediate. Found by Task 9's live
  re-verification.
- **Fixed at the root (Task 12, `a3fa4173`)**: even with Tasks 8 and 10,
  a session's *own* phase timeout elapsing (no transition, no stale leftover
  — the session was correct and simply ran long) triggered the identical
  collision, because `InsertActiveSession` was a plain `INSERT` against a
  `card_id`-only `PRIMARY KEY`, never an upsert. Found by Task 11's live
  re-verification (waited past the exact timeout window neither Task 7 nor
  Task 9 had run long enough to reach). Changed the SQL to
  `INSERT ... ON CONFLICT(card_id) DO UPDATE`, closing every respawn
  collision at the source rather than patching per-trigger.
- **Fixed (Task 14, `6740615f`)**: Task 12's upsert stopped the crash-loop
  but never stopped the *superseded* session — every timeout-triggered
  replacement leaked one real, never-terminated orphaned process. Found by
  code review during Task 12's review pass. Wired `sessionsvc.Service.Kill`
  in as an optional `orchestrator.Config.Killer`, called (best-effort) before
  every replacement spawn.
- **Known, accepted, not a bug**: `dispatch.go`'s Todo→Running auto-dispatch
  and the orchestrator's own tick both spawn a worker the first time a card
  enters `running`, because `dispatch.go` never writes to `active_session`.
  One-time, bounded (confirmed at exactly 2 sessions in every live test in
  this plan — Tasks 9, 11, 15 — never more). A real design decision (which
  system owns the initial spawn) is needed to resolve it; out of scope here.
- **Non-blocking, unrelated to this plan, noted but not investigated**:
  `ao project set-config --config-json` CLI command reports success but does
  not persist to the `projects.config` column; `PUT /api/v1/projects/{id}/config`
  over HTTP directly works.
- The three `TestLifecycleDispatcherIsUnwired_*` tests in
  `internal/service/workboard/lifecycle_dispatcher_test.go` remain red by
  design (Tasks 7-9 of the *original*, larger plan — automatic phase
  advancement — are not part of this branch's scope).

## Verification

- Task 15 final live smoke (`.superpowers/sdd/task-15-report.md`): isolated
  daemon (`AO_DATA_DIR`/`AO_RUN_FILE`/`AO_PORT` under a scratch dir, never
  `~/.ao`), fresh throwaway git repo project, `command` harness (inert,
  avoids runaway spend). **PASS**: full lifecycle Todo→Running→Review→Testing→Done
  completed; zero `UNIQUE constraint failed` errors across the entire
  ~105-minute run; nine consecutive timeout-triggered replacement cycles on
  the review phase, each ~10m30s apart, every one clean; superseded sessions
  confirmed actually terminated (not just replaced in bookkeeping — only the
  final phase's session and the accepted first-phase double-spawn remained
  running at cleanup time). The subagent that ran this test hit an account
  spend limit mid-cleanup after confirming the result; the controlling
  session verified the isolated daemon's state directly, stopped it, killed
  the remaining tmux sessions, confirmed the real user daemon was untouched
  throughout, and finished this write-up.
- Task 12 (`.superpowers/sdd/task-12-report.md`): unit-level upsert
  regression test plus full backend suite, all green except the three
  documented out-of-scope tests.
- Task 14 (`.superpowers/sdd/task-14-report.md`): unit-level kill-on-replace
  tests plus full backend suite, same result.
- Task 11 live smoke (`.superpowers/sdd/task-11-report.md`), superseded by
  Task 15 above: found the root-cause collision bug that Task 12 fixed.
- Task 9 live smoke (`.superpowers/sdd/task-9-report.md`), superseded by
  Task 15 above: found the transition-triggered stale-row bug that Task 10
  fixed.
- Task 7 live smoke (`.superpowers/sdd/task-7-report.md`), superseded by
  Task 15 above: found the original immediate-runaway bug that Task 8 fixed.
