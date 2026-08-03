# Director Agent — Design Spec

## Overview

Today, only Hermes can act as a project's card-driving commander (the
"Hermes commanding" path in `internal/service/workboard/dispatch.go`, gated
by `project.Config.Orchestrator.Harness == domain.HarnessHermes`). Every
other harness in the registry is a thin wrapper around an installed CLI
(`claude`, `codex`, `hermes`, …), so "which model drives the card" is
whatever that CLI happens to be configured for — not something AO controls.

This spec adds the **Director agent**: a first-party agent harness, written
in TypeScript on [LangChain DeepAgents]
(https://docs.langchain.com/oss/javascript/deepagents/overview), that AO owns
end to end. It drives a work card through `Todo → Running → Review → Testing
→ Redo → Done`, delegates implementation to worker sessions, and — unlike
every existing harness — lets the user choose its engine explicitly
(`openai:gpt-5`, `anthropic:claude-sonnet-4-6`, `openrouter:…` for MiniMax /
Qwen / Kimi, `ollama:…` for local models).

Hermes is not modified, not removed, and not special-cased by any of this. It
remains one ordinary harness among the others, usable as a worker (or as the
legacy commander, unchanged, for projects that still configure it that way).

**Motivation:** the existing Hermes commander got stuck on a real card — it
hit its 60-iteration response budget while waiting on human clarification for
an ambiguous review finding, instead of transitioning the card to `blocked`
and recording what it needed. Because Hermes is an opaque external CLI, that
behavior can only be requested via prompt, never guaranteed. Owning the agent
loop means the budget/blocked rule becomes enforceable code, not a polite
instruction.

## Why TypeScript, not Python

DeepAgents ships both a [Python](https://docs.langchain.com/oss/python/deepagents/overview)
and a [JavaScript/TypeScript](https://docs.langchain.com/oss/javascript/deepagents/overview)
library with the same feature set (subagents, custom tools, planning
middleware, virtual filesystem, and the same provider list). TypeScript is
the right choice for this repo:

- **No new runtime.** Node 22 and npm are already required — the Electron
  frontend, the root `package.json` scripts, and the `openapi-typescript`
  codegen all depend on them. Python appears nowhere in the repo (one
  prototype file aside) and would add a runtime every user must install.
- **Existing toolchain.** The repo already has TypeScript config, typecheck,
  and build wiring to extend.
- **Free type-safe API access.** `npm run api` already generates
  `frontend/src/api/schema.ts` from the daemon's OpenAPI spec. A TypeScript
  Director can consume those exact generated types to call daemon endpoints;
  a Python Director would need a hand-maintained client.

## Non-Goals

- **Do not modify, rename, or extend any existing Hermes-specific *backend*
  file or identifier.** `dispatch.go`'s `commanding` branch,
  `isHermesCommander` (the Go one in `actions.go`), `HermesSender`,
  `PrepareHermesAnswerAttempt`, `hermesWorkboardPrompt`, `hermesCardBriefing`,
  `stall_nudge.go`, `switch_agent.go`'s commander-exclusion logic, and the
  public wire enum (`hermes_unavailable`/`non_hermes_orchestrator`) are left
  completely unmodified. Hermes keeps working exactly as it does today.
  **One deliberate exception, frontend only:** the renderer's local
  `isHermesCommander` constant in `WorkCardFocusPanel.tsx` *is* generalized
  (see "Frontend" below) — it is a display-only predicate with no backend
  counterpart, and leaving it Hermes-only would make a Director-driven card
  render as having no commander, i.e. the feature would visibly not work.
- **Not a generalization of the existing commanding path.** The Director is a
  parallel, independent mechanism, not a parameterized version of the Hermes
  one.
- **No parity with Hermes's auxiliary behaviors in this pass.** Auto-answer
  (`answer.go`), stall-nudging (`stall_nudge.go`), and commander-aware
  rate-limit switching (`switch_agent.go`) are Hermes-only today and are not
  built for the Director here. The MVP is the core drive loop.
- **The `commander/orchestrator` Go package is untouched and unrelated.**
  That system has the daemon itself decide phase transitions with no
  long-lived AI commander. This spec is the *other* pattern — one long-lived
  agent that self-drives a card.

## Architecture

Three pieces, in dependency order:

```
┌──────────────────────────────────────────────────────────┐
│ director/  (new TypeScript package)                      │
│   DeepAgents loop: model, system prompt, tools,          │
│   subagents, budget/blocked enforcement                  │
│   ↓ tools call                                           │
│   `ao workboard card ...` / `ao spawn` (CLI subprocess)   │
└──────────────────────────────────────────────────────────┘
             ▲ launched as a normal session process
┌──────────────────────────────────────────────────────────┐
│ internal/adapters/agent/director/  (new Go adapter)      │
│   HarnessDirector = "director"; builds the argv           │
│   (`node <installed-path>/index.js`), passes model +      │
│   prompt + card context via env                          │
└──────────────────────────────────────────────────────────┘
             ▲ selected by config
┌──────────────────────────────────────────────────────────┐
│ Config.Director (RoleOverride) + dispatch branch          │
│   opt-in per project; spawns/reuses one Director session  │
└──────────────────────────────────────────────────────────┘
```

**Flow:**

```
Card enters Todo
  → DispatchOnce sees project.Config.Director.Harness is set
  → spawns/resumes the project's one Director session (the Go adapter runs the TS agent)
  → Director reads the card, plans, delegates implementation via `ao spawn` worker sessions
  → Director calls `ao workboard card transition <id> --to <next>` as phases complete
  → repeats until terminal state (Done / Blocked)
```

**Why the Director talks to AO through the `ao` CLI rather than the HTTP API:**
the five `ao workboard card ...` commands already exist, are already wired to
the daemon, and were verified end-to-end during this session's live testing.
Shelling out reuses that tested path and inherits the daemon's validation
(`ValidateWorkflowTransition`) for free. The generated TypeScript API types
remain available if a later pass wants direct HTTP calls; this spec does not
need them.

## The `director/` TypeScript package

New top-level directory (sibling to `backend/` and `frontend/`) — it is a
daemon-side agent, not renderer code, so it does not belong under
`frontend/`.

```
director/
  package.json          deepagents, langchain, @langchain/core
  tsconfig.json
  src/
    index.ts            entrypoint: read env config, build agent, run loop
    prompt.ts           the Director system prompt
    tools.ts            ao-CLI-backed tools (transition, show, spawn worker, …)
    budget.ts           iteration-budget tracking + forced-blocked rule
```

### Engine selection

DeepAgents takes the model as a `provider:model-name` string, which maps
directly onto the existing `AgentConfig.Model` field — no new config shape:

```jsonc
{
  "director": {
    "agent": "director",
    "agentConfig": { "model": "openrouter:minimax/minimax-m2" }
  }
}
```

Supported provider prefixes per the DeepAgents docs: `openai`, `anthropic`,
`google`, `openrouter`, `fireworks`, `baseten`, `ollama`. This is what makes
"pick the Director's engine — MiniMax, Qwen, Kimi, GPT" work: MiniMax/Qwen/
Kimi are reachable through `openrouter:` (or `ollama:` when self-hosted), GPT
through `openai:`, Claude through `anthropic:`.

**Unlike every other harness, the Director genuinely honors this field** —
verified, only `claude-code` forwards `AgentConfig.Model` today (`--model`,
`claudecode.go:162-164`); `codex`, `opencode`, `kimi`, `qwen`, and `hermes`
all ignore it. The Director owning its own loop is precisely what makes
engine selection real rather than advisory.

The plan must define a default model for when `agentConfig.model` is unset,
and fail with a clear error (not a silent fallback) when the configured
provider's API key is missing.

### API keys

LangChain reads provider keys from conventional environment variables
(`OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `OPENROUTER_API_KEY`, …). The
existing per-project `Config.Env` map is already passed into session
processes, so keys flow through the mechanism that exists. The plan must
confirm this end to end and must not log key values.

### Budget and the blocked rule (the motivating fix)

The Director tracks its own iteration count. On approaching the limit, or on
hitting genuine ambiguity requiring a human decision, it must call
`ao workboard card transition <id> --to blocked --reason "<exact question>"`
**before** continuing to deliberate — enforced in `budget.ts`, not merely
requested in the prompt. A blocked card carrying a clear question is always
a better outcome than a silently exhausted session. This is the concrete
improvement over the Hermes commander, and is directly testable.

## Go adapter: `internal/adapters/agent/director/`

Follows the shape of the existing adapters (closest template: `command`,
which launches a configured argv and passes prompt via `AO_PROMPT` /
`AO_SYSTEM_PROMPT`, reporting activity through `ao hooks command <event>`).

- `domain.HarnessDirector AgentHarness = "director"`, added to
  `AllHarnesses` in `internal/domain/harness.go`.
- Registered in `internal/adapters/agent/registry/`'s `Constructors()` — the
  single edit that makes it appear everywhere (`ao spawn --agent director`,
  the agent catalog, `ao doctor`).
- `GetLaunchCommand` builds `node <dir>/index.js` with the resolved model and
  prompt; `GetRestoreCommand` resumes an existing agent session (mirroring
  how `claudecode`/`codex`/`hermes` resume by native session id) so a
  long-lived commander survives a daemon restart.
- `AuthStatus` reports whether the configured provider's key is present.

**Where the built package lives at runtime** is an open implementation
decision the plan must settle: the adapter needs a stable absolute path to
the built JS. `internal/skillassets` already solves an equivalent problem —
it embeds assets in the Go binary and installs them into the data dir at
boot, giving sessions a stable path. Reusing that pattern (embed the built
Director bundle, install under `~/.ao/`) avoids requiring a separate install
step and keeps the daemon a single distributable binary. The plan should
evaluate that against simply requiring `npm install` in `director/`, and pick
one — but bundle size and build wiring must be considered before committing.

## Config Schema

```go
// internal/domain/projectconfig.go
type ProjectConfig struct {
	Worker       RoleOverride `json:"worker,omitempty"`
	Orchestrator RoleOverride `json:"orchestrator,omitempty"`
	Director     RoleOverride `json:"director,omitempty"` // NEW
	// ... existing fields unchanged
}
```

`RoleOverride` is already `{Harness, AgentConfig}` — exactly what is needed,
no new type. `Director.Harness == ""` means not opted in, so every existing
project is unaffected with zero migration. Validation mirrors the existing
`Worker`/`Orchestrator` loop (unknown harness → error), extended with
`"director"`.

**Precedence:** if a project sets both `Director.Harness` and
`Orchestrator.Harness == HarnessHermes`, the Director path wins (checked
first in `DispatchOnce`). Deterministic, not forbidden.

## Dispatch integration (one additive change)

In `DispatchOnce`, before the existing `commanding := ...` line:

```go
if directorEnabled(project) {
	return d.dispatchToDirector(ctx, project, cards, now)
}
```

Nothing else in `dispatch.go` changes. `commanding`,
`hasActiveNonHermesOrchestrator`, `hermesCardBriefing`, and every existing
branch stay byte-for-byte identical.

New file `internal/service/workboard/director_dispatch.go`:

- `directorEnabled(project) bool` — `project.Config.Director.Harness != ""`.
- `isProjectDirector(session, harness) bool` — `!session.IsTerminated &&
  session.Kind == domain.KindOrchestrator && session.Harness == harness`.
  Self-contained; does not touch `isHermesCommander`.
- `dispatchToDirector(...)` — find the project's live Director session or
  spawn one.

**Implementation note, verify before writing:** `sessionsvc.Service.SpawnOrchestrator`
takes no harness parameter — it resolves the harness implicitly from
`project.Config.Orchestrator.Harness`, and internally calls
`verifyOrchestratorReplacement`, which also reads that field. Both read the
*wrong* config field for a Director spawn. The Director path most likely
needs `Service.Spawn(ctx, ports.SpawnConfig{Kind: KindOrchestrator, Harness:
project.Config.Director.Harness, ...})` directly. Since that skips
`verifyOrchestratorReplacement`, the plan must decide whether an equivalent
guard is needed for the Director path. Do not assume `SpawnOrchestrator` is
reusable as-is.

## Making the config settable (blocker)

Adding `Config.Director` to the domain is not enough — there is currently no
working way to set it, so the feature would ship unreachable.

### The CLI silently drops unknown config fields (root-caused)

`ao project set-config --config-json` reports success but discards fields.
Verified root cause: `internal/cli/project.go` hand-mirrors the config DTOs,
and its `agentConfig` (lines 77-80) declares only `Model`/`Permissions` —
it is missing `Command []string`, which `domain.AgentConfig` has.
`encoding/json` ignores unknown fields by default, so `command` is dropped
during unmarshal and the CLI PUTs a config without it. (This is the bug hit
during live testing today, worked around by calling
`PUT /api/v1/projects/{id}/config` directly.) `director` would be swallowed
identically.

Required in `internal/cli/project.go`:

- Add the missing `Command []string \`json:"command,omitempty"\`` to
  `agentConfig` — fixes the pre-existing bug.
- Add `Director roleOverride \`json:"director,omitempty"\`` to `projectConfig`.
- Evaluate `json.Decoder.DisallowUnknownFields` for `--config-json` so future
  DTO drift fails loudly instead of silently. This is the durable fix for the
  whole class of bug; weigh it against breaking callers that pass extra keys.

### First-class flags

`--config-json` requires hand-writing the whole object. Add, mirroring the
existing `--worker-agent`/`--orchestrator-agent` pattern:

- `--director-agent <harness>` → `Config.Director.Harness`
- `--director-model <provider:model>` → `Config.Director.AgentConfig.Model`

A desktop-UI control is out of scope.

## Frontend: the Director page must recognize a non-Hermes commander

`frontend/src/renderer/components/WorkCardFocusPanel.tsx` hardcodes Hermes as
the only possible commander:

```ts
// line 68
const isHermesCommander = session?.kind === "orchestrator" && session.harness === "hermes";
```

That flag gates the "Work owner" vs "Linked session" label (line 224), the
"Hermes coordinates this task" text (line 226), and "Nudge commander" vs
"Nudge agent" (line 196). A Director session (`kind === "orchestrator"`,
`harness === "director"`) falls to the plain-worker branch on all of them, so
the card renders as having no commander.

**Change:** generalize the predicate rather than adding a second one —
`kind === "orchestrator"` alone already identifies a commander session for
both paths:

```ts
const isCommander = session?.kind === "orchestrator";
```

Rename to `isCommander` and replace Hermes-specific copy with neutral wording
(name the agent from `session.harness` where an agent name is shown). Same
for the dispatch-failure strings at lines 240-242 — those render from the
existing wire enum, so keep enum values untouched (see Non-Goals) and
neutralize only display text.

`CreateWorkCardDialog.tsx:290`'s hardcoded `hermes` chip is about the card's
*coding* agent, not the commander — out of scope, noted so it is not confused
for part of this change.

## Error handling

- **Provider key missing / model string invalid:** the Director exits with a
  clear, non-secret error; dispatch records a `dispatch_failed` event with a
  new generic reason (e.g. `director_unavailable`), independent of the
  Hermes-named constants.
- **Node or the Director bundle missing:** surfaced by the adapter's
  `AuthStatus`/launch error and by `ao doctor`, the same way a missing CLI
  binary is for other harnesses.
- **Director session crashes:** the next `DispatchOnce` pass finds no live
  Director session and spawns a fresh one — same recovery shape as today's
  orchestrator respawn, no new logic.
- **Card stuck in a non-terminal phase with a live Director:** out of scope
  (no stall-nudge equivalent in this pass). The enforced budget/blocked rule
  is the mitigation.

## Testing

**Director package (TypeScript)**
- Budget enforcement: a run that approaches the iteration limit issues the
  `--to blocked` transition with a non-empty reason before exhausting itself.
  This is the regression test for the motivating bug.
- Tool layer: each `ao`-backed tool builds the expected argv and surfaces a
  non-zero exit as a tool error rather than silently continuing.
- Engine selection: a configured `provider:model` string reaches the agent
  constructor; a missing provider key produces a clear error, not a silent
  fallback.

**Go adapter**
- `GetLaunchCommand` includes the resolved model and prompt; `GetRestoreCommand`
  resumes by native session id.
- The adapter is registered — `registry.Constructors()` yields `director`, and
  `domain.HarnessDirector.IsKnown()` is true.

**Dispatch**
- Project with `Director.Harness` unset → existing Hermes/plain-worker path
  runs, completely unaffected.
- Project with it set → Director path spawns one session per project, and a
  later tick resumes rather than re-spawns.
- Existing `commanding`-path tests pass unmodified, proving the new branch is
  additive.

**Config plumbing**
- `--config-json` round-trips a `director` block *and* an
  `agentConfig.command` array without dropping either — the regression test
  for the hand-mirrored-DTO bug root-caused above.
- The new `--director-agent` / `--director-model` flags set the right fields.

**Frontend**
- `WorkCardFocusPanel` renders commander UI for a session with
  `kind === "orchestrator"` and `harness === "director"` — fails before the
  predicate change, passes after.
- Existing Hermes-session tests pass unmodified.

## Out of scope (explicit follow-ups)

- Auto-answer routing for an idle Director (`answer.go` is Hermes-only).
- Stall-nudging an idle Director (`stall_nudge.go` is Hermes-only).
- Excluding the Director from rate-limit auto-switch/kill (`switch_agent.go`).
- Renaming backend Hermes identifiers, or changing the wire-level
  `hermes_unavailable`/`non_hermes_orchestrator` enum values.
- Any change to `commander/orchestrator/*`.
- Adding `--model` forwarding to the `codex`/`opencode`/`kimi`/`qwen`
  adapters (each needs its own CLI-flag research).
- A desktop-UI control for the Director config.
- `CreateWorkCardDialog.tsx:290`'s hardcoded coding-agent chip.
- Updating `internal/skillassets/using-ao/`'s Hermes-specific wording.
- DeepAgents' filesystem/sandbox backends and MCP tool support — the Director
  delegates implementation to worker sessions rather than editing code
  itself, so these are not needed for the MVP.
