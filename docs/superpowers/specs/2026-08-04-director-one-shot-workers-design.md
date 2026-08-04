# Director one-shot workers

Date: 2026-08-04
Status: approved, not yet implemented

## Problem

The Director delegates a card's phases by spawning **interactive** worker
sessions (`ao spawn`) and then talking to them through their terminals:

- `spawn_worker` returns as soon as the pane exists, so the Director never
  learns when the subtask finished except by being told.
- The worker is instructed, in its prompt, to report back with
  `ao send --session <director>` — text injected into the Director's stdin.
- `answer_worker` injects the Director's reply back into the worker's stdin.

That back-channel is the source of the sentinel machinery: the tmux runtime
appends `MESSAGE_END_SENTINEL` to every outbound message
(`backend/internal/adapters/runtime/tmux/commands.go`) and the Director splits
inbound stdin on it (`director/src/inbound.ts`). It exists because large worker
handoffs arrive as many tmux chunks and any timing-based split truncates them.

The Director does not need a conversation with its workers. It needs: run a
subtask, wait, read the result, move the card. That is a one-shot (headless)
invocation.

## Goal

The Director drives workers with one-shot commands. **The frontend is
unchanged** — a worker is still a real session with a worktree, a tmux pane,
a sidebar row, and live terminal output. Only the CLI's mode changes:
headless instead of interactive, so it finishes and exits on its own.

Non-goal: changing card flow. `transition_card` still moves the card through
Todo → Running → Review → Testing → Redo → Done.

Non-goal: removing the Director's own stdin channel. A human must still be
able to steer the Director mid-run with `ao send --session <director>`. With
worker traffic gone, that channel carries only occasional short human
messages, so the existing sentinel split stays as-is and is left untouched.

## Design

### 1. Headless launch commands (backend)

New optional adapter capability, alongside the existing optional interfaces in
`backend/internal/ports/agent.go` (`AgentAuthChecker`, `AgentBinaryResolver`,
`AgentCapabilities`):

```go
// AgentHeadless is the optional capability for adapters whose CLI has a
// one-shot mode: run a single instruction to completion and exit.
type AgentHeadless interface {
    GetHeadlessCommand(ctx context.Context, cfg LaunchConfig) (cmd []string, err error)
}
```

Implemented for the two harnesses the Director uses today:

| harness | argv |
| --- | --- |
| `claude-code` | `claude -p <prompt> --dangerously-skip-permissions [--model <m>]` |
| `codex` | `codex exec <prompt> --yolo [--model <m>]` |

Adapters that do not implement it are rejected at spawn time with a clear
error naming the harness. Silence is not an option: an interactive CLI
launched for a one-shot spawn would never exit and the Director would wait
forever. Adding another harness later is ~5 lines plus a table test.

### 2. One-shot spawn (backend)

- `ports.SpawnConfig` gains `OneShot bool`.
- `session_manager.Manager.Spawn` — when `OneShot` is set, resolve the adapter
  through `AgentHeadless` and use `GetHeadlessCommand` in place of
  `GetLaunchCommand` (`manager.go:350`). Everything else is untouched:
  worktree provisioning, hook install, binary pre-flight, `runtime.Create`,
  `MarkSpawned`, the DB row. That is what keeps the frontend identical.
- Prompt delivery: a one-shot launch always carries its prompt in the argv.
  `deliverPromptAfterStart` must be skipped for one-shot spawns — a headless
  process has no interactive prompt to type into.
- `ports.RuntimeConfig` gains `NotifyExit bool`, set for one-shot spawns.
  - tmux (`buildLaunchCommand`, `tmux.go:465`): insert `; ao session
    mark-exited` after the agent argv and *before* the existing
    `; exec "${SHELL:-/bin/sh}" -i` keep-alive. `POST
    /sessions/{id}/exited` deliberately does not tear down the runtime or
    workspace (`controllers/sessions.go:493`), so the pane and its output
    survive for inspection while the row flips to `terminated`.
  - conpty: return an explicit "one-shot is not supported on the ConPTY
    runtime" error rather than ignoring the flag and hanging.

### 3. `ao spawn --oneshot --wait` (CLI)

- `--oneshot` sets `oneShot` on the spawn request body.
- `--wait` polls `GET /api/v1/sessions/{id}` every 2s until
  `status == "terminated"`, then prints `session <id> exited`. Requires
  `--oneshot`.
- `--wait-timeout` (default 30m) bounds the poll. On expiry the command exits
  non-zero with a message naming the session id, so the Director sees a tool
  failure instead of blocking its own loop indefinitely.

### 4. Director (`director/`)

- `buildSpawnWorkerArgv` appends `--oneshot --wait`. The `spawn_worker` tool
  becomes synchronous: it returns only after the worker has exited.
- Delete the `answer_worker` tool and `buildSendWorkerAnswerArgv`. A headless
  worker never asks a question.
- Worker prompt boilerplate: drop the `ao send --session <director>`
  instructions. Keep the `ao workboard card handoff …` instruction — that is
  now the only result channel, and it is durable. The Director reads it back
  with the existing `show_card` tool.
- `index.ts` stdin handling, `inbound.ts`, and the tmux sentinel: **unchanged**.

## Data flow after the change

```
Director (live session, DeepAgents loop)
  ├─ show_card                 → ao workboard card show   (read state)
  ├─ spawn_worker              → ao spawn --oneshot --wait  (blocks)
  │     └─ worker session: worktree + tmux pane + sidebar row (unchanged)
  │          └─ headless CLI runs, records `ao workboard card handoff`, exits
  │               └─ `ao session mark-exited` → row terminated, pane kept
  ├─ show_card                 → read the handoff
  └─ transition_card           → move the card to the next column
```

Human → Director steering keeps its existing path: `ao send --session
<director>` → tmux stdin + sentinel → `inbound.ts` → new model turn.

## Error handling

- Harness without a headless command: spawn fails with a named error; the
  Director surfaces it as a tool failure and can block the card with a reason.
- Worker exits non-zero: the session is still marked terminated (the
  `mark-exited` call is a separate shell command, so it runs regardless).
  The Director detects failure by reading the card — no handoff recorded means
  the phase did not complete.
- `--wait` timeout: non-zero exit, message names the session, Director decides
  (retry or block).
- Worker never reaches `mark-exited` (killed pane, daemon restart): the
  existing daemon-restart crash recovery still catches the stale row; `--wait`
  bounds the Director's exposure in the meantime.

## Trade-offs accepted

- A one-shot worker cannot be asked a question mid-run; it runs with
  permissions bypassed and either finishes or fails. This is the point of the
  change.
- Worker sessions are short-lived, so they leave the active sidebar quickly.
  Their pane and output remain; list them with
  `ao session ls --include-terminated`.
- The sentinel stays in the codebase for the human steering channel. It is no
  longer on the hot path.

## Testing

- `ports`: compile-time assertions that claude-code and codex satisfy
  `AgentHeadless`.
- Adapter table tests for the two headless argvs, including the model override
  and prompt placement.
- `session_manager`: one-shot spawn uses the headless argv, skips after-start
  prompt delivery, and fails clearly for a harness without the capability.
- tmux: `buildLaunchCommand` with `NotifyExit` emits `mark-exited` before the
  keep-alive exec, and does not when the flag is off.
- CLI: `--wait` returns when the polled status flips to `terminated`, and
  errors on timeout.
- `director/src/tools.test.ts`: `buildSpawnWorkerArgv` carries
  `--oneshot --wait`; the removed `answer_worker` cases are deleted.

## Out of scope

- Windows/ConPTY one-shot support (explicit error for now).
- The non-Director dispatch path in
  `backend/internal/service/workboard/dispatch.go` (Hermes-commanded and plain
  workboard projects) keeps spawning interactive workers.
