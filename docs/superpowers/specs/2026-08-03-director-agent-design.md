# Director Agent — Design Spec

## Overview

Today, only Hermes can act as a project's card-driving commander (the
"Hermes commanding" path in `internal/service/workboard/dispatch.go`, gated
by `project.Config.Orchestrator.Harness == domain.HarnessHermes`). This spec
adds a second, independent commander path — the **Director agent** — that
lets a project use any already-installed CLI harness (Claude Code, Codex, or
OpenCode) to drive a work card through `Todo → Running → Review → Testing →
Redo → Done`, with Hermes reduced to an ordinary worker-capable harness, on
equal footing with Claude Code/Codex/OpenCode, for projects that opt in.

**Motivation:** the existing Hermes commander got stuck on a real card —
it hit its 60-iteration response budget while waiting on human clarification
for an ambiguous review finding, instead of transitioning the card to
`blocked` and recording exactly what it needed. The user wants an
alternative commander, driven by a harness they already use daily, and
explicitly does not want the new path to touch, rename, or extend any of the
existing Hermes-specific code — it must be additive only.

## Non-Goals

- **Do not modify, rename, or extend any existing Hermes-specific file or
  identifier.** `dispatch.go`'s `commanding` branch, `isHermesCommander`,
  `HermesSender`, `PrepareHermesAnswerAttempt`, `hermesWorkboardPrompt`,
  `hermesCardBriefing`, `stall_nudge.go`, `switch_agent.go`'s
  commander-exclusion logic, and the public wire enum
  (`hermes_unavailable`/`non_hermes_orchestrator`) are all left completely
  unmodified. Hermes keeps working exactly as it does today for any project
  that still configures it as `Orchestrator.Harness`.
- **Not a generalization of the existing commanding path.** This is a
  parallel, independent mechanism, not a parameterized version of the
  Hermes one. The two paths do not share code beyond the already
  harness-agnostic session-spawn primitives (`session_manager`, `session`
  service) both already build on.
- **No parity with Hermes's auxiliary behaviors in this pass.** Auto-answer
  on an unresponsive commander (`answer.go`), stall-nudging an idle
  commander (`stall_nudge.go`), and commander-aware rate-limit switching
  (`switch_agent.go`) are Hermes-only today and are **not** built for the
  Director agent in this spec — see "Out of Scope" below. The MVP is the
  core drive loop only.
- **The new `commander/orchestrator` Go package (wired up earlier this
  session) is untouched and unrelated.** That system has the daemon itself
  decide phase transitions and spawn short-lived per-phase sessions with no
  long-lived AI commander at all. This spec is about the *other* pattern —
  one long-lived AI session that self-drives a card via CLI calls — mirroring
  Hermes's existing shape, just with a swappable harness.

## Architecture

A project opts in by setting a new config field. When set, `DispatchOnce`
gains one additive branch — checked *before* the existing `commanding`
check, so a project cannot accidentally have both paths active — that
spawns or reuses a single long-lived "director" session for the project,
using the configured harness, with a director-specific system prompt. The
director session drives cards through the workflow itself, by shelling out
to the same `ao workboard card ...` CLI commands (mounted this session,
already harness-agnostic) that any agent uses to report progress.

```
Card enters Todo
  → DispatchOnce sees project.Config.Director.Harness is set
  → spawns/resumes the one Director session for this project (Claude Code / Codex / OpenCode)
  → Director session reads the card (`ao workboard card show`), works or delegates to worker sessions
  → Director calls `ao workboard card transition <id> --to <next>` itself as phases complete
  → repeats until the card reaches a terminal state (Done / Blocked)
```

## Config Schema

Add a new field to `domain.ProjectConfig`, reusing the existing
`RoleOverride` type (already shaped exactly right — `Harness` +
`AgentConfig` — no new type needed):

```go
// projectconfig.go
type ProjectConfig struct {
	Worker       RoleOverride `json:"worker,omitempty"`
	Orchestrator RoleOverride `json:"orchestrator,omitempty"`
	Director     RoleOverride `json:"director,omitempty"` // NEW
	// ... existing fields unchanged
}
```

`Director.Harness == ""` means the project has not opted in — every existing
project is unaffected with zero config migration needed. Validation mirrors
the existing `Worker`/`Orchestrator` loop in `projectconfig.go` (unknown
harness → error), extended to include `"director"` in the role map.

**Guard:** if a project sets both `Director.Harness` and
`Orchestrator.Harness == HarnessHermes`, the Director path takes priority
(checked first in `DispatchOnce`) — a project should realistically only use
one commander mechanism, but this spec does not need to forbid the
combination, just define a deterministic precedence.

## New Files

### `internal/service/workboard/director_prompt.go`

`directorWorkboardPrompt() string` — the system prompt appended for a
Director session. Content mirrors the *intent* of `hermesWorkboardPrompt`
(read the card, plan, delegate to workers via `ao spawn`, drive the card
through phases via CLI, escalate to `blocked` when stuck) but is written
harness-neutral (no "Hermes" wording) and fixes the two real problems this
spec exists to solve:

1. Uses the **current** command name: `ao workboard card transition <id>
   --to <status> --reason <text>` (not the older `ao workboard status`
   Hermes's prompt still references).
2. **Budget-safety rule, stated explicitly and early in the prompt:** the
   Director must track its own remaining iteration budget. If a phase
   cannot be resolved with clear confidence — ambiguous finding, missing
   input, anything requiring a human decision — it must call
   `ao workboard card transition <id> --to blocked --reason "<exact
   question for the human>"` **immediately**, before continuing to converse
   about the ambiguity, not after exhausting its budget waiting for an
   answer that never comes. A blocked card with a clear, recorded question
   is always the correct outcome over a silently exhausted session.

### `internal/service/workboard/director_dispatch.go`

- `directorEnabled(project domain.ProjectRecord) bool` — `project.Config.Director.Harness != ""`.
- `isProjectDirector(session domain.SessionRecord, harness domain.AgentHarness) bool` — `!session.IsTerminated && session.Kind == domain.KindOrchestrator && session.Harness == harness`. A small, self-contained predicate; does not touch or reuse `isHermesCommander`.
- Dispatch logic: given a project with `directorEnabled == true`, find its existing live director session (`ListSessions` + `isProjectDirector`) or spawn a new one with `Kind: domain.KindOrchestrator`, `Harness: project.Config.Director.Harness` (explicit, not resolved implicitly), and `directorWorkboardPrompt()` as the prompt.
  **Implementation note, verify against real code before writing this:** `sessionsvc.Service.SpawnOrchestrator` resolves its harness implicitly from `project.Config.Orchestrator.Harness` (confirmed: it takes no harness parameter) and internally calls `verifyOrchestratorReplacement`, which also reads `Config.Orchestrator.Harness`. Both of those read the *wrong* config field for a Director spawn. The Director path most likely needs to call the lower-level `Service.Spawn(ctx, ports.SpawnConfig{Kind: KindOrchestrator, Harness: project.Config.Director.Harness, ...})` directly instead of `SpawnOrchestrator`, and — since that skips `verifyOrchestratorReplacement` — determine during planning whether an equivalent check is needed for the Director path or whether it's safe to omit (Director sessions aren't subject to the same "exactly one Hermes commander" invariant that check enforces). Do not assume `SpawnOrchestrator` can be reused as-is without resolving this.

### `internal/service/workboard/dispatch.go` (one additive change)

In `DispatchOnce`, before the existing `commanding := ...` line, add:

```go
if directorEnabled(project) {
	return d.dispatchToDirector(ctx, project, cards, now) // new function in director_dispatch.go
}
```

Nothing else in this file changes. `commanding`, `hasActiveNonHermesOrchestrator`, `hermesCardBriefing`, and every existing branch remain byte-for-byte identical.

## Error Handling

- No installed/authenticated harness for the configured `Director.Harness`: spawn fails, recorded as a `dispatch_failed` event with a new, generic reason (e.g. `director_unavailable`) — mirrors the existing `hermes_unavailable` pattern but as its own independent reason, not reusing the Hermes-named constant.
- Director session itself crashes/terminates: next `DispatchOnce` pass finds no live director session (`isProjectDirector` returns nothing live) and spawns a fresh one — same recovery shape as today's orchestrator respawn, no new logic needed.
- A card stuck in a non-terminal phase with a live director session: out of scope for this MVP (no stall-nudge equivalent) — the director's own budget-safety prompt rule (see above) is the mitigation, not a daemon-side timeout.

## Testing

- Table tests for `director_dispatch.go` covering: project with `Director.Harness` unset (existing Hermes/plain worker path runs, completely unaffected), project with it set to each of `claude-code`/`codex`/`opencode` (director path spawns correctly, one session per project, resumed not re-spawned on a later tick).
- A regression test asserting `dispatch.go`'s existing `commanding`-path tests still pass unmodified — proves the new branch is additive, not a behavior change to the existing path.
- `directorWorkboardPrompt()`'s output is asserted (string-contains) to include the `ao workboard card transition` command and NOT include `ao workboard status` (guards against reintroducing the stale-command bug) and to include explicit blocked-before-budget language.

## Out of Scope (explicit follow-ups, not built here)

- Auto-answer routing when a Director session goes idle waiting on a human (Hermes-only today, `answer.go`).
- Stall-nudging an idle Director commander (Hermes-only today, `stall_nudge.go`).
- Excluding the Director session from rate-limit auto-switch/kill (Hermes-only today, `switch_agent.go`).
- Renaming any existing Hermes-specific identifier, or generalizing the wire-level `hermes_unavailable`/`non_hermes_orchestrator` failure reasons.
- Any change to `commander/orchestrator/*` (the separate, already-wired Go-orchestrator system).
