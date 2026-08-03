# Director Agent — Design Spec

## Overview

Today, only Hermes can act as a project's card-driving commander (the
"Hermes commanding" path in `internal/service/workboard/dispatch.go`, gated
by `project.Config.Orchestrator.Harness == domain.HarnessHermes`). This spec
adds a second, independent commander path — the **Director agent** — that
lets a project use any already-installed CLI harness (Claude Code, Codex, or
OpenCode) — on a configurable engine/model — to drive a work card through
`Todo → Running → Review → Testing → Redo → Done`, with Hermes reduced to an
ordinary worker-capable harness, on equal footing with Claude
Code/Codex/OpenCode, for projects that opt in.

**Scope note:** this covers the full path to a usable feature — the daemon
dispatch logic, the config plumbing needed to actually turn it on, and the
Director-page UI that has to recognize a non-Hermes commander. All three are
required; shipping only the first would leave the feature unreachable and
invisible.

**Motivation:** the existing Hermes commander got stuck on a real card —
it hit its 60-iteration response budget while waiting on human clarification
for an ambiguous review finding, instead of transitioning the card to
`blocked` and recording exactly what it needed. The user wants an
alternative commander, driven by a harness they already use daily, and
explicitly does not want the new path to touch, rename, or extend any of the
existing Hermes-specific code — it must be additive only.

## Non-Goals

- **Do not modify, rename, or extend any existing Hermes-specific
  *backend* file or identifier.** `dispatch.go`'s `commanding` branch,
  `isHermesCommander` (the Go one in `actions.go`), `HermesSender`,
  `PrepareHermesAnswerAttempt`, `hermesWorkboardPrompt`,
  `hermesCardBriefing`, `stall_nudge.go`, `switch_agent.go`'s
  commander-exclusion logic, and the public wire enum
  (`hermes_unavailable`/`non_hermes_orchestrator`) are all left completely
  unmodified. Hermes keeps working exactly as it does today for any project
  that still configures it as `Orchestrator.Harness`.
  **One deliberate exception, frontend only:** the renderer's local
  `isHermesCommander` constant in `WorkCardFocusPanel.tsx` *is* generalized
  (see "Frontend" below). It is a display-only predicate with no backend
  counterpart, and leaving it Hermes-only would make a Director-driven card
  render as having no commander — i.e. the feature would visibly not work.
  The Go `isHermesCommander` in `service/workboard/actions.go` is a
  different, untouched identifier that happens to share the name.
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

## Choosing the Director's Engine (model / provider)

The Director must be configurable to run on a chosen engine (e.g. MiniMax,
Qwen, Kimi, GPT) rather than being locked to one model. Because
`Config.Director` is a `RoleOverride`, it already carries an `AgentConfig`,
and `domain.AgentConfig` already has a `Model` field
(`internal/domain/agentconfig.go:29-30`). So engine selection needs **no new
config structure** — it is expressed as:

```jsonc
{
  "director": {
    "agent": "claude-code",              // which harness (CLI) runs the Director
    "agentConfig": { "model": "gpt-5" }  // which model that harness runs on
  }
}
```

**Critical caveat — model-flag support is not universal.** Only some
adapters actually forward `AgentConfig.Model` to their CLI. Verified by
inspection:

| Adapter | Honors `AgentConfig.Model`? | How |
|---|---|---|
| `claude-code` | Yes | appends `--model <model>` (`claudecode.go:162-164`) |
| `codex` | No | never reads `cfg.Config.Model` |
| `opencode` | No | never reads `cfg.Config.Model` |
| `kimi` | No | never reads `cfg.Config.Model` |
| `qwen` | No | never reads `cfg.Config.Model` |
| `hermes` | No | model comes from its own `providers.json` |

Consequences the implementation plan must respect:

1. For adapters that ignore `Model`, the engine is chosen by picking that
   **harness** (`director.agent`), and the model within it comes from that
   CLI's own configuration — not from AO. Setting `director.agentConfig.model`
   for those harnesses does nothing today.
2. Silently ignoring a configured model is a bad failure mode. The Director
   dispatch path must detect "a model was configured for a harness that
   cannot apply it" and surface it — at minimum a daemon warning log naming
   the harness and model, so the user is not left wondering why their engine
   choice had no effect. Deciding between warn-only and hard-reject is an
   implementation-plan decision; do not silently drop it.
3. Extending `codex`/`opencode`/`kimi`/`qwen` to forward `--model` is
   **out of scope for this spec** — each needs its own CLI-flag research.
   Track separately if the user needs model selection on those harnesses.

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

## Making the Config Actually Settable (blocker)

Adding `Config.Director` to the domain is not enough — there is currently
**no working way for a user to set it**, so the feature would ship unusable.
Two gaps, both must be closed:

### 1. The CLI drops unknown config fields (root-caused)

`ao project set-config --config-json` reports success but silently discards
fields. Root cause, verified: `internal/cli/project.go` hand-mirrors the
config DTOs, and its `agentConfig` (line 77-80) declares only
`Model`/`Permissions` — it is missing `Command []string`, which
`domain.AgentConfig` has. `encoding/json` ignores unknown fields by default,
so any `command` in the supplied JSON is dropped during unmarshal, and the
CLI then PUTs a config without it. (This is exactly the bug hit during live
testing today, worked around by calling
`PUT /api/v1/projects/{id}/config` directly.)

The same failure will silently swallow `director` unless the CLI DTO is
updated. Required changes in `internal/cli/project.go`:

- Add the missing `Command []string \`json:"command,omitempty"\`` to
  `agentConfig` (fixes the pre-existing bug).
- Add `Director roleOverride \`json:"director,omitempty"\`` to
  `projectConfig`.
- Consider rejecting unknown fields (`json.Decoder.DisallowUnknownFields`)
  for `--config-json` so future DTO drift fails loudly instead of silently.
  This is the durable fix for the whole class of bug; the plan should weigh
  it against breaking any caller that currently passes extra keys.

### 2. A first-class way to set it

`--config-json` requires hand-writing the whole config object. Add dedicated
flags to `ao project set-config`, mirroring the existing
`--worker-agent`/`--orchestrator-agent` pattern:

- `--director-agent <harness>` → `Config.Director.Harness`
- `--director-model <model>` → `Config.Director.AgentConfig.Model`

This is the minimum for the feature to be usable from the CLI. A desktop-UI
control for it is **out of scope** for this spec.

## Frontend: Director page must recognize a non-Hermes commander

The Director page currently hardcodes Hermes as the only possible commander,
so a Director-driven card would render with the wrong UI and wrong labels.
Verified in `frontend/src/renderer/components/WorkCardFocusPanel.tsx`:

```ts
// line 68
const isHermesCommander = session?.kind === "orchestrator" && session.harness === "hermes";
```

That flag gates the commander-specific UI — the "Work owner" vs "Linked
session" label (line 224), the "Hermes coordinates this task" text (line
226), and the "Nudge commander" vs "Nudge agent" action (line 196). With a
Director session (`kind === "orchestrator"`, `harness === "claude-code"`),
every one of those falls to the plain-worker branch, and the card looks like
it has no commander at all.

**Required change — generalize the predicate, don't add a second one.** The
`harness === "hermes"` clause is the only Hermes-specific part; `kind ===
"orchestrator"` alone already identifies a commander session correctly for
both paths:

```ts
const isCommander = session?.kind === "orchestrator";
```

Rename the variable to `isCommander` and replace the Hermes-specific copy
with neutral wording (e.g. "Commander coordinates this task", and where the
agent should be named, use `session.harness` rather than the literal
"Hermes"). Same treatment for the two dispatch-failure strings at lines
240-242 ("Hermes commander unavailable" / "A non-Hermes orchestrator is
active") — these render from the existing wire enum, so keep the enum values
untouched (see Non-Goals) and only neutralize the display text.

Also note `CreateWorkCardDialog.tsx:290` renders a hardcoded `hermes` chip
as the coding-agent label. That is about the card's *coding* agent, not the
commander, so it is **out of scope** here — flagged only so it is not
mistaken for part of this change.

## Teaching the Director its commands

The Director's ability to drive a card depends on it actually knowing the
`ao workboard card ...` commands. Two mechanisms already exist and the plan
must confirm which applies:

1. `directorWorkboardPrompt()` (this spec) states the commands inline.
2. `internal/skillassets/using-ao/` is installed into the data dir at daemon
   boot and read by sessions as a skill catalog; it contains workboard
   command docs that currently reference Hermes.

The prompt (1) is the load-bearing mechanism for this spec and must be
self-sufficient — the Director must work whether or not it reads the skill
asset. Updating the skill asset's Hermes-specific wording is out of scope.

## Error Handling

- No installed/authenticated harness for the configured `Director.Harness`: spawn fails, recorded as a `dispatch_failed` event with a new, generic reason (e.g. `director_unavailable`) — mirrors the existing `hermes_unavailable` pattern but as its own independent reason, not reusing the Hermes-named constant.
- Director session itself crashes/terminates: next `DispatchOnce` pass finds no live director session (`isProjectDirector` returns nothing live) and spawns a fresh one — same recovery shape as today's orchestrator respawn, no new logic needed.
- A card stuck in a non-terminal phase with a live director session: out of scope for this MVP (no stall-nudge equivalent) — the director's own budget-safety prompt rule (see above) is the mitigation, not a daemon-side timeout.

## Testing

**Backend**
- Table tests for `director_dispatch.go` covering: project with `Director.Harness` unset (existing Hermes/plain worker path runs, completely unaffected), project with it set to each of `claude-code`/`codex`/`opencode` (director path spawns correctly, one session per project, resumed not re-spawned on a later tick).
- A regression test asserting `dispatch.go`'s existing `commanding`-path tests still pass unmodified — proves the new branch is additive, not a behavior change to the existing path.
- `directorWorkboardPrompt()`'s output is asserted (string-contains) to include the `ao workboard card transition` command and NOT include `ao workboard status` (guards against reintroducing the stale-command bug) and to include explicit blocked-before-budget language.
- A test that a model configured against a harness which cannot apply it produces the warning/rejection decided in "Choosing the Director's Engine", rather than being silently dropped.

**Config plumbing**
- A CLI test that `--config-json` round-trips a `director` block *and* an `agentConfig.command` array without dropping either — this is the regression test for the hand-mirrored-DTO bug root-caused above, and would have caught it.
- A CLI test for the new `--director-agent` / `--director-model` flags.

**Frontend**
- `WorkCardFocusPanel` test asserting a session with `kind === "orchestrator"` and `harness === "claude-code"` renders the commander UI (work-owner label, "Nudge commander") — this fails before the predicate change and passes after.
- Existing Hermes-session tests must still pass unmodified, proving the generalized predicate did not regress the Hermes path.

## Out of Scope (explicit follow-ups, not built here)

- Auto-answer routing when a Director session goes idle waiting on a human (Hermes-only today, `answer.go`).
- Stall-nudging an idle Director commander (Hermes-only today, `stall_nudge.go`).
- Excluding the Director session from rate-limit auto-switch/kill (Hermes-only today, `switch_agent.go`).
- Renaming any existing Hermes-specific identifier, or generalizing the wire-level `hermes_unavailable`/`non_hermes_orchestrator` failure reasons (display text is neutralized; enum values stay).
- Any change to `commander/orchestrator/*` (the separate, already-wired Go-orchestrator system).
- Adding `--model` forwarding to the `codex`/`opencode`/`kimi`/`qwen` adapters (each needs its own CLI-flag research).
- A desktop-UI control for setting the Director config (CLI flags only in this pass).
- The hardcoded `hermes` coding-agent chip in `CreateWorkCardDialog.tsx:290` (about the card's coding agent, not the commander).
- Updating `internal/skillassets/using-ao/`'s Hermes-specific wording.
