# Director One-Shot Workers Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let the Director delegate a card's phases with one-shot (headless) worker runs that finish and exit on their own, while every worker remains a normal AO session with a worktree, a tmux pane, and a sidebar row.

**Architecture:** A new optional adapter capability (`ports.AgentHeadless`) supplies a headless argv per harness. `ports.SpawnConfig.OneShot` selects it in `session_manager.Manager.Spawn`; everything else in the spawn path is untouched, which is what keeps the frontend unchanged. `ports.RuntimeConfig.NotifyExit` makes the tmux launch string call `ao session mark-exited` after the agent exits, so the session row flips to terminated without tearing down the pane. `ao spawn --oneshot --wait` polls until that happens, which turns the Director's `spawn_worker` tool into a blocking call and removes the need for any worker-to-Director back-channel.

**Tech Stack:** Go 1.x (backend, CLI, adapters), TypeScript + vitest (`director/`), cobra (CLI), chi (HTTP), tmux runtime.

**Spec:** `docs/superpowers/specs/2026-08-04-director-one-shot-workers-design.md`

## Global Constraints

- Conventional commit messages (`feat:`, `fix:`, `docs:`, `test:`, `chore:`).
- Go tests run from `backend/`: `go test ./...`. Repo-wide lint: `npm run lint`.
- Director tests run from `director/`: `npx vitest run`.
- API contract is code-first. After editing `backend/internal/httpd/controllers/dto.go`, run `npm run api` from the repo root and commit `backend/internal/httpd/apispec/openapi.yaml` and `frontend/src/api/schema.ts` alongside the Go change.
- All app state resolves under `~/.ao` only. No new writes outside it.
- Do not add network calls to tests; use `httptest` and the existing fakes.
- The Director's own stdin channel, `director/src/inbound.ts`, and the tmux `MESSAGE_END_SENTINEL` are **out of scope** — do not modify them.
- The non-Director dispatch path in `backend/internal/service/workboard/dispatch.go` is **out of scope** — it keeps spawning interactive workers.

---

## File Structure

**Backend — ports (contracts)**
- Modify `backend/internal/ports/agent.go` — add the `AgentHeadless` optional capability interface.
- Modify `backend/internal/ports/session.go` — add `SpawnConfig.OneShot`.
- Modify `backend/internal/ports/outbound.go:90-95` — add `RuntimeConfig.NotifyExit`.

**Backend — adapters**
- Modify `backend/internal/adapters/agent/claudecode/claudecode.go` — implement `GetHeadlessCommand`.
- Modify `backend/internal/adapters/agent/codex/codex.go` — implement `GetHeadlessCommand`.
- Modify `backend/internal/adapters/runtime/tmux/tmux.go:465-498` — emit `ao session mark-exited` when `NotifyExit` is set.
- Modify `backend/internal/adapters/runtime/conpty/runtime.go:60-70` — reject `NotifyExit` explicitly.

**Backend — session manager**
- Modify `backend/internal/session_manager/manager.go` — reject a one-shot spawn for an adapter without the capability, select the headless argv, set `NotifyExit`, skip after-start prompt delivery.

**Backend — API + CLI**
- Modify `backend/internal/httpd/controllers/dto.go:518-530` — add `oneShot` to `SpawnSessionRequest`.
- Modify `backend/internal/httpd/controllers/sessions.go:151` — forward it into `ports.SpawnConfig`.
- Modify `backend/internal/cli/spawn.go` — `--oneshot`, `--wait`, `--wait-timeout`, and the poll loop.

**Director**
- Modify `director/src/tools.ts` — `buildSpawnWorkerArgv` gains the one-shot flags; delete `buildSendWorkerAnswerArgv`.
- Modify `director/src/index.ts` — delete the `answer_worker` tool, rewrite the worker prompt boilerplate.
- Modify `director/src/tools.test.ts` — update/remove the affected cases.

---

### Task 1: `AgentHeadless` capability + Claude Code headless command

**Files:**
- Modify: `backend/internal/ports/agent.go:66-71` (insert after `AgentAuthChecker`)
- Modify: `backend/internal/adapters/agent/claudecode/claudecode.go`
- Test: `backend/internal/adapters/agent/claudecode/claudecode_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `ports.AgentHeadless` with method `GetHeadlessCommand(ctx context.Context, cfg ports.LaunchConfig) (cmd []string, err error)`. `*claudecode.Plugin` satisfies it.

- [ ] **Step 1: Write the failing test**

Append to `backend/internal/adapters/agent/claudecode/claudecode_test.go`:

```go
func TestGetHeadlessCommandRunsOneShot(t *testing.T) {
	p := &Plugin{resolvedBinary: "claude"}

	cmd, err := p.GetHeadlessCommand(context.Background(), ports.LaunchConfig{
		AgentSessionID: "11111111-2222-3333-4444-555555555555",
		Prompt:         "-implement the card",
	})
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		"claude",
		"--print",
		"--session-id", "11111111-2222-3333-4444-555555555555",
		"--dangerously-skip-permissions",
		"--", "-implement the card",
	}
	if !reflect.DeepEqual(cmd, want) {
		t.Fatalf("unexpected command\nwant: %#v\n got: %#v", want, cmd)
	}
}

func TestGetHeadlessCommandCarriesModelAndSystemPrompt(t *testing.T) {
	p := &Plugin{resolvedBinary: "claude"}

	cmd, err := p.GetHeadlessCommand(context.Background(), ports.LaunchConfig{
		Config:       ports.AgentConfig{Model: "claude-opus-4-5"},
		SystemPrompt: "You are a worker.",
		Prompt:       "do it",
	})
	if err != nil {
		t.Fatal(err)
	}

	if !containsSubsequence(cmd, []string{"--model", "claude-opus-4-5"}) {
		t.Fatalf("command missing model override: %#v", cmd)
	}
	if !containsSubsequence(cmd, []string{"--append-system-prompt", "You are a worker."}) {
		t.Fatalf("command missing system prompt: %#v", cmd)
	}
}

func TestGetHeadlessCommandRejectsEmptyPrompt(t *testing.T) {
	p := &Plugin{resolvedBinary: "claude"}

	if _, err := p.GetHeadlessCommand(context.Background(), ports.LaunchConfig{}); err == nil {
		t.Fatal("expected an error for a one-shot launch with no prompt")
	}
}

func TestPluginSatisfiesAgentHeadless(t *testing.T) {
	var _ ports.AgentHeadless = (*Plugin)(nil)
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd backend && go test ./internal/adapters/agent/claudecode/ -run 'Headless' -v`
Expected: FAIL — `p.GetHeadlessCommand undefined` / `undefined: ports.AgentHeadless`.

- [ ] **Step 3: Add the port interface**

In `backend/internal/ports/agent.go`, insert after the `AgentAuthChecker` block (line 65):

```go
// AgentHeadless is the optional capability for adapters whose CLI has a
// one-shot mode: run a single instruction to completion and exit, with no
// interactive TUI and no approval prompts. AO uses it for supervised subtasks
// (the Director delegating a card phase) where the caller waits for the run to
// finish. Adapters that do not implement it cannot be spawned one-shot: their
// interactive launch would never exit and the caller would wait forever.
type AgentHeadless interface {
	GetHeadlessCommand(ctx context.Context, cfg LaunchConfig) (cmd []string, err error)
}
```

- [ ] **Step 4: Implement the Claude Code headless command**

In `backend/internal/adapters/agent/claudecode/claudecode.go`, add after `GetLaunchCommand` (which ends around line 179):

```go
// GetHeadlessCommand builds the argv for a one-shot Claude Code run. Shape:
//
//	claude --print \
//	       [--session-id <uuid>] \
//	       --dangerously-skip-permissions \
//	       [--model <model>] \
//	       [--append-system-prompt <system prompt>] \
//	       -- <prompt>
//
// --print is Claude's non-interactive mode: it runs the prompt to completion,
// writes the result to stdout, and exits. Permissions are bypassed outright
// rather than mapped from config: a headless run has no one to answer an
// approval prompt, and a run that stalls on one would hang the caller waiting
// for it. The prompt is required and passed after `--` so a prompt beginning
// with "-" is not mistaken for a flag.
func (p *Plugin) GetHeadlessCommand(ctx context.Context, cfg ports.LaunchConfig) (cmd []string, err error) {
	if err := cfg.Config.Validate(); err != nil {
		return nil, fmt.Errorf("claude-code: %w", err)
	}
	if strings.TrimSpace(cfg.Prompt) == "" {
		return nil, fmt.Errorf("claude-code: a one-shot launch requires a prompt")
	}

	binary, err := p.claudeBinary(ctx)
	if err != nil {
		return nil, err
	}

	cmd = []string{binary, "--print"}
	sessionID := strings.TrimSpace(cfg.AgentSessionID)
	if sessionID == "" && cfg.SessionID != "" {
		sessionID = claudeSessionUUID(cfg.SessionID)
	}
	if sessionID != "" {
		cmd = append(cmd, "--session-id", sessionID)
	}
	cmd = append(cmd, "--dangerously-skip-permissions")

	if model := strings.TrimSpace(cfg.Config.Model); model != "" {
		cmd = append(cmd, "--model", model)
	}

	systemPrompt, err := resolveSystemPrompt(cfg)
	if err != nil {
		return nil, err
	}
	if systemPrompt != "" {
		cmd = append(cmd, "--append-system-prompt", systemPrompt)
	}

	return append(cmd, "--", cfg.Prompt), nil
}
```

Then add the compile-time assertion next to the existing ones (near `var _ ports.AgentAuthChecker = (*Plugin)(nil)`, around line 63):

```go
var _ ports.AgentHeadless = (*Plugin)(nil)
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd backend && go test ./internal/adapters/agent/claudecode/ ./internal/ports/ -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/ports/agent.go backend/internal/adapters/agent/claudecode/
git commit -m "feat(agent): add a headless one-shot launch capability for claude-code"
```

---

### Task 2: Codex headless command

**Files:**
- Modify: `backend/internal/adapters/agent/codex/codex.go`
- Test: `backend/internal/adapters/agent/codex/codex_test.go`

**Interfaces:**
- Consumes: `ports.AgentHeadless` from Task 1.
- Produces: `*codex.Plugin` satisfies `ports.AgentHeadless`.

- [ ] **Step 1: Write the failing test**

Append to `backend/internal/adapters/agent/codex/codex_test.go`:

```go
func TestGetHeadlessCommandRunsExecSubcommand(t *testing.T) {
	p := &Plugin{resolvedBinary: "codex"}

	cmd, err := p.GetHeadlessCommand(context.Background(), ports.LaunchConfig{
		Prompt: "-implement the card",
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(cmd) < 2 || cmd[0] != "codex" || cmd[1] != "exec" {
		t.Fatalf("command must start with `codex exec`: %#v", cmd)
	}
	if cmd[len(cmd)-2] != "--" || cmd[len(cmd)-1] != "-implement the card" {
		t.Fatalf("prompt must be passed last after `--`: %#v", cmd)
	}
	for _, want := range []string{"--dangerously-bypass-approvals-and-sandbox", "--dangerously-bypass-hook-trust"} {
		if !containsFlag(cmd, want) {
			t.Fatalf("command missing %s: %#v", want, cmd)
		}
	}
}

func TestGetHeadlessCommandCarriesModelOverride(t *testing.T) {
	p := &Plugin{resolvedBinary: "codex"}

	cmd, err := p.GetHeadlessCommand(context.Background(), ports.LaunchConfig{
		Config: ports.AgentConfig{Model: "gpt-5-codex"},
		Prompt: "do it",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !containsSubsequence(cmd, []string{"--model", "gpt-5-codex"}) {
		t.Fatalf("command missing model override: %#v", cmd)
	}
}

func TestGetHeadlessCommandRejectsEmptyPrompt(t *testing.T) {
	p := &Plugin{resolvedBinary: "codex"}

	if _, err := p.GetHeadlessCommand(context.Background(), ports.LaunchConfig{}); err == nil {
		t.Fatal("expected an error for a one-shot launch with no prompt")
	}
}

func TestCodexPluginSatisfiesAgentHeadless(t *testing.T) {
	var _ ports.AgentHeadless = (*Plugin)(nil)
}

func containsFlag(cmd []string, flag string) bool {
	for _, arg := range cmd {
		if arg == flag {
			return true
		}
	}
	return false
}
```

Note: `containsSubsequence` may already exist in this package. Run `grep -n "func containsSubsequence\|func containsFlag" backend/internal/adapters/agent/codex/*_test.go` first and drop whichever helper is already defined rather than redeclaring it.

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd backend && go test ./internal/adapters/agent/codex/ -run 'Headless' -v`
Expected: FAIL — `p.GetHeadlessCommand undefined`.

- [ ] **Step 3: Implement the Codex headless command**

In `backend/internal/adapters/agent/codex/codex.go`, add after `GetLaunchCommand` (ends line 93):

```go
// GetHeadlessCommand builds the argv for a one-shot Codex run. Shape:
//
//	codex exec -c check_for_update_on_startup=false \
//	           --dangerously-bypass-hook-trust \
//	           --dangerously-bypass-approvals-and-sandbox \
//	           [--model <model>] \
//	           [-c model_instructions_file=... | -c developer_instructions=...] \
//	           -- <prompt>
//
// `codex exec` is Codex's non-interactive mode: it runs the prompt to
// completion and exits. Approvals and the sandbox are bypassed outright rather
// than mapped from config — a headless run has no one to answer an approval
// prompt, and a run that stalls on one would hang the caller waiting for it.
// The TUI-only flags from GetLaunchCommand (rate-limit nudge, terminal
// compatibility, workspace trust) are deliberately omitted: `exec` renders no
// TUI. The prompt is required and passed after `--` so a leading "-" is not
// read as a flag.
func (p *Plugin) GetHeadlessCommand(ctx context.Context, cfg ports.LaunchConfig) (cmd []string, err error) {
	if strings.TrimSpace(cfg.Prompt) == "" {
		return nil, fmt.Errorf("codex: a one-shot launch requires a prompt")
	}

	binary, err := p.codexBinary(ctx)
	if err != nil {
		return nil, err
	}

	cmd = []string{binary, "exec"}
	appendNoUpdateCheckFlag(&cmd)
	appendHookTrustBypassFlag(&cmd)
	appendSessionHookFlags(&cmd)
	cmd = append(cmd, "--dangerously-bypass-approvals-and-sandbox")

	if model := strings.TrimSpace(cfg.Config.Model); model != "" {
		cmd = append(cmd, "--model", model)
	}

	if cfg.SystemPromptFile != "" {
		cmd = append(cmd, "-c", "model_instructions_file="+cfg.SystemPromptFile)
	} else if cfg.SystemPrompt != "" {
		cmd = append(cmd, "-c", "developer_instructions="+codexTOMLConfigString(cfg.SystemPrompt))
	}

	return append(cmd, "--", cfg.Prompt), nil
}
```

Add the compile-time assertion next to the plugin's existing `var _ ports.Agent = (*Plugin)(nil)` line:

```go
var _ ports.AgentHeadless = (*Plugin)(nil)
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd backend && go test ./internal/adapters/agent/codex/ -v`
Expected: PASS. If `codex exec` rejects `appendSessionHookFlags`' config keys in the installed CLI version, drop that one call and its assertion — the hooks are an activity-reporting nicety, not a requirement for a one-shot run.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/adapters/agent/codex/
git commit -m "feat(agent): add a headless one-shot launch capability for codex"
```

---

### Task 3: Runtime reports a one-shot agent's exit

**Files:**
- Modify: `backend/internal/ports/outbound.go:90-95`
- Modify: `backend/internal/adapters/runtime/tmux/tmux.go:465-498`
- Modify: `backend/internal/adapters/runtime/conpty/runtime.go:60-70`
- Test: `backend/internal/adapters/runtime/tmux/tmux_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `ports.RuntimeConfig.NotifyExit bool`. When true, the tmux launch string runs `ao session mark-exited` after the agent argv and before the keep-alive shell.

- [ ] **Step 1: Write the failing test**

Append to `backend/internal/adapters/runtime/tmux/tmux_test.go`:

```go
func TestBuildLaunchCommandReportsExitForOneShot(t *testing.T) {
	got := buildLaunchCommand(ports.RuntimeConfig{
		Argv:       []string{"claude", "--print", "--", "do it"},
		Env:        map[string]string{"AO_SESSION_ID": "demo-1"},
		NotifyExit: true,
	})

	if !strings.Contains(got, "; ao session mark-exited; exec ") {
		t.Fatalf("one-shot launch must report its exit before the keep-alive shell: %s", got)
	}
	agentIdx := strings.Index(got, "'claude'")
	reportIdx := strings.Index(got, "ao session mark-exited")
	if agentIdx == -1 || reportIdx == -1 || agentIdx > reportIdx {
		t.Fatalf("mark-exited must run after the agent argv: %s", got)
	}
}

func TestBuildLaunchCommandOmitsExitReportByDefault(t *testing.T) {
	got := buildLaunchCommand(ports.RuntimeConfig{
		Argv: []string{"claude"},
		Env:  map[string]string{"AO_SESSION_ID": "demo-1"},
	})

	if strings.Contains(got, "mark-exited") {
		t.Fatalf("an interactive launch must not report an exit: %s", got)
	}
}
```

Make sure the file imports `strings` and `ports`; add them if the test file does not already.

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd backend && go test ./internal/adapters/runtime/tmux/ -run 'BuildLaunchCommand.*Exit' -v`
Expected: FAIL — `unknown field NotifyExit in struct literal`.

- [ ] **Step 3: Add the field to RuntimeConfig**

In `backend/internal/ports/outbound.go`, replace lines 90-95 with:

```go
type RuntimeConfig struct {
	SessionID     domain.SessionID
	WorkspacePath string
	Argv          []string
	Env           map[string]string
	// NotifyExit asks the runtime to report the session's own exit once Argv
	// finishes. It is set for one-shot launches, whose agent exits on its own:
	// nothing else observes that exit while the daemon keeps running, so the
	// session row would otherwise stay live forever. Runtimes that cannot
	// honour it must fail Create rather than ignore it — a caller waiting for
	// the exit would hang.
	NotifyExit bool
}
```

- [ ] **Step 4: Implement in the tmux runtime**

In `backend/internal/adapters/runtime/tmux/tmux.go`, in `buildLaunchCommand`, replace:

```go
	b.WriteString(strings.Join(parts, " "))
	// Keep the tmux session alive after the agent exits so the operator can
```

with:

```go
	b.WriteString(strings.Join(parts, " "))
	if cfg.NotifyExit {
		// A one-shot agent exits on its own and nothing else observes that
		// while the daemon keeps running. `ao session mark-exited` self-targets
		// via AO_SESSION_ID (exported above) and deliberately does not tear
		// down the runtime, so the finished agent's output stays readable in
		// the pane while the session row flips to terminated. It runs as its
		// own command so a non-zero agent exit still reports.
		b.WriteString("; ao session mark-exited")
	}
	// Keep the tmux session alive after the agent exits so the operator can
```

- [ ] **Step 5: Reject the flag in the ConPTY runtime**

In `backend/internal/adapters/runtime/conpty/runtime.go`, in `Create`, after the `len(cfg.Argv) == 0` guard (line 68-70) add:

```go
	// One-shot launches need an exit report the pty-host does not emit yet.
	// Fail loudly: silently ignoring the flag would leave a live session row
	// and hang whatever is waiting for the run to finish.
	if cfg.NotifyExit {
		return ports.RuntimeHandle{}, fmt.Errorf("conpty: one-shot sessions are not supported on the ConPTY runtime")
	}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `cd backend && go test ./internal/adapters/runtime/... ./internal/ports/`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/ports/outbound.go backend/internal/adapters/runtime/
git commit -m "feat(runtime): report a one-shot agent's exit from the tmux launch"
```

---

### Task 4: One-shot spawn in the session manager

**Files:**
- Modify: `backend/internal/ports/session.go:14-32`
- Modify: `backend/internal/session_manager/manager.go` (lines 258-263, 350, 365-370, 384)
- Test: `backend/internal/session_manager/manager_test.go`

**Interfaces:**
- Consumes: `ports.AgentHeadless` (Task 1), `ports.RuntimeConfig.NotifyExit` (Task 3).
- Produces: `ports.SpawnConfig.OneShot bool`; exported sentinel `session_manager.ErrOneShotUnsupported`.

- [ ] **Step 1: Write the failing tests**

Append to `backend/internal/session_manager/manager_test.go`:

```go
// headlessAgent mimics claude-code: it implements the optional one-shot
// capability and records which command the manager asked for.
type headlessAgent struct {
	fakeAgent
	headlessCalls int
	lastHeadless  ports.LaunchConfig
}

func (a *headlessAgent) GetHeadlessCommand(_ context.Context, cfg ports.LaunchConfig) ([]string, error) {
	a.headlessCalls++
	a.lastHeadless = cfg
	return []string{"headless"}, nil
}

// afterStartHeadlessAgent reports PromptDeliveryAfterStart while also
// supporting one-shot, so a test can prove the manager skips the after-start
// send for a headless launch (which carries its prompt in argv).
type afterStartHeadlessAgent struct{ headlessAgent }

func (afterStartHeadlessAgent) GetPromptDeliveryStrategy(context.Context, ports.LaunchConfig) (ports.PromptDeliveryStrategy, error) {
	return ports.PromptDeliveryAfterStart, nil
}

func TestSpawn_OneShotUsesHeadlessCommandAndAsksForExitReport(t *testing.T) {
	st := newFakeStore()
	st.projects["mer"] = domain.ProjectRecord{ID: "mer", Config: testRoleAgents()}
	agent := &headlessAgent{}
	rt := &fakeRuntime{}
	m := New(Deps{
		Runtime: rt, Agents: singleAgent{agent: agent}, Workspace: &fakeWorkspace{}, Store: st,
		Messenger: &fakeMessenger{}, Lifecycle: &fakeLCM{store: st},
		LookPath: func(string) (string, error) { return "/bin/true", nil },
	})

	if _, err := m.Spawn(ctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker, Prompt: "do it", OneShot: true}); err != nil {
		t.Fatal(err)
	}
	if agent.headlessCalls != 1 {
		t.Fatalf("GetHeadlessCommand calls = %d, want 1", agent.headlessCalls)
	}
	if !reflect.DeepEqual(rt.lastCfg.Argv, []string{"headless"}) {
		t.Fatalf("runtime argv = %#v, want the headless command", rt.lastCfg.Argv)
	}
	if !rt.lastCfg.NotifyExit {
		t.Fatal("a one-shot spawn must ask the runtime to report the agent's exit")
	}
}

func TestSpawn_InteractiveKeepsLaunchCommandAndNoExitReport(t *testing.T) {
	st := newFakeStore()
	st.projects["mer"] = domain.ProjectRecord{ID: "mer", Config: testRoleAgents()}
	agent := &headlessAgent{}
	rt := &fakeRuntime{}
	m := New(Deps{
		Runtime: rt, Agents: singleAgent{agent: agent}, Workspace: &fakeWorkspace{}, Store: st,
		Messenger: &fakeMessenger{}, Lifecycle: &fakeLCM{store: st},
		LookPath: func(string) (string, error) { return "/bin/true", nil },
	})

	if _, err := m.Spawn(ctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker, Prompt: "do it"}); err != nil {
		t.Fatal(err)
	}
	if agent.headlessCalls != 0 {
		t.Fatalf("GetHeadlessCommand calls = %d, want 0 for an interactive spawn", agent.headlessCalls)
	}
	if rt.lastCfg.NotifyExit {
		t.Fatal("an interactive spawn must not ask for an exit report")
	}
}

func TestSpawn_OneShotRejectsAgentWithoutHeadlessSupport(t *testing.T) {
	st := newFakeStore()
	st.projects["mer"] = domain.ProjectRecord{ID: "mer", Config: testRoleAgents()}
	rt := &fakeRuntime{}
	ws := &fakeWorkspace{}
	m := New(Deps{
		Runtime: rt, Agents: fakeAgents{}, Workspace: ws, Store: st,
		Messenger: &fakeMessenger{}, Lifecycle: &fakeLCM{store: st},
		LookPath: func(string) (string, error) { return "/bin/true", nil },
	})

	_, err := m.Spawn(ctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker, Prompt: "do it", OneShot: true})
	if !errors.Is(err, ErrOneShotUnsupported) {
		t.Fatalf("err = %v, want ErrOneShotUnsupported", err)
	}
	if len(st.sessions) != 0 {
		t.Fatalf("rejected one-shot spawn left %d session rows behind, want 0", len(st.sessions))
	}
}

func TestSpawn_OneShotSkipsAfterStartPromptDelivery(t *testing.T) {
	st := newFakeStore()
	st.projects["mer"] = domain.ProjectRecord{ID: "mer", Config: testRoleAgents()}
	msg := &fakeMessenger{}
	m := New(Deps{
		Runtime: &fakeRuntime{}, Agents: singleAgent{agent: &afterStartHeadlessAgent{}},
		Workspace: &fakeWorkspace{}, Store: st, Messenger: msg, Lifecycle: &fakeLCM{store: st},
		LookPath: func(string) (string, error) { return "/bin/true", nil },
	})

	if _, err := m.Spawn(ctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker, Prompt: "do it", OneShot: true}); err != nil {
		t.Fatal(err)
	}
	if len(msg.sent) != 0 {
		t.Fatalf("one-shot spawn sent %d messages into the pane, want 0", len(msg.sent))
	}
}
```

Before running, check the fake names against the file: `grep -n "type fakeStore\|sessions \|type fakeMessenger" -A 6 backend/internal/session_manager/manager_test.go`. Use whatever field the existing fakes expose for "rows created" and "messages sent" instead of `st.sessions` / `msg.sent` if the names differ, and add `errors`/`reflect` to the imports if missing.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd backend && go test ./internal/session_manager/ -run 'OneShot|Interactive' -v`
Expected: FAIL — `unknown field OneShot in struct literal` and `undefined: ErrOneShotUnsupported`.

- [ ] **Step 3: Add the SpawnConfig field**

In `backend/internal/ports/session.go`, add to `SpawnConfig` after `DirectorCardID`:

```go
	// OneShot runs the agent in its headless mode: it executes the prompt to
	// completion and exits, instead of holding an interactive session open.
	// The session is otherwise ordinary — worktree, runtime pane, sidebar row —
	// so a caller can still watch it. Requires the harness's adapter to
	// implement AgentHeadless.
	OneShot bool
```

- [ ] **Step 4: Wire it through the manager**

In `backend/internal/session_manager/manager.go`:

(a) Add the sentinel next to the existing spawn errors (find them with `grep -n "ErrMissingHarness\|ErrUnknownHarness\s*=" backend/internal/session_manager/*.go` and add it in the same `var` block):

```go
	// ErrOneShotUnsupported reports a one-shot spawn for a harness whose
	// adapter has no headless command. Launching its interactive TUI instead
	// would hang whatever waits for the run to finish.
	ErrOneShotUnsupported = errors.New("agent harness does not support one-shot runs")
```

(b) After the unknown-harness guard (line 261-263), add:

```go
	// Reject a one-shot request the adapter cannot honour before any durable
	// state exists, so a bad request costs no worktree and no orphan row.
	if cfg.OneShot {
		agent, _ := m.agents.Agent(cfg.Harness)
		if _, headless := agent.(ports.AgentHeadless); !headless {
			return domain.SessionRecord{}, fmt.Errorf("spawn: %w: %q", ErrOneShotUnsupported, cfg.Harness)
		}
	}
```

(c) Replace line 350:

```go
	argv, err := agent.GetLaunchCommand(ctx, launchCfg)
```

with:

```go
	argv, err := launchArgv(ctx, agent, launchCfg, cfg.OneShot)
```

(d) Add the helper below `Spawn` (anywhere in the file after it, e.g. above `deliverPromptAfterStart`):

```go
// launchArgv picks the adapter command for a spawn: its headless one-shot
// command when the spawn asked for one, otherwise its interactive launch
// command. Spawn rejects a one-shot request for an adapter without the
// capability before reaching here, so the assertion is defense in depth.
func launchArgv(ctx context.Context, agent ports.Agent, cfg ports.LaunchConfig, oneShot bool) ([]string, error) {
	if !oneShot {
		return agent.GetLaunchCommand(ctx, cfg)
	}
	headless, ok := agent.(ports.AgentHeadless)
	if !ok {
		return nil, fmt.Errorf("%w: adapter exposes no headless command", ErrOneShotUnsupported)
	}
	return headless.GetHeadlessCommand(ctx, cfg)
}
```

(e) In the `m.runtime.Create` call (line 365-370), add the field after `Env`:

```go
		NotifyExit:    cfg.OneShot,
```

(f) Replace line 384:

```go
	m.deliverPromptAfterStart(ctx, id, handle, agent, launchCfg, prompt)
```

with:

```go
	// A headless launch always carries its prompt in argv, and its process has
	// no interactive prompt to type into — sending would land on the keep-alive
	// shell after the agent exits.
	if !cfg.OneShot {
		m.deliverPromptAfterStart(ctx, id, handle, agent, launchCfg, prompt)
	}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd backend && go test ./internal/session_manager/ ./internal/ports/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/ports/session.go backend/internal/session_manager/
git commit -m "feat(session): spawn a session in the agent's one-shot headless mode"
```

---

### Task 5: API field + `ao spawn --oneshot --wait`

**Files:**
- Modify: `backend/internal/httpd/controllers/dto.go:518-530`
- Modify: `backend/internal/httpd/controllers/sessions.go:151`
- Modify: `backend/internal/cli/spawn.go`
- Test: `backend/internal/cli/spawn_test.go`
- Regenerate: `backend/internal/httpd/apispec/openapi.yaml`, `frontend/src/api/schema.ts`

**Interfaces:**
- Consumes: `ports.SpawnConfig.OneShot` (Task 4).
- Produces: `SpawnSessionRequest.OneShot bool` (`json:"oneShot,omitempty"`); CLI flags `--oneshot`, `--wait`, `--wait-timeout`; package var `sessionWaitPollInterval time.Duration`.

- [ ] **Step 1: Write the failing tests**

Append to `backend/internal/cli/spawn_test.go`:

```go
func TestSpawnOneShotWaitsForExit(t *testing.T) {
	cfg := setConfigEnv(t)
	restore := sessionWaitPollInterval
	sessionWaitPollInterval = time.Millisecond
	t.Cleanup(func() { sessionWaitPollInterval = restore })

	polls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/sessions":
			var req spawnRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			if !req.OneShot {
				t.Fatalf("spawn request = %#v, want oneShot true", req)
			}
			_, _ = io.WriteString(w, `{"session":{"id":"demo-9","status":"idle"}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/sessions/demo-9":
			polls++
			if polls < 2 {
				_, _ = io.WriteString(w, `{"session":{"id":"demo-9","status":"running","isTerminated":false}}`)
				return
			}
			_, _ = io.WriteString(w, `{"session":{"id":"demo-9","status":"terminated","isTerminated":true}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	writeRunFileFor(t, cfg, srv)

	out, errOut, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }},
		"spawn", "--project", "demo", "--name", "worker", "--oneshot", "--wait")
	if err != nil {
		t.Fatalf("spawn --oneshot --wait failed: %v stderr=%s", err, errOut)
	}
	if !strings.Contains(out, "session demo-9 exited") {
		t.Fatalf("output missing the exit line: %s", out)
	}
	if polls < 2 {
		t.Fatalf("polled %d times, want at least 2 (one live, one terminated)", polls)
	}
}

func TestSpawnWaitTimesOut(t *testing.T) {
	cfg := setConfigEnv(t)
	restore := sessionWaitPollInterval
	sessionWaitPollInterval = time.Millisecond
	t.Cleanup(func() { sessionWaitPollInterval = restore })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/sessions":
			_, _ = io.WriteString(w, `{"session":{"id":"demo-9","status":"idle"}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/sessions/demo-9":
			_, _ = io.WriteString(w, `{"session":{"id":"demo-9","status":"running","isTerminated":false}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	writeRunFileFor(t, cfg, srv)

	_, _, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }},
		"spawn", "--project", "demo", "--name", "worker", "--oneshot", "--wait", "--wait-timeout", "10ms")
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	if !strings.Contains(err.Error(), "demo-9") {
		t.Fatalf("timeout error = %v, want it to name the session", err)
	}
}

func TestSpawnWaitRequiresOneShot(t *testing.T) {
	var out, errb bytes.Buffer
	root := NewRootCommand(Deps{Out: &out, Err: &errb})
	root.SetArgs([]string{"spawn", "--project", "demo", "--name", "worker", "--wait"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "--wait requires --oneshot") {
		t.Fatalf("err = %v, want it to mention --wait requires --oneshot", err)
	}
}
```

Add `"time"` to the test file's imports.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd backend && go test ./internal/cli/ -run 'SpawnOneShot|SpawnWait' -v`
Expected: FAIL — `unknown field OneShot in struct literal spawnRequest`, `undefined: sessionWaitPollInterval`.

- [ ] **Step 3: Add the API field**

In `backend/internal/httpd/controllers/dto.go`, add to `SpawnSessionRequest` after `DisplayName`:

```go
	// OneShot runs the agent headlessly: it executes the prompt to completion
	// and exits, instead of holding an interactive session open. The session is
	// otherwise ordinary, so it still appears with its own pane and sidebar row.
	OneShot bool `json:"oneShot,omitempty"`
```

In `backend/internal/httpd/controllers/sessions.go`, extend the Spawn call on line 151:

```go
	sess, err := c.Svc.Spawn(r.Context(), ports.SpawnConfig{ProjectID: in.ProjectID, IssueID: in.IssueID, Kind: in.Kind, Harness: in.Harness, Branch: in.Branch, Prompt: in.Prompt, DisplayName: displayName, OneShot: in.OneShot})
```

- [ ] **Step 4: Add the CLI flags and the poll loop**

In `backend/internal/cli/spawn.go`:

(a) Add to the `spawnRequest` struct (find it near the top of the file, above `spawnResult`):

```go
	OneShot bool `json:"oneShot,omitempty"`
```

(b) Add to `spawnOptions`:

```go
	oneShot     bool
	wait        bool
	waitTimeout time.Duration
```

(c) Add the package-level poll interval near the top of the file (below the imports):

```go
// sessionWaitPollInterval is how often `ao spawn --wait` re-reads the session.
// A package var so tests can shrink it; two seconds is well under any real
// one-shot run and costs the daemon nothing.
var sessionWaitPollInterval = 2 * time.Second
```

(d) In `RunE`, after the existing `--no-takeover` validation, add:

```go
			if opts.wait && !opts.oneShot {
				return usageError{fmt.Errorf("--wait requires --oneshot")}
			}
```

(e) Set the field on the request literal: add `OneShot: opts.oneShot,` to the `spawnRequest{...}` composite literal.

(f) Replace the final attach-hint block at the end of `RunE`:

```go
			_, err := fmt.Fprintf(out, "attach with: %s\n", attach)
			return err
```

with:

```go
			if _, err := fmt.Fprintf(out, "attach with: %s\n", attach); err != nil {
				return err
			}
			if !opts.wait {
				return nil
			}
			return ctx.waitForSessionExit(cmd.Context(), out, res.Session.ID, opts.waitTimeout)
```

(g) Register the flags next to the existing ones:

```go
	f.BoolVar(&opts.oneShot, "oneshot", false, "Run the agent headlessly: execute the prompt to completion, then exit")
	f.BoolVar(&opts.wait, "wait", false, "Block until the one-shot session exits (requires --oneshot)")
	f.DurationVar(&opts.waitTimeout, "wait-timeout", 30*time.Minute, "How long --wait blocks before giving up")
```

(h) Add the poll helper at the end of the file:

```go
// waitForSessionExit blocks until the session's row reports terminated, which
// a one-shot session reaches by running `ao session mark-exited` after its
// agent finishes. The timeout is what keeps a caller (the Director) from
// blocking forever on an agent that wedged instead of exiting.
func (c *commandContext) waitForSessionExit(ctx context.Context, out io.Writer, id string, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 30 * time.Minute
	}
	deadline := time.Now().Add(timeout)
	for {
		var res sessionResponse
		if err := c.getJSON(ctx, "sessions/"+url.PathEscape(id), &res); err != nil {
			return err
		}
		if res.Session.IsTerminated {
			_, err := fmt.Fprintf(out, "session %s exited\n", id)
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out after %s waiting for session %s to exit", timeout, id)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(sessionWaitPollInterval):
		}
	}
}
```

Add `"context"`, `"io"`, and `"time"` to the file's imports if they are not already there.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd backend && go test ./internal/cli/ ./internal/httpd/...`
Expected: PASS. The spec-drift test in `./internal/httpd/...` will fail until the next step.

- [ ] **Step 6: Regenerate the API artifacts**

Run from the repo root: `npm run api`
Then: `cd backend && go test ./internal/httpd/...`
Expected: PASS, with `openapi.yaml` and `frontend/src/api/schema.ts` showing the new `oneShot` field.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/httpd backend/internal/cli frontend/src/api/schema.ts
git commit -m "feat(cli): add ao spawn --oneshot --wait for headless worker runs"
```

---

### Task 6: Director delegates with one-shot runs

**Files:**
- Modify: `director/src/tools.ts:16-25`
- Modify: `director/src/index.ts:78-113,123`
- Test: `director/src/tools.test.ts`

**Interfaces:**
- Consumes: `ao spawn --oneshot --wait` (Task 5).
- Produces: `buildSpawnWorkerArgv(projectId: string, agent: string, prompt: string): string[]` now emits `--oneshot --wait`. `buildSendWorkerAnswerArgv` is deleted.

- [ ] **Step 1: Update the tests**

In `director/src/tools.test.ts`:

- Remove `buildSendWorkerAnswerArgv` from the import on line 2.
- Delete the `"builds the terminal answer argv"` and `"rejects a blank worker answer"` cases.
- Replace the `"builds the spawn-worker argv"` case with:

```ts
	it("builds the spawn-worker argv as a blocking one-shot run", () => {
		expect(buildSpawnWorkerArgv("proj-1", "claude-code", "implement X")).toEqual([
			"spawn", "--project", "proj-1", "--agent", "claude-code", "--name", "claude-code-worker",
			"--oneshot", "--wait", "--prompt", "implement X",
		]);
	});
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd director && npx vitest run src/tools.test.ts`
Expected: FAIL — the argv is missing `--oneshot --wait`, and the import of the deleted symbol errors.

- [ ] **Step 3: Update the argv builders**

In `director/src/tools.ts`, replace `buildSpawnWorkerArgv` and delete `buildSendWorkerAnswerArgv`:

```ts
/** Workers run one-shot: the CLI executes the subtask to completion and exits,
 * and `--wait` blocks until it does. The Director therefore learns a phase is
 * finished by its tool call returning, not by the worker messaging it back. */
export function buildSpawnWorkerArgv(projectId: string, agent: string, prompt: string): string[] {
	const name = `${agent}-worker`.slice(0, 20);
	return ["spawn", "--project", projectId, "--agent", agent, "--name", name, "--oneshot", "--wait", "--prompt", prompt];
}
```

- [ ] **Step 4: Update the Director's tools**

In `director/src/index.ts`:

(a) Remove `buildSendWorkerAnswerArgv` from the import block (lines 13-20).

(b) Replace the `spawnWorker` tool (lines 78-100) with:

```ts
	const spawnWorker = tool(
		async ({ agent, prompt }: { agent: string; prompt: string }) =>
			runAo(
				runner,
				buildSpawnWorkerArgv(
					cfg.projectId,
					agent,
					`${prompt.trim()}\n\nYou are running one-shot: you cannot ask questions, so decide and proceed. Before you finish, record a durable handoff with \`ao workboard card handoff <card-id> --phase <coding|review|testing> --summary "<what you did or found>" --changed <file> --check "<command and result>" --commit <sha-or-pr> --next "<what the next phase must verify>". That handoff is the only report the Director reads.`,
				),
			),
		{
			name: "spawn_worker",
			description:
				"Run an implementation subtask as a one-shot worker session. Blocks until the worker exits; read its result with show_card.",
			schema: z.object({
				agent: z
				.string()
				.describe(
					"Harness id to run the worker on, e.g. claude-code, codex. Use the VALUE of the card's codingAgent/reviewerAgent/testingAgent field, never the field name itself.",
				),
				prompt: z.string().describe("The exact subtask, including the card id"),
			}),
		},
	);
```

(c) Delete the entire `answerWorker` tool (lines 102-113).

(d) Change the `tools` array on line 123 to:

```ts
		tools: [showCard, transitionCard, spawnWorker],
```

- [ ] **Step 5: Run the tests and the type check to verify they pass**

Run: `cd director && npx vitest run && npx tsc --noEmit`
Expected: PASS, no type errors.

- [ ] **Step 6: Check the Director's system prompt for stale instructions**

Run: `grep -n "answer_worker\|ao send\|asks a question" director/src/prompt.ts`
If any hit describes answering worker questions or the `ao send` back-channel, delete that sentence — a one-shot worker never asks. Leave everything else in the prompt alone. Then re-run `cd director && npx vitest run`.

- [ ] **Step 7: Commit**

```bash
git add director/src
git commit -m "feat(director): delegate card phases as blocking one-shot worker runs"
```

---

### Task 7: End-to-end verification

**Files:** none modified — this task proves the feature works in the real app.

**Interfaces:**
- Consumes: everything from Tasks 1-6.
- Produces: nothing.

- [ ] **Step 1: Run the full backend suite**

Run: `cd backend && go test ./...`
Expected: PASS.

- [ ] **Step 2: Run the repo lint**

Run: `npm run lint`
Expected: PASS (backend `go test ./...` + golangci-lint v2.12.2).

- [ ] **Step 3: Build the Director bundle**

Run: `cd director && npm run build`
Expected: a clean build with no type errors.

- [ ] **Step 4: Spawn a one-shot worker by hand**

Against a registered project (substitute a real project id):

```bash
ao spawn --project <projectId> --agent claude-code --name oneshot-probe \
  --oneshot --wait --prompt "Print the repository's top-level directory names and stop."
```

Expected: the command blocks, then prints `spawned session <id> (…)`, the attach hint, and finally `session <id> exited`. While it blocks, `ao session ls` shows the session live; after it returns, `ao session ls --include-terminated` shows it terminated.

- [ ] **Step 5: Confirm the frontend is unchanged**

While the command from Step 4 is still blocking, open the app and confirm the worker appears in the sidebar with a live terminal showing the headless run's output. Run `ao preview` from inside the session if a visual check of the panel is needed.

- [ ] **Step 6: Verify the rejection path**

```bash
ao spawn --project <projectId> --agent aider --name oneshot-reject --oneshot --wait --prompt "hi"
```

Expected: a clear error naming the harness and one-shot support, and no new session row in `ao session ls --include-terminated`.

- [ ] **Step 7: Drive one card with the Director**

Set the project's Director harness, create a card, and let the Director run it. Expected: the card moves Todo → Running → Review → Testing → Done; each phase spawns a worker session that appears, runs headlessly, and exits on its own; the Director never calls `answer_worker` (it no longer exists) and reads each phase's result from the card handoff.

- [ ] **Step 8: Commit any fixes found during verification**

```bash
git add -A
git commit -m "fix(director): <what the end-to-end run exposed>"
```

If nothing needed fixing, skip this step.

---

## Self-Review

**Spec coverage:**

| Spec section | Task |
| --- | --- |
| §1 Headless launch commands (`AgentHeadless`, claude-code, codex, reject unknown) | 1, 2 |
| §2 One-shot spawn (`SpawnConfig.OneShot`, headless argv, skip after-start prompt) | 4 |
| §2 `RuntimeConfig.NotifyExit`, tmux `mark-exited`, conpty rejection | 3 |
| §3 `ao spawn --oneshot --wait --wait-timeout` | 5 |
| §4 Director: blocking `spawn_worker`, delete `answer_worker`, prompt boilerplate | 6 |
| Card flow unchanged (`transition_card`) | untouched by design; verified in 7 Step 7 |
| Stdin/sentinel untouched | Global Constraints; verified by 6 Step 5 passing without touching `inbound.ts` |
| Error handling (unsupported harness, timeout, non-zero exit) | 4 Step 1, 5 Step 1, 3 Step 4 comment |
| Testing section | 1-6 each carry their tests |

**Placeholder scan:** no TBD/TODO; every code step carries the actual code; the two "check the existing helper/fake names first" notes name the exact `grep` to run and what to do with the result.

**Type consistency:** `GetHeadlessCommand(ctx, ports.LaunchConfig) ([]string, error)` is identical in Tasks 1, 2, and 4. `NotifyExit` is a `bool` on `ports.RuntimeConfig` in Tasks 3 and 4. `OneShot` is a `bool` on `ports.SpawnConfig` (Task 4) and on `SpawnSessionRequest`/`spawnRequest` as `oneShot` (Task 5). `buildSpawnWorkerArgv` keeps its three-parameter signature in Task 6.
