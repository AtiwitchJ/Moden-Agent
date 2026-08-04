# Headless Coverage for the Remaining Harnesses Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every shipped agent adapter that has a real non-interactive mode implements `ports.AgentHeadless`, so `ao spawn --oneshot` (and therefore the Director) works with any of them instead of only `claude-code` and `codex`.

**Architecture:** Two shapes of work, decided by what each adapter's `GetLaunchCommand` already does. Most adapters already launch a non-interactive CLI (`aider -m`, `pi --print`, `goose run -t`, `cursor-agent -p`, …) — for those, `GetHeadlessCommand` delegates to the existing launch command through one shared helper, forcing bypass permissions and requiring a prompt. Three adapters (`opencode`, `hermes`, `agy`) launch a TUI but have a separate verified one-shot form, so they get a real second argv builder. Every remaining TUI-only harness is **out of scope**: it goes straight onto a documented exclusion list that a registry test enforces, so `ao spawn --oneshot` rejects it with a clear error instead of hanging.

**Tech Stack:** Go 1.x, the adapter packages under `backend/internal/adapters/agent/`.

**Prerequisite:** `docs/superpowers/plans/2026-08-04-director-one-shot-workers.md` Tasks 1-2 must be merged first — this plan builds on the `ports.AgentHeadless` interface defined there.

## Global Constraints

- Conventional commit messages (`feat:`, `fix:`, `docs:`, `test:`, `chore:`).
- Go tests run from `backend/`: `go test ./...`. Repo-wide lint: `npm run lint`.
- **Never copy a CLI flag from a blog post, a table, or memory.** Before writing any argv, run the binary's own `--help` and confirm. Two entries in the reference table this work started from were already wrong against the installed binaries (`codex exec --yolo` does not exist; `agy --approve all` does not exist). When a binary is not installed, do not guess — follow the "binary unavailable" branch the task gives you.
- A headless command must **exit on its own**. An adapter whose CLI renders a TUI and waits, even with a prompt flag, does not qualify and must go on the exclusion list rather than get a half-working implementation.
- Headless runs bypass approvals: there is nobody to answer a prompt. Set `cfg.Permissions = ports.PermissionModeBypassPermissions` rather than mapping project config.
- A headless launch requires a non-empty prompt. Return an error, never an argv that would open an interactive session.
- No network calls in tests.

---

## Harness Inventory

Ordered exactly as `backend/internal/adapters/agent/registry/registry.go:47-75` registers them.

| harness | current launch shape | group |
| --- | --- | --- |
| `claude-code` | interactive | done (plan 1, Task 1) |
| `codex` | interactive | done (plan 1, Task 2) |
| `opencode` | `opencode [--dangerously-skip-permissions] [--prompt <p>]` (TUI) | B — new argv |
| `grok` | `grok --no-auto-update [--permission-mode <m>] -p <p>` | A — delegate |
| `cursor` | `cursor-agent -p --output-format stream-json --trust … <p>` | A — delegate |
| `qwen` | approval flag + `-p <p>` | A — delegate |
| `copilot` | `copilot [permission flags]`, prompt sent after start | excluded — TUI, out of scope |
| `kimi` | `kimi -p <p>` | A — delegate |
| `droid` | `droid [--settings <path>] … [prompt]` (TUI) | excluded — TUI, out of scope |
| `amp` | `amp [--permission-mode <m>] … [-- <p>]` (TUI) | excluded — TUI, out of scope |
| `agy` | `agy --add-dir <ws> … [--prompt-interactive <p>]` (TUI) | B — new argv |
| `crush` | `crush [--cwd <ws>] [--yolo] [-- <p>]` (TUI) | excluded — TUI, out of scope |
| `aider` | `aider -m <p> … --no-stream --no-pretty` | A — delegate |
| `goose` | `[env GOOSE_MODE=<m>] goose run [--system <t>] -t <p>` | A — delegate |
| `auggie` | `auggie --print … [-- <p>]` | A — delegate |
| `continue` | `cn --print [--auto\|--readonly] <p>` | A — delegate |
| `devin` | `devin [--permission-mode <m>] -p <p>` | A — delegate |
| `cline` | `cline --json … -- <p>` when prompted | A — delegate |
| `kiro` | `kiro-cli chat --no-interactive [trust flags] -- <p>` | A — delegate |
| `kilocode` | `[env KILO_CONFIG_CONTENT=<json>] kilocode [--prompt <p>]` (TUI) | excluded — TUI, out of scope |
| `vibe` | `vibe --trust --output text … -p <p>` | A — delegate |
| `pi` | `pi --print [--append-system-prompt <s>] [<p>]` | A — delegate |
| `autohand` | `autohand [--path <ws>] … [-- <p>]` (TUI) | excluded — TUI, out of scope |
| `command` | runs a user-supplied command | excluded by definition |
| `openclaw` | `openclaw [--cwd <ws>] [--yolo] [-- <p>]` (TUI) | excluded — TUI, out of scope |
| `hermes` | `hermes [--yolo]`, prompt sent after start | B — new argv |
| `director` | AO's own Director loop | excluded by definition |

Verified locally against installed binaries while writing this plan: `claude`, `codex`, `agy`, `cursor-agent`, `hermes`, `opencode`. Every other Group A row is a **claim from the adapter's own doc comment**, not a verified fact — hence the mandatory `--help` step in Task 2.

The seven TUI-only harnesses (`copilot`, `droid`, `amp`, `crush`, `kilocode`, `autohand`, `openclaw`) are deliberately **not** implemented here. None has a confirmed self-terminating mode, and a half-working one would hang a Director waiting on it — worse than an honest rejection. They go on the exclusion list in Task 6 with that reason. Revisit one only when its binary is installed and its `--help` proves a non-interactive mode; that is a new, separately scoped change.

---

## File Structure

- Create: `backend/internal/adapters/agent/headless/headless.go` — the shared `FromLaunch` helper used by every Group A adapter.
- Create: `backend/internal/adapters/agent/headless/headless_test.go`.
- Modify (Group A, one `GetHeadlessCommand` method + one assertion each): `grok/`, `cursor/`, `qwen/`, `kimi/`, `aider/`, `goose/`, `auggie/`, `continueagent/`, `devin/`, `cline/`, `kiro/`, `vibe/`, `pi/`.
- Modify (Group B, a real second argv builder each): `opencode/`, `hermes/`, `agy/`.
- Create: `backend/internal/adapters/agent/registry/headless_test.go` — the drift guard listing every harness with no headless mode.

Not modified by this plan: `copilot/`, `droid/`, `amp/`, `crush/`, `kilocode/`, `autohand/`, `openclaw/`, `command/`, `director/`.

---

### Task 1: Shared `FromLaunch` helper

**Files:**
- Create: `backend/internal/adapters/agent/headless/headless.go`
- Test: `backend/internal/adapters/agent/headless/headless_test.go`

**Interfaces:**
- Consumes: `ports.AgentHeadless`, `ports.LaunchConfig` (plan 1, Task 1).
- Produces: `headless.FromLaunch(ctx context.Context, launch func(context.Context, ports.LaunchConfig) ([]string, error), cfg ports.LaunchConfig) ([]string, error)`.

- [ ] **Step 1: Write the failing test**

Create `backend/internal/adapters/agent/headless/headless_test.go`:

```go
package headless

import (
	"context"
	"reflect"
	"testing"

	"github.com/modernagent/modern-agent/backend/internal/ports"
)

func TestFromLaunchForcesBypassPermissions(t *testing.T) {
	var got ports.LaunchConfig
	launch := func(_ context.Context, cfg ports.LaunchConfig) ([]string, error) {
		got = cfg
		return []string{"agent", "--print", cfg.Prompt}, nil
	}

	argv, err := FromLaunch(context.Background(), launch, ports.LaunchConfig{
		Prompt:      "do it",
		Permissions: ports.PermissionModeAcceptEdits,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Permissions != ports.PermissionModeBypassPermissions {
		t.Fatalf("permissions = %q, want bypassPermissions: a headless run has nobody to answer an approval", got.Permissions)
	}
	if !reflect.DeepEqual(argv, []string{"agent", "--print", "do it"}) {
		t.Fatalf("argv = %#v", argv)
	}
}

func TestFromLaunchRequiresAPrompt(t *testing.T) {
	launch := func(context.Context, ports.LaunchConfig) ([]string, error) {
		t.Fatal("launch must not be called without a prompt")
		return nil, nil
	}

	if _, err := FromLaunch(context.Background(), launch, ports.LaunchConfig{Prompt: "  "}); err == nil {
		t.Fatal("expected an error for a one-shot launch with no prompt")
	}
}

func TestFromLaunchPropagatesLaunchErrors(t *testing.T) {
	launch := func(context.Context, ports.LaunchConfig) ([]string, error) {
		return nil, context.Canceled
	}

	if _, err := FromLaunch(context.Background(), launch, ports.LaunchConfig{Prompt: "do it"}); err == nil {
		t.Fatal("expected the launch error to propagate")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd backend && go test ./internal/adapters/agent/headless/ -v`
Expected: FAIL — package does not exist / `undefined: FromLaunch`.

- [ ] **Step 3: Write the helper**

Create `backend/internal/adapters/agent/headless/headless.go`:

```go
// Package headless adapts agent adapters whose interactive launch command is
// already a non-interactive, self-terminating run (`aider -m`, `pi --print`,
// `goose run -t`, …) onto ports.AgentHeadless.
//
// Those adapters need no second argv builder: their launch command is already
// the one-shot command. What a one-shot spawn additionally requires is that the
// run cannot stall on an approval prompt and cannot degrade into an interactive
// session for want of a prompt. FromLaunch enforces both in one place so a
// dozen adapters do not each repeat the same five lines.
package headless

import (
	"errors"
	"context"
	"strings"

	"github.com/modernagent/modern-agent/backend/internal/ports"
)

// ErrPromptRequired reports a one-shot launch with no instruction to run.
var ErrPromptRequired = errors.New("a one-shot launch requires a prompt")

// FromLaunch builds a headless command by calling the adapter's own launch
// command with approvals bypassed. A headless run has nobody to answer an
// approval prompt, so a mapped permission mode would be a way to hang.
func FromLaunch(
	ctx context.Context,
	launch func(context.Context, ports.LaunchConfig) ([]string, error),
	cfg ports.LaunchConfig,
) ([]string, error) {
	if strings.TrimSpace(cfg.Prompt) == "" {
		return nil, ErrPromptRequired
	}
	cfg.Permissions = ports.PermissionModeBypassPermissions
	return launch(ctx, cfg)
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd backend && go test ./internal/adapters/agent/headless/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/adapters/agent/headless/
git commit -m "feat(agent): add a shared headless helper for already non-interactive adapters"
```

---

### Task 2: Group A — delegate for adapters that already launch non-interactively

**Files:**
- Modify, one method each: `backend/internal/adapters/agent/{grok,cursor,qwen,kimi,aider,goose,auggie,continueagent,devin,cline,kiro,vibe,pi}/*.go`
- Test: one new test file per package, or one appended test per existing `*_test.go`.

**Interfaces:**
- Consumes: `headless.FromLaunch` (Task 1).
- Produces: each listed `*Plugin` satisfies `ports.AgentHeadless`.

- [ ] **Step 1: Verify each CLI actually exits**

For every harness in this task, run its binary's help and confirm the non-interactive flag the adapter already uses is documented as running once and exiting:

```bash
for b in grok cursor-agent qwen kimi aider goose auggie cn devin cline kiro-cli vibe pi; do
  echo "===== $b"; command -v "$b" >/dev/null 2>&1 && "$b" --help 2>&1 | head -40 || echo "NOT INSTALLED"
done
```

Record the result for each. Three outcomes:
- **Installed and the flag runs once then exits** → implement it in this task.
- **Installed but the CLI stays interactive** (renders a TUI, waits for input) → do NOT implement. Add the harness to the exclusion list in Task 6 instead, with the `--help` line that proves it.
- **NOT INSTALLED** → do NOT implement and do NOT guess. Add it to the exclusion list in Task 6 marked `unverified: binary not installed`, so a later session can revisit it with the binary present.

Write the outcome per harness into the commit message body in Step 5.

- [ ] **Step 2: Write the failing test (repeat per implemented harness)**

For each harness you are implementing, append this to that package's existing `*_test.go`, substituting the package's own plugin construction (copy the `&Plugin{resolvedBinary: …}` form the package's existing `TestGetLaunchCommand*` uses) and the binary name:

```go
func TestGetHeadlessCommandRunsOneShot(t *testing.T) {
	p := &Plugin{resolvedBinary: "BINARY"}

	cmd, err := p.GetHeadlessCommand(context.Background(), ports.LaunchConfig{
		Prompt:      "implement the card",
		Permissions: ports.PermissionModeAcceptEdits,
	})
	if err != nil {
		t.Fatal(err)
	}

	joined := strings.Join(cmd, " ")
	if !strings.Contains(joined, "implement the card") {
		t.Fatalf("headless command must carry the prompt: %#v", cmd)
	}

	interactive, err := p.GetLaunchCommand(context.Background(), ports.LaunchConfig{
		Prompt:      "implement the card",
		Permissions: ports.PermissionModeBypassPermissions,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cmd, interactive) {
		t.Fatalf("this adapter's launch command is already one-shot, so the headless command must match it under bypass permissions\nheadless:    %#v\ninteractive: %#v", cmd, interactive)
	}
}

func TestGetHeadlessCommandRejectsEmptyPrompt(t *testing.T) {
	p := &Plugin{resolvedBinary: "BINARY"}

	if _, err := p.GetHeadlessCommand(context.Background(), ports.LaunchConfig{}); err == nil {
		t.Fatal("expected an error for a one-shot launch with no prompt")
	}
}

func TestPluginSatisfiesAgentHeadless(t *testing.T) {
	var _ ports.AgentHeadless = (*Plugin)(nil)
}
```

Add `reflect` and `strings` to that file's imports if missing.

- [ ] **Step 3: Run the tests to verify they fail**

Run: `cd backend && go test ./internal/adapters/agent/... -run 'Headless' -v`
Expected: FAIL — `p.GetHeadlessCommand undefined` in each package you edited.

- [ ] **Step 4: Implement (repeat per implemented harness)**

Add to each package, immediately after its `GetLaunchCommand`:

```go
// GetHeadlessCommand builds the argv for a one-shot run. This adapter's launch
// command is already non-interactive — it runs the prompt to completion and
// exits — so the one-shot form is the same command with approvals bypassed and
// a prompt required. See the headless package for why both are enforced.
func (p *Plugin) GetHeadlessCommand(ctx context.Context, cfg ports.LaunchConfig) (cmd []string, err error) {
	return headless.FromLaunch(ctx, p.GetLaunchCommand, cfg)
}
```

Add the import `"github.com/modernagent/modern-agent/backend/internal/adapters/agent/headless"` and, next to the package's existing `var _ ports.Agent = (*Plugin)(nil)`:

```go
var _ ports.AgentHeadless = (*Plugin)(nil)
```

- [ ] **Step 5: Run the tests to verify they pass, then commit**

Run: `cd backend && go test ./internal/adapters/agent/...`
Expected: PASS.

```bash
git add backend/internal/adapters/agent/
git commit -m "feat(agent): support one-shot runs on the already non-interactive harnesses

Verified with each CLI's own --help:
- implemented: <list>
- interactive only, excluded: <list>
- not installed, unverified: <list>"
```

---

### Task 3: opencode headless run

**Files:**
- Modify: `backend/internal/adapters/agent/opencode/opencode.go`
- Test: `backend/internal/adapters/agent/opencode/opencode_test.go`

**Interfaces:**
- Consumes: `ports.AgentHeadless`.
- Produces: `*opencode.Plugin` satisfies `ports.AgentHeadless`; argv shape `opencode run --auto -- <prompt>`.

- [ ] **Step 1: Re-verify the flags**

Run: `opencode run --help`
Expected to confirm: the `run` subcommand takes the message positionally, and `--auto` is "auto-approve permissions that are not explicitly denied". Note that **`-p` on opencode is `--password`, not print** — do not use it. If the installed version differs, adjust the argv below to match what `--help` actually shows.

- [ ] **Step 2: Write the failing test**

Append to `backend/internal/adapters/agent/opencode/opencode_test.go`:

```go
func TestGetHeadlessCommandRunsSubcommand(t *testing.T) {
	p := &Plugin{resolvedBinary: "opencode"}

	cmd, err := p.GetHeadlessCommand(context.Background(), ports.LaunchConfig{Prompt: "implement the card"})
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"opencode", "run", "--auto", "--", "implement the card"}
	if !reflect.DeepEqual(cmd, want) {
		t.Fatalf("unexpected command\nwant: %#v\n got: %#v", want, cmd)
	}
}

func TestGetHeadlessCommandRejectsEmptyPrompt(t *testing.T) {
	p := &Plugin{resolvedBinary: "opencode"}

	if _, err := p.GetHeadlessCommand(context.Background(), ports.LaunchConfig{}); err == nil {
		t.Fatal("expected an error for a one-shot launch with no prompt")
	}
}

func TestPluginSatisfiesAgentHeadless(t *testing.T) {
	var _ ports.AgentHeadless = (*Plugin)(nil)
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `cd backend && go test ./internal/adapters/agent/opencode/ -run Headless -v`
Expected: FAIL — `p.GetHeadlessCommand undefined`.

- [ ] **Step 4: Implement**

Add after `GetLaunchCommand` in `opencode.go`:

```go
// GetHeadlessCommand builds the argv for a one-shot opencode run:
//
//	opencode run --auto -- <prompt>
//
// `run` is opencode's non-interactive subcommand: it sends one message and
// exits, unlike the bare `opencode` launch which opens the TUI. `--auto`
// auto-approves every permission that is not explicitly denied, which a
// headless run needs because nothing can answer a prompt. Note that opencode's
// `-p` is --password, not print — the interactive launch's `--prompt` flag has
// no place here.
func (p *Plugin) GetHeadlessCommand(ctx context.Context, cfg ports.LaunchConfig) (cmd []string, err error) {
	if strings.TrimSpace(cfg.Prompt) == "" {
		return nil, fmt.Errorf("opencode: a one-shot launch requires a prompt")
	}
	binary, err := p.opencodeBinary(ctx)
	if err != nil {
		return nil, err
	}
	return []string{binary, "run", "--auto", "--", cfg.Prompt}, nil
}
```

Use the package's actual binary-resolver method name — check it with `grep -n "func (p \*Plugin) .*[Bb]inary" backend/internal/adapters/agent/opencode/opencode.go`. Add the `var _ ports.AgentHeadless = (*Plugin)(nil)` assertion.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd backend && go test ./internal/adapters/agent/opencode/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/adapters/agent/opencode/
git commit -m "feat(agent): add a headless one-shot launch capability for opencode"
```

---

### Task 4: hermes headless run

**Files:**
- Modify: `backend/internal/adapters/agent/hermes/hermes.go`
- Test: `backend/internal/adapters/agent/hermes/hermes_test.go`

**Interfaces:**
- Consumes: `ports.AgentHeadless`.
- Produces: `*hermes.Plugin` satisfies `ports.AgentHeadless`; argv shape `hermes chat -Q --yolo -q <prompt>`.

This adapter matters more than its position suggests: `hermes` is in the default reviewer and testing chains (`backend/internal/domain/workboard.go:410-414`), so without it the Director hits an unsupported harness on fallback.

- [ ] **Step 1: Re-verify the flags**

Run: `hermes chat --help`
Expected to confirm: `-q/--query` is "Single query (non-interactive mode)", `-Q/--quiet` suppresses the banner for programmatic use, and `--yolo` bypasses approval prompts.

- [ ] **Step 2: Write the failing test**

Append to `backend/internal/adapters/agent/hermes/hermes_test.go`:

```go
func TestGetHeadlessCommandRunsSingleQuery(t *testing.T) {
	p := &Plugin{resolvedBinary: "hermes"}

	cmd, err := p.GetHeadlessCommand(context.Background(), ports.LaunchConfig{Prompt: "implement the card"})
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"hermes", "chat", "-Q", "--yolo", "-q", "implement the card"}
	if !reflect.DeepEqual(cmd, want) {
		t.Fatalf("unexpected command\nwant: %#v\n got: %#v", want, cmd)
	}
}

func TestGetHeadlessCommandRejectsEmptyPrompt(t *testing.T) {
	p := &Plugin{resolvedBinary: "hermes"}

	if _, err := p.GetHeadlessCommand(context.Background(), ports.LaunchConfig{}); err == nil {
		t.Fatal("expected an error for a one-shot launch with no prompt")
	}
}

func TestPluginSatisfiesAgentHeadless(t *testing.T) {
	var _ ports.AgentHeadless = (*Plugin)(nil)
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `cd backend && go test ./internal/adapters/agent/hermes/ -run Headless -v`
Expected: FAIL — `p.GetHeadlessCommand undefined`.

- [ ] **Step 4: Implement**

Add after `GetLaunchCommand` in `hermes.go`:

```go
// GetHeadlessCommand builds the argv for a one-shot hermes run:
//
//	hermes chat -Q --yolo -q <prompt>
//
// `chat -q` is hermes's single-query mode: it answers once and exits, which is
// why this adapter's PromptDeliveryAfterStart strategy (needed for the TUI
// launch, which takes no prompt at all) does not apply here — the prompt rides
// the command. `-Q` suppresses the banner so the output is programmatic, and
// `--yolo` bypasses the approval prompts a headless run cannot answer.
func (p *Plugin) GetHeadlessCommand(ctx context.Context, cfg ports.LaunchConfig) (cmd []string, err error) {
	if strings.TrimSpace(cfg.Prompt) == "" {
		return nil, fmt.Errorf("hermes: a one-shot launch requires a prompt")
	}
	binary, err := p.hermesBinary(ctx)
	if err != nil {
		return nil, err
	}
	return []string{binary, "chat", "-Q", "--yolo", "-q", cfg.Prompt}, nil
}
```

Use the package's actual binary-resolver method name (`grep -n "func (p \*Plugin) .*[Bb]inary" backend/internal/adapters/agent/hermes/hermes.go`). Add the `var _ ports.AgentHeadless = (*Plugin)(nil)` assertion.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd backend && go test ./internal/adapters/agent/hermes/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/adapters/agent/hermes/
git commit -m "feat(agent): add a headless one-shot launch capability for hermes"
```

---

### Task 5: agy headless run

**Files:**
- Modify: `backend/internal/adapters/agent/agy/agy.go`
- Test: `backend/internal/adapters/agent/agy/agy_test.go`

**Interfaces:**
- Consumes: `ports.AgentHeadless`.
- Produces: `*agy.Plugin` satisfies `ports.AgentHeadless`; argv shape `agy --add-dir <ws> --print --dangerously-skip-permissions <prompt>`.

- [ ] **Step 1: Re-verify the flags**

Run: `agy --help`
Expected to confirm: `--print` / `-p` is "Run a single prompt non-interactively and print the response", and `--dangerously-skip-permissions` is "Auto-approve all tool permission requests without prompting". The reference table's `--approve all` does **not** exist — do not use it. Also note `--print-timeout` (default 5m0s): if a card phase can exceed five minutes, pass a larger value.

- [ ] **Step 2: Write the failing test**

Append to `backend/internal/adapters/agent/agy/agy_test.go`:

```go
func TestGetHeadlessCommandRunsPrintMode(t *testing.T) {
	p := &Plugin{resolvedBinary: "agy"}

	cmd, err := p.GetHeadlessCommand(context.Background(), ports.LaunchConfig{
		WorkspacePath: "/tmp/ws",
		Prompt:        "implement the card",
	})
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		"agy", "--add-dir", "/tmp/ws",
		"--print", "--dangerously-skip-permissions", "--print-timeout", "30m",
		"implement the card",
	}
	if !reflect.DeepEqual(cmd, want) {
		t.Fatalf("unexpected command\nwant: %#v\n got: %#v", want, cmd)
	}
}

func TestGetHeadlessCommandRejectsEmptyPrompt(t *testing.T) {
	p := &Plugin{resolvedBinary: "agy"}

	if _, err := p.GetHeadlessCommand(context.Background(), ports.LaunchConfig{}); err == nil {
		t.Fatal("expected an error for a one-shot launch with no prompt")
	}
}

func TestPluginSatisfiesAgentHeadless(t *testing.T) {
	var _ ports.AgentHeadless = (*Plugin)(nil)
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `cd backend && go test ./internal/adapters/agent/agy/ -run Headless -v`
Expected: FAIL — `p.GetHeadlessCommand undefined`.

- [ ] **Step 4: Implement**

Add after `GetLaunchCommand` in `agy.go`:

```go
// GetHeadlessCommand builds the argv for a one-shot Agy run:
//
//	agy --add-dir <workspace> --print --dangerously-skip-permissions \
//	    --print-timeout 30m <prompt>
//
// `--print` runs a single prompt non-interactively and prints the response,
// unlike the interactive launch's `--prompt-interactive`.
// `--dangerously-skip-permissions` auto-approves tool requests, which a
// headless run needs because nothing can answer them. `--print-timeout`
// defaults to five minutes, well under a real card phase, so it is raised to
// match the caller's own one-shot wait budget.
func (p *Plugin) GetHeadlessCommand(ctx context.Context, cfg ports.LaunchConfig) (cmd []string, err error) {
	if strings.TrimSpace(cfg.Prompt) == "" {
		return nil, fmt.Errorf("agy: a one-shot launch requires a prompt")
	}
	binary, err := p.agyBinary(ctx)
	if err != nil {
		return nil, err
	}
	cmd = []string{binary}
	if cfg.WorkspacePath != "" {
		cmd = append(cmd, "--add-dir", cfg.WorkspacePath)
	}
	cmd = append(cmd, "--print", "--dangerously-skip-permissions", "--print-timeout", "30m")
	return append(cmd, cfg.Prompt), nil
}
```

Use the package's actual binary-resolver method name (`grep -n "func (p \*Plugin) .*[Bb]inary" backend/internal/adapters/agent/agy/agy.go`). Add the `var _ ports.AgentHeadless = (*Plugin)(nil)` assertion.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd backend && go test ./internal/adapters/agent/agy/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/adapters/agent/agy/
git commit -m "feat(agent): add a headless one-shot launch capability for agy"
```

---

### Task 6: The exclusion list + registry drift guard

**Files:**
- Create: `backend/internal/adapters/agent/registry/headless_test.go`

**Interfaces:**
- Consumes: everything above.
- Produces: `noHeadlessHarnesses` — the documented exclusion list, enforced by a test over the real registry.

The seven TUI-only harnesses are **out of scope for this plan** — do not implement `GetHeadlessCommand` for any of them here. They are recorded, not fixed. An excluded harness fails `ao spawn --oneshot` with the clear `ErrOneShotUnsupported` error from plan 1, which is the correct behavior: a Director that gets an honest rejection can block the card with a reason, while a half-working headless command would leave it waiting on a process that never exits.

- [ ] **Step 1: Write the drift guard test**

Create `backend/internal/adapters/agent/registry/headless_test.go`. The `noHeadlessHarnesses` entries below are the seven out-of-scope TUI harnesses plus the two excluded by definition; add to them any Group A harness that Task 2's triage left unimplemented (interactive-only, or binary not installed), copying the reason from that task's commit body:

```go
package registry

import (
	"testing"

	"github.com/modernagent/modern-agent/backend/internal/ports"
)

// noHeadlessHarnesses lists every registered harness that deliberately has no
// one-shot command, with the reason. A harness here cannot be spawned with
// `ao spawn --oneshot` and cannot be used for a Director-delegated card phase.
//
// This list is the contract: adding a harness to the registry without either
// implementing ports.AgentHeadless or naming it here fails the test below, so
// a new adapter cannot silently become un-delegatable.
var noHeadlessHarnesses = map[string]string{
	"command":  "runs a user-supplied command; one-shot semantics are the caller's, not ours",
	"director": "AO's own Director loop, which drives cards rather than executing one",

	// TUI-only launches with no confirmed self-terminating mode. Out of scope
	// deliberately: a half-working headless command would hang a Director
	// waiting on a process that never exits, which is worse than this honest
	// rejection. Revisit one only with its binary installed and its --help
	// showing a non-interactive mode.
	"copilot":  "interactive TUI; the adapter delivers its prompt after start",
	"droid":    "interactive TUI launch",
	"amp":      "interactive TUI launch",
	"crush":    "interactive TUI launch",
	"kilocode": "interactive TUI launch",
	"autohand": "interactive TUI launch",
	"openclaw": "interactive TUI launch",

	// Add any Group A harness Task 2 left unimplemented, with its finding, e.g.:
	// "kimi": "unverified: binary not installed",
}

func TestEveryHarnessIsHeadlessOrDocumented(t *testing.T) {
	for _, a := range Constructors(t.TempDir()) {
		id := a.Manifest().ID
		agent, ok := a.(ports.Agent)
		if !ok {
			continue
		}
		_, headless := agent.(ports.AgentHeadless)
		reason, excluded := noHeadlessHarnesses[id]

		switch {
		case headless && excluded:
			t.Errorf("harness %q implements AgentHeadless but is still listed as excluded (%q); remove it from noHeadlessHarnesses", id, reason)
		case !headless && !excluded:
			t.Errorf("harness %q has no headless command and no documented reason; implement ports.AgentHeadless or add it to noHeadlessHarnesses", id)
		}
	}
}
```

- [ ] **Step 2: Run the guard to verify it reflects reality**

Run: `cd backend && go test ./internal/adapters/agent/registry/ -run Headless -v`
Expected: PASS once every registered harness either implements the interface or carries a reason. A failure here names exactly which harness is unaccounted for — fix the list or the adapter, not the test's logic.

- [ ] **Step 3: Commit**

```bash
git add backend/internal/adapters/agent/
git commit -m "feat(agent): document and enforce which harnesses support one-shot runs"
```

---

### Task 7: Whole-repo verification

**Files:** none modified.

- [ ] **Step 1: Run the full backend suite**

Run: `cd backend && go test ./...`
Expected: PASS.

- [ ] **Step 2: Run the repo lint**

Run: `npm run lint`
Expected: PASS.

- [ ] **Step 3: Spawn one-shot on each newly supported harness**

For every harness implemented in this plan whose binary is installed, against a registered project:

```bash
ao spawn --project <projectId> --agent <harness> --name oneshot-<harness> \
  --oneshot --wait --wait-timeout 5m \
  --prompt "Print the repository's top-level directory names and stop."
```

Expected per harness: the command blocks, then prints `session <id> exited`. If one blocks until the timeout, that harness's CLI did not exit — remove its `GetHeadlessCommand` and move it to `noHeadlessHarnesses` with that finding as the reason. A harness that hangs is worse than one that is honestly excluded.

- [ ] **Step 4: Confirm the default agent chains resolve**

Check `backend/internal/domain/workboard.go:410-414`: every harness in `DefaultReviewerAgents` and `DefaultTestingAgents` (`claude-code`, `codex`, `opencode`, `hermes`) must now implement `AgentHeadless`. Run:

```bash
cd backend && go test ./internal/adapters/agent/registry/ -run Headless -v
```

and confirm none of those four appear in `noHeadlessHarnesses`. If one does, the Director will fail on chain fallback — fix that harness before calling this plan done.

- [ ] **Step 5: Commit any fixes**

```bash
git add -A
git commit -m "fix(agent): <what the one-shot spawn runs exposed>"
```

Skip if nothing needed fixing.

---

## Self-Review

**Spec coverage:** this plan has no separate spec — it extends `docs/superpowers/specs/2026-08-04-director-one-shot-workers-design.md` §1, whose stated follow-up was "adding another harness later is ~5 lines plus a table test". Every registered harness in `registry.go:47-75` appears in the inventory table and is routed to exactly one of: done in plan 1 (2), delegate (13), new argv (3), excluded as TUI-only and out of scope (7), excluded by definition (2). 2+13+3+7+2 = 27, matching the registry. The four harnesses in the default reviewer/testing chains (`claude-code`, `codex`, `opencode`, `hermes`) are all in the implemented set, which Task 7 Step 4 re-checks.

**Placeholder scan:** the only intentionally open content is the "add any Group A harness Task 2 left unimplemented" line inside `noHeadlessHarnesses`, which cannot be written before Task 2's `--help` triage runs — Task 6 Step 1 says exactly what fills it and shows the entry format. Task 2 does not name argvs for uninstalled binaries by design; guessing them is the failure mode this plan exists to prevent, and the task gives the exact `--help` command and the three-way decision that replaces guessing.

**Type consistency:** `GetHeadlessCommand(ctx context.Context, cfg ports.LaunchConfig) (cmd []string, err error)` is identical in Tasks 2-5 and matches plan 1's `ports.AgentHeadless`. `headless.FromLaunch` keeps one signature across Tasks 1 and 2. `noHeadlessHarnesses` is a `map[string]string` keyed by manifest id in both its declaration and the test that reads it.

**Scope check:** the seven TUI-only harnesses were dropped from implementation on purpose. Task 6 records them with a reason and the guard test enforces that they stay recorded; picking one up later is a separate change that starts with installing its binary and reading its `--help`.
