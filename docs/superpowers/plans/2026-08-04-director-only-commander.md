# Director-Only Commander Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the Director the single, working card commander — it starts, it moves cards, and the UI shows its real state instead of a raw terminal.

**Architecture:** Six independent fixes along one causal chain. (1) A Director spawned without a card id dies silently; fail that spawn loudly instead. (2) The Hermes commander orchestrator ticks for every project regardless of config; gate it off on Director projects. (3) Five workboard code paths hard-code `harness == "hermes"` to mean "this card's commander"; widen that one predicate to include the Director. (4) The Director frames every inbound terminal message as "a worker question", so a completed-phase handoff never becomes a `transition_card` call; buffer whole messages and let the model classify them. (5) The focus panel's commander terminal is replaced with a Director status summary. (6) The provider API key the Director needs to boot gets a field in project settings.

Tasks 1-4 are what make the card move; 5 and 6 are the surfaces around them. Task 6 is independent of the rest and can be done first if you want a Director that boots before you debug one that stalls.

**Tech Stack:** Go 1.x (backend, `backend/`), TypeScript + Node (Director agent, `director/`, bundled with esbuild), React + TanStack Query + shadcn/ui (renderer, `frontend/src/renderer/`), tmux runtime adapter, SQLite store.

## Global Constraints

- All app state resolves under `~/.ao` (or `AO_DATA_DIR`). Never write to `~/Library/Application Support`.
- Renderer UI clones the modern-agent web app verbatim; build from `frontend/src/renderer/components/ui/*` primitives. Read `DESIGN.md` before any visual decision.
- Go tests: `cd backend && go test ./internal/...`. Director tests: `cd director && npm test`. Frontend tests: `cd frontend && npm test`.
- The Director's committed bundle `backend/internal/directorassets/bundle/index.js` is generated. It MUST be regenerated from `director/src` in the same commit as any `director/src` change.
- Commit messages end with the repo's existing trailer convention. One commit per task.

---

### Task 1: Fail a Director spawn that has no card id

A Director launched without `AO_DIRECTOR_CARD_ID` prints `director: AO_DIRECTOR_CARD_ID is required but was not set` and exits. The tmux runtime appends `; exec $SHELL` to every launch command (`backend/internal/adapters/runtime/tmux/tmux.go:484`), so the pane survives as a bare shell, `IsAlive` stays true, and the session row keeps `is_terminated = 0` forever. The result is a session that looks like a live commander and is a dead shell — exactly what `test-13` is on this machine.

`ports.SpawnConfig.DirectorCardID` is set in exactly one place, `backend/internal/service/workboard/director_dispatch.go:83`. Every other caller — `SpawnOrchestrator`, `ao spawn --agent director`, the renderer's ensure-on-load orchestrator spawn — produces the dead session. Fix it once, at the boundary all of them cross.

**Files:**
- Modify: `backend/internal/session_manager/manager.go` (add error var near line 43; add guard in `Spawn` after line 244)
- Test: `backend/internal/session_manager/manager_test.go` (add after `TestSpawn_RejectsMissingRoleHarness`, ~line 627)

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `sessionmanager.ErrDirectorCardRequired` (`error`) — Task 5 does not use it; no other task depends on it.

- [ ] **Step 1: Write the failing test**

Add to `backend/internal/session_manager/manager_test.go`:

```go
func TestSpawn_RejectsDirectorWithoutCardID(t *testing.T) {
	st := newFakeStore()
	st.projects["mer"] = domain.ProjectRecord{ID: "mer"}
	m := New(Deps{
		Runtime: &fakeRuntime{}, Agents: fakeAgents{}, Workspace: &fakeWorkspace{}, Store: st,
		Messenger: &fakeMessenger{}, Lifecycle: &fakeLCM{store: st},
		LookPath: func(string) (string, error) { return "/bin/true", nil },
	})

	_, err := m.Spawn(ctx, ports.SpawnConfig{
		ProjectID: "mer",
		Kind:      domain.KindOrchestrator,
		Harness:   domain.HarnessDirector,
	})
	if !errors.Is(err, ErrDirectorCardRequired) {
		t.Fatalf("err = %v, want ErrDirectorCardRequired", err)
	}
	if len(st.sessions) != 0 {
		t.Fatalf("a rejected Director spawn must not create a session row, got %d", len(st.sessions))
	}
}

func TestSpawn_AllowsDirectorWithCardID(t *testing.T) {
	st := newFakeStore()
	st.projects["mer"] = domain.ProjectRecord{ID: "mer"}
	m := New(Deps{
		Runtime: &fakeRuntime{}, Agents: fakeAgents{}, Workspace: &fakeWorkspace{}, Store: st,
		Messenger: &fakeMessenger{}, Lifecycle: &fakeLCM{store: st},
		LookPath: func(string) (string, error) { return "/bin/true", nil },
	})

	if _, err := m.Spawn(ctx, ports.SpawnConfig{
		ProjectID:      "mer",
		Kind:           domain.KindOrchestrator,
		Harness:        domain.HarnessDirector,
		DirectorCardID: "card_1",
	}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if len(st.sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(st.sessions))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd backend && go test ./internal/session_manager/ -run 'TestSpawn_(Rejects|Allows)Director' -v
```

Expected: FAIL — `undefined: ErrDirectorCardRequired` (compile error).

- [ ] **Step 3: Add the error var**

In `backend/internal/session_manager/manager.go`, in the existing `var (...)` block that declares `ErrMissingHarness` (line ~43), add:

```go
	// ErrDirectorCardRequired rejects a Director spawn that names no work card.
	// The Director exits immediately without AO_DIRECTOR_CARD_ID, and the tmux
	// runtime's `; exec $SHELL` tail keeps the pane alive afterwards — so the
	// failure is invisible: a live-looking session that is a bare shell. Refuse
	// the spawn instead of creating one.
	ErrDirectorCardRequired = errors.New("session: director harness requires a work card id")
```

- [ ] **Step 4: Add the guard**

In `backend/internal/session_manager/manager.go`, in `Spawn`, immediately after the `ErrMissingHarness` check (the block ending at line 244) and **before** the `m.agents.Agent(cfg.Harness)` lookup:

```go
	// The Director loop is scoped to one card; without it the process exits at
	// startup. Reject here, before any durable state exists.
	if cfg.Harness == domain.HarnessDirector && strings.TrimSpace(cfg.DirectorCardID) == "" {
		return domain.SessionRecord{}, fmt.Errorf("spawn: %w", ErrDirectorCardRequired)
	}
```

`strings` and `fmt` are already imported in this file.

- [ ] **Step 5: Run tests to verify they pass**

```bash
cd backend && go test ./internal/session_manager/ -run 'TestSpawn_' -v
```

Expected: PASS, including the two new tests.

- [ ] **Step 6: Run the full backend suite**

```bash
cd backend && go test ./internal/...
```

Expected: PASS. If a test spawns `HarnessDirector` without a card id, it was asserting the broken behaviour — give it a `DirectorCardID: "card_test"`.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/session_manager/manager.go backend/internal/session_manager/manager_test.go
git commit -m "fix(director): reject a Director spawn with no work card id

Without AO_DIRECTOR_CARD_ID the Director exits at startup, and the tmux
runtime's `exec \$SHELL` tail keeps the pane alive — producing a session
that reports itself live while being a bare shell. Only workboard's
dispatchToDirector ever set the card id; every other spawn path silently
produced a dead commander."
```

---

### Task 2: Gate the Hermes commander orchestrator off on Director projects

`ConfiguredOrchestrator.Tick` (`backend/internal/commander/orchestrator/orchestrator.go:91`) runs for every project on a 30s timer and on every card-change CDC event (`backend/internal/daemon/orchestrator_wiring.go:78,88`). It reads no project config, so on a Director project it spawns its own per-phase workers and briefings while the Director spawns its own — two commanders on one card.

`Dispatcher.DispatchOnce` already short-circuits to `dispatchToDirector` at `backend/internal/service/workboard/dispatch.go:155`, so the dispatch side is already Director-only. Only the tick loop is missing the gate. Gate inside `Tick` rather than at the wiring, so both the CDC and periodic callers — and any future one — are covered by one check.

**Files:**
- Modify: `backend/internal/commander/orchestrator/orchestrator.go` (add `GetProject` to `OrchestratorStore`, ~line 38; gate `Tick`, ~line 91)
- Test: `backend/internal/commander/orchestrator/orchestrator_test.go` (add `GetProject` to `fakeStore`, ~line 24; add test)

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `OrchestratorStore.GetProject(ctx context.Context, id string) (domain.ProjectRecord, bool, error)` — `*sqlite.Store` already satisfies it structurally (`DispatchStore` uses the identical signature).

- [ ] **Step 1: Write the failing test**

Add to `backend/internal/commander/orchestrator/orchestrator_test.go`:

```go
func TestTick_SkipsProjectsCommandedByTheDirector(t *testing.T) {
	st := newFakeStore()
	st.projects = map[string]domain.ProjectRecord{
		"p": {ID: "p", Config: domain.ProjectConfig{
			Director: domain.AgentRoleConfig{Harness: domain.HarnessDirector},
		}},
	}
	st.cards["c1"] = domain.WorkCard{ID: "c1", ProjectID: "p", Status: domain.CardStatusRunning}
	sp := &fakeSpawner{}
	o := New(Config{Spawner: sp, Store: st, WIPLimit: 4})

	if err := o.Tick(ctx, "p"); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if len(sp.spawned) != 0 {
		t.Fatalf("spawned = %d, want 0 — the Director owns this project's cards", len(sp.spawned))
	}
}

func TestTick_StillRunsForHermesProjects(t *testing.T) {
	st := newFakeStore()
	st.projects = map[string]domain.ProjectRecord{
		"p": {ID: "p", Config: domain.ProjectConfig{
			Orchestrator: domain.AgentRoleConfig{Harness: domain.HarnessHermes},
		}},
	}
	st.cards["c1"] = domain.WorkCard{ID: "c1", ProjectID: "p", Status: domain.CardStatusRunning}
	sp := &fakeSpawner{}
	o := New(Config{Spawner: sp, Store: st, WIPLimit: 4})

	if err := o.Tick(ctx, "p"); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if len(sp.spawned) != 1 {
		t.Fatalf("spawned = %d, want 1", len(sp.spawned))
	}
}
```

Read the existing tests in this file first: use whatever the file's spawner fake is actually named and however it records spawns. If it is not `fakeSpawner{spawned: ...}`, adapt these two assertions to the existing fake rather than adding a second one.

- [ ] **Step 2: Add `projects` to the store fake**

In `backend/internal/commander/orchestrator/orchestrator_test.go`, add the field to `fakeStore` (line ~17) and a method:

```go
type fakeStore struct {
	cards           map[string]domain.WorkCard
	activeSessions  map[string]ActiveSessionRecord
	redoCycles      map[string][]domain.RedoCycle
	events          []domain.WorkCardEvent
	listSessionsOut []domain.SessionRecord
	projects        map[string]domain.ProjectRecord
}

func (s *fakeStore) GetProject(_ context.Context, id string) (domain.ProjectRecord, bool, error) {
	rec, ok := s.projects[id]
	return rec, ok, nil
}
```

Leave `newFakeStore` as it is — a nil `projects` map reads fine, and `GetProject` then reports `ok == false`, which the gate must treat as "not a Director project".

- [ ] **Step 3: Run test to verify it fails**

```bash
cd backend && go test ./internal/commander/orchestrator/ -run 'TestTick_(Skips|StillRuns)' -v
```

Expected: FAIL — `TestTick_SkipsProjectsCommandedByTheDirector` reports `spawned = 1, want 0`.

- [ ] **Step 4: Add `GetProject` to the store interface**

In `backend/internal/commander/orchestrator/orchestrator.go`, add to `OrchestratorStore` (line ~38):

```go
	GetProject(ctx context.Context, id string) (domain.ProjectRecord, bool, error)
```

- [ ] **Step 5: Gate `Tick`**

Replace `Tick` in `backend/internal/commander/orchestrator/orchestrator.go`:

```go
// Tick is called periodically (every 30s) and on every card state change event.
// It lists all cards in active phases (running, review, testing, redo) and
// ensures each has a live session.
//
// A project whose cards are commanded by the Director is skipped entirely: the
// Director spawns and sequences its own phase workers, so ticking here would
// put a second commander on every card.
func (o *ConfiguredOrchestrator) Tick(ctx context.Context, projectID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	commanded, err := o.directorCommanded(ctx, projectID)
	if err != nil {
		return err
	}
	if commanded {
		return nil
	}
	cards, err := o.store.ListWorkCards(ctx, projectID, defaultBoardID)
	if err != nil {
		return fmt.Errorf("list work cards: %w", err)
	}
	return o.tickActiveCards(ctx, cards)
}

// directorCommanded reports whether the project opted into AO's Director
// harness. Deliberately a local predicate rather than a shared one: the two
// commander mechanisms share no state, and workboard's directorEnabled makes
// the same point from the other side.
func (o *ConfiguredOrchestrator) directorCommanded(ctx context.Context, projectID string) (bool, error) {
	project, ok, err := o.store.GetProject(ctx, projectID)
	if err != nil {
		return false, fmt.Errorf("get project %s: %w", projectID, err)
	}
	if !ok {
		return false, nil
	}
	return project.Config.Director.Harness == domain.HarnessDirector, nil
}
```

- [ ] **Step 6: Run tests to verify they pass**

```bash
cd backend && go test ./internal/commander/... -v
```

Expected: PASS.

- [ ] **Step 7: Run the full backend suite**

```bash
cd backend && go test ./internal/...
```

Expected: PASS. Any other fake implementing `OrchestratorStore` (check `backend/internal/daemon/orchestrator_store_adapter*.go` and its tests) needs the new `GetProject` method — `*sqlite.Store` already has it.

- [ ] **Step 8: Commit**

```bash
git add backend/internal/commander/orchestrator/ backend/internal/daemon/
git commit -m "fix(commander): stop the Hermes orchestrator ticking on Director projects

Tick read no project config, so on a project configured for the Director
it spawned its own per-phase workers alongside the Director's — two
commanders on one card. DispatchOnce already routed to dispatchToDirector;
only the tick loop was ungated."
```

---

### Task 3: Treat the Director as a card commander everywhere Hermes is

Five workboard paths use `isHermesCommander` (`backend/internal/service/workboard/actions.go:333`) to mean "this card's commander session". Each one is wrong for a Director-commanded card:

| Call site | Current effect on a Director card |
|---|---|
| `stall_nudge.go:105` | An idle, stalled Director is never nudged — it stays stuck forever. |
| `answer.go:116` | The card's own commander is mistaken for a coding worker with an unanswered question. |
| `actions.go:152` (retarget) | The retarget handoff is not delivered to the Director. |
| `actions.go:278` (split) | `commanded == false`, so split takes the kill-the-worker path against the commander. |
| `switch_agent.go:179` | The rate-limit auto-switch does not skip the Director — it can kill the commander. |

All five already route through one predicate. Widen it.

`answer.go:280` and `answer.go:393` are a different thing: they pick the project's Hermes orchestrator as a *fallback* recipient. Leave those alone — Task 2 means a Director project has no Hermes orchestrator, so those branches simply find nothing.

**Files:**
- Modify: `backend/internal/service/workboard/actions.go:333-344` (rename and widen the predicate, update both local callers)
- Modify: `backend/internal/service/workboard/answer.go:116`, `backend/internal/service/workboard/stall_nudge.go:105`, `backend/internal/service/workboard/switch_agent.go:179` (rename at the call site)
- Modify: `backend/internal/service/workboard/director_dispatch.go:20-25` (update the stale doc comment)
- Test: `backend/internal/service/workboard/actions_test.go`, `backend/internal/service/workboard/stall_nudge_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `isCardCommander(session domain.SessionRecord) bool` and `cardCommanderSession(sessions []domain.SessionRecord, id domain.SessionID) (domain.SessionRecord, bool)` — package-private, replacing `isHermesCommander` / `hermesCommanderSession`.

- [ ] **Step 1: Write the failing test**

Add to `backend/internal/service/workboard/actions_test.go`:

```go
func TestIsCardCommander_AcceptsHermesAndDirector(t *testing.T) {
	cases := []struct {
		name string
		rec  domain.SessionRecord
		want bool
	}{
		{"hermes commander", domain.SessionRecord{Kind: domain.KindOrchestrator, Harness: domain.HarnessHermes}, true},
		{"director commander", domain.SessionRecord{Kind: domain.KindOrchestrator, Harness: domain.HarnessDirector}, true},
		{"terminated director", domain.SessionRecord{Kind: domain.KindOrchestrator, Harness: domain.HarnessDirector, IsTerminated: true}, false},
		{"coding worker", domain.SessionRecord{Kind: domain.KindWorker, Harness: domain.HarnessDirector}, false},
		{"other orchestrator", domain.SessionRecord{Kind: domain.KindOrchestrator, Harness: domain.HarnessCodex}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isCardCommander(tc.rec); got != tc.want {
				t.Fatalf("isCardCommander = %v, want %v", got, tc.want)
			}
		})
	}
}
```

Then add the Director counterpart of the stall-nudge test. Append to `backend/internal/service/workboard/stall_nudge_test.go`:

```go
func TestStallNudge_IdleDirectorCommanderGetsNudged(t *testing.T) {
	now := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	director := idleCommander("director-1", now.Add(-11*time.Minute))
	director.Harness = domain.HarnessDirector
	store := &stallNudgeStore{
		cards:    map[string]domain.WorkCard{"c1": stallCard("c1", "running", "director-1")},
		sessions: []domain.SessionRecord{director},
	}
	sender := &stallNudgeSender{}
	n := NewStallNudger(StallNudgeDeps{Store: store, Sender: sender, Clock: func() time.Time { return now }, NewID: func() string { return "ev-1" }})

	nudged, err := n.ReconcileProject(context.Background(), "p1")
	if err != nil {
		t.Fatalf("ReconcileProject: %v", err)
	}
	if len(nudged) != 1 || nudged[0] != "c1" {
		t.Fatalf("nudged = %v, want [c1] — a stalled Director must be nudged like a Hermes commander", nudged)
	}
	if len(sender.sent) != 1 || sender.sent[0].session != "director-1" {
		t.Fatalf("sent = %+v, want one message to director-1", sender.sent)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd backend && go test ./internal/service/workboard/ -run 'TestIsCardCommander' -v
```

Expected: FAIL — `undefined: isCardCommander` (compile error).

- [ ] **Step 3: Widen the predicate**

Replace `backend/internal/service/workboard/actions.go:333-344`:

```go
// isCardCommander reports whether a session is a live commander that owns work
// cards — AO's Director or a Hermes orchestrator. A commander is coordinated
// with (nudged, handed off to, answered), never treated as the coding worker
// and never killed by the rate-limit auto-switch.
func isCardCommander(session domain.SessionRecord) bool {
	if session.IsTerminated || session.Kind != domain.KindOrchestrator {
		return false
	}
	return session.Harness == domain.HarnessHermes || session.Harness == domain.HarnessDirector
}

func cardCommanderSession(sessions []domain.SessionRecord, id domain.SessionID) (domain.SessionRecord, bool) {
	for _, session := range sessions {
		if session.ID == id && isCardCommander(session) {
			return session, true
		}
	}
	return domain.SessionRecord{}, false
}
```

- [ ] **Step 4: Update the five call sites**

```bash
cd backend && \
  grep -rln 'isHermesCommander\|hermesCommanderSession' internal/service/workboard/ | \
  xargs sed -i '' -e 's/hermesCommanderSession/cardCommanderSession/g' -e 's/isHermesCommander/isCardCommander/g'
```

Then fix the three comments the rename leaves stale, by hand:

- `backend/internal/service/workboard/stall_nudge.go` (~line 106): replace `// Only Hermes commanders are nudged.` with `// Only card commanders (Director, Hermes) are nudged.`
- `backend/internal/service/workboard/switch_agent.go` (~line 177): replace `// A Hermes-linked card is commanded by its orchestrator, not executed by` with `// A commander-linked card is coordinated by that session, not executed in`
- `backend/internal/service/workboard/director_dispatch.go:20-22`: replace `// Deliberately independent of isHermesCommander: the two commander mechanisms // share no state and must not share a predicate.` with `// Deliberately independent of isCardCommander: that predicate answers "may I // coordinate with this session"; this one answers "is this project's Director // already running", and the two must not drift into each other.`

- [ ] **Step 5: Run tests to verify they pass**

```bash
cd backend && go test ./internal/service/workboard/ -v
```

Expected: PASS, including both new tests.

- [ ] **Step 6: Run the full backend suite**

```bash
cd backend && go test ./internal/...
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/service/workboard/
git commit -m "fix(workboard): treat the Director as a card commander, not a worker

isHermesCommander gated five paths on harness == hermes: stall nudges,
autonomous answering, retarget handoff, split, and the rate-limit agent
switch. On a Director-commanded card each one misfired — no stall nudge
ever, and the switcher was free to kill the commander itself."
```

---

### Task 4: Let the Director recognise a finished phase

`director/src/index.ts:166-176` reads the Director's stdin — the channel `ao send` writes into — and does two things wrong:

```ts
const lines = pendingInput.split(/\r?\n/);
pendingInput = lines.pop() ?? "";
for (const line of lines) {
    const question = line.trim();
    if (question !== "" && !terminalCard) {
        enqueue(`A worker sent this terminal question. Resolve it and use answer_worker to reply:\n${question}`);
    }
}
```

1. Every inbound line becomes its own model turn. A worker's handoff report is multi-line by construction (`ao workboard card handoff` records changed files, checks, commit, next focus, and the Director's own spawn prompt tells the worker to send that same report), so one report becomes N fragmentary turns.
2. Every line is labelled *a question to answer with `answer_worker`*. A completed-phase report arrives pre-classified as something else, so the model replies to the worker instead of calling `transition_card`. This is the direct cause of "the Director never moves the card".

Fix: buffer until the message stops arriving, then hand the model the whole text with a neutral frame that names both possibilities.

**Files:**
- Create: `director/src/inbound.ts`
- Create: `director/src/inbound.test.ts`
- Modify: `director/src/index.ts:164-176`
- Modify: `director/build.mjs` (copy the built bundle into the Go embed dir)
- Regenerate: `backend/internal/directorassets/bundle/index.js`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `inboundInstruction(message: string): string` from `./inbound.js` — used only by `director/src/index.ts`.

- [ ] **Step 1: Write the failing test**

Create `director/src/inbound.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { inboundInstruction } from "./inbound.js";

describe("inboundInstruction", () => {
	it("keeps a multi-line report intact", () => {
		const report = "Handoff: coding done\nChanged: src/a.ts\nChecks: npm test PASS";
		expect(inboundInstruction(report)).toContain(report);
	});

	it("does not pre-classify the message as a question", () => {
		const out = inboundInstruction("Handoff: coding done");
		expect(out).not.toContain("terminal question");
	});

	it("names both outcomes so a finished phase can advance the card", () => {
		const out = inboundInstruction("Handoff: coding done");
		expect(out).toContain("transition_card");
		expect(out).toContain("answer_worker");
	});

	it("trims surrounding whitespace", () => {
		expect(inboundInstruction("  hi  \n")).toContain("hi");
		expect(inboundInstruction("  hi  \n")).not.toContain("  hi  ");
	});
});
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd director && npm test -- inbound
```

Expected: FAIL — `Failed to resolve import "./inbound.js"`.

- [ ] **Step 3: Write the module**

Create `director/src/inbound.ts`:

```ts
/** Frames a message that arrived on the Director's terminal control channel.
 *
 * Deliberately does not pre-classify it. Workers send two kinds of message on
 * this channel — a question that blocks them, and the phase handoff report they
 * are required to send when a phase completes — and the earlier framing called
 * every one of them a question. A completed phase then never became a
 * transition_card call, so the card never moved. */
export function inboundInstruction(message: string): string {
	return `A worker session sent this message on your terminal channel:

${message.trim()}

Decide what it is before you act:

- A question or a blocker: resolve it, then reply with the answer_worker tool.
- A phase handoff report (a completed phase, its changed files, checks and their results, commit or PR, and the next phase's focus): verify it against the live card with show_card, then either start the next phase's worker or advance the card with transition_card. Do not reply with answer_worker to a report that asks you nothing.
- Neither: read the card and continue driving it.`;
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd director && npm test -- inbound
```

Expected: PASS, 4 tests.

- [ ] **Step 5: Wire it into the stdin reader**

In `director/src/index.ts`, add to the import block at the top:

```ts
import { inboundInstruction } from "./inbound.js";
```

Then replace lines 164-176 (from `process.stdin.setEncoding("utf8");` through the closing `});` of the `data` handler) with:

```ts
	// The daemon's session messenger writes to this process's PTY. Keeping stdin
	// open turns the Director terminal into a control channel: a worker's
	// `ao send` message is a new model turn, not text lost at a shell prompt.
	//
	// One `ao send` arrives as several stdin chunks (tmux send-keys is chunked,
	// then Enter). Buffer until the message stops arriving, so a multi-line
	// handoff report reaches the model whole instead of one turn per line.
	//
	// ponytail: a fixed quiet window, not a framed protocol. The ceiling is a
	// message that stalls mid-flight for longer than the window arriving as two
	// turns. Upgrade path: have `ao send` frame messages with an explicit
	// terminator the Director splits on.
	const INBOUND_QUIET_MS = 300;
	process.stdin.setEncoding("utf8");
	let pendingInput = "";
	let flushTimer: NodeJS.Timeout | undefined;
	const flushInbound = () => {
		const message = pendingInput.trim();
		pendingInput = "";
		if (message !== "" && !terminalCard) enqueue(inboundInstruction(message));
	};
	process.stdin.on("data", (chunk: string) => {
		pendingInput += chunk;
		if (flushTimer) clearTimeout(flushTimer);
		flushTimer = setTimeout(flushInbound, INBOUND_QUIET_MS);
	});
```

Leave `process.stdin.resume()`, `await finished;` and `process.stdin.pause();` exactly as they are.

- [ ] **Step 6: Typecheck and run the whole Director suite**

```bash
cd director && npm run typecheck && npm test
```

Expected: both PASS.

- [ ] **Step 7: Make the build copy the bundle into the Go embed dir**

The committed bundle at `backend/internal/directorassets/bundle/index.js` is what actually ships and runs; `director/dist/index.js` is not. Nothing copies one to the other today, so a `director/src` change can silently not ship. In `director/build.mjs`, after the `writeFile("dist/index.js", ...)` call, add:

```js
// The Go daemon embeds and installs this copy — it, not dist/, is what runs.
// Copying here keeps a src change from silently not shipping.
await copyFile("dist/index.js", "../backend/internal/directorassets/bundle/index.js");
```

`copyFile` is already imported in this file.

- [ ] **Step 8: Rebuild the bundle and verify it carries the fix**

```bash
cd director && npm run build
cd .. && grep -c "A worker sent this terminal question" backend/internal/directorassets/bundle/index.js
grep -c "on your terminal channel" backend/internal/directorassets/bundle/index.js
```

Expected: `0` for the first grep, `1` for the second.

- [ ] **Step 9: Verify the built bundle still starts**

Run the freshly built bundle and confirm the missing-card-id error is the *only* startup failure — no module-resolution or syntax error ahead of it:

```bash
cd /tmp && AO_SESSION_ID=s1 node /Users/up-mac/wokrspace/mind/moden-agent/director/dist/index.js </dev/null 2>&1 | head -3
```

Expected, exactly one line:

```
director: AO_DIRECTOR_CARD_ID is required but was not set
```

- [ ] **Step 10: Commit**

```bash
git add director/src/inbound.ts director/src/inbound.test.ts director/src/index.ts \
        director/build.mjs director/dist/index.js \
        backend/internal/directorassets/bundle/index.js
git commit -m "fix(director): stop framing every inbound message as a worker question

Each stdin line became its own turn, labelled a question to answer with
answer_worker. A worker's phase handoff report is multi-line and asks
nothing — so it arrived fragmented and pre-classified, and the model
replied to the worker instead of calling transition_card. Buffer whole
messages and let the model classify them. build.mjs now copies the bundle
into the Go embed dir, which is the copy that actually ships."
```

---

### Task 5: Replace the commander terminal with a Director status summary

The focus panel offers "Show commander terminal" for any orchestrator-kind session (`frontend/src/renderer/components/WorkCardFocusPanel.tsx:297,319`). For a Director that terminal is a raw DeepAgents log — and when the Director has died it is a bare shell, which reads as a working commander. Replace it with facts.

Note what the panel must **not** claim: session liveness is not knowable from `session.isTerminated`, because the tmux runtime's `exec $SHELL` tail keeps a pane alive after the agent exits. Show the harness, the phase, the linked session id, and the project's last dispatch attempt — all facts — and let "View all sessions" remain the route to the terminal itself.

`useDirectorStatus(projectId)` already exists (`frontend/src/renderer/hooks/useWorkboardQuery.ts:78`) and returns `{ projectId, daemonReady, runningCount, wipLimit, todoCount, lastDispatchAttempt?: { result: "success"|"wip_full"|"error", attemptedAt?, error? } }`. Nothing new is needed on the backend.

**Files:**
- Modify: `frontend/src/renderer/components/WorkCardFocusPanel.tsx`
- Test: `frontend/src/renderer/components/WorkCardFocusPanel.test.tsx`

**Interfaces:**
- Consumes: `useDirectorStatus` from `../hooks/useWorkboardQuery` (existing).
- Produces: nothing consumed by other tasks.

- [ ] **Step 1: Extend the test file's mocks and render helper**

`frontend/src/renderer/components/WorkCardFocusPanel.test.tsx` mocks `apiClient` with only `DELETE`, `PATCH` and `POST`, and its `renderPanel` takes a card but no session. `useDirectorStatus` issues a `GET`, so both need extending.

Change the `vi.hoisted` block and the `api-client` mock (lines 8-21) to add a `GET`:

```tsx
const { deleteMock, patchMock, getMock } = vi.hoisted(() => ({
	deleteMock: vi.fn(),
	patchMock: vi.fn(),
	getMock: vi.fn(),
}));

vi.mock("../lib/api-client", () => ({
	apiClient: {
		DELETE: (...args: unknown[]) => deleteMock(...args),
		PATCH: (...args: unknown[]) => patchMock(...args),
		GET: (...args: unknown[]) => getMock(...args),
		POST: vi.fn(),
	},
	apiErrorMessage: (error: unknown, fallback = "Request failed") =>
		typeof error === "object" && error !== null && "message" in error ? String(error.message) : fallback,
}));
```

Change `renderPanel` (line 57) to accept an optional session:

```tsx
function renderPanel(card: WorkCard = scheduledCard, session?: WorkspaceSession) {
	render(
		<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
			<WorkCardFocusPanel card={card} projectId="proj-1" session={session} theme="dark" daemonReady onClose={vi.fn()} />
		</QueryClientProvider>,
	);
}
```

Add `getMock` to the `beforeEach` reset (line 66), returning a Director status payload:

```tsx
	getMock.mockReset().mockResolvedValue({
		data: { projectId: "proj-1", daemonReady: true, runningCount: 1, todoCount: 2, wipLimit: 4 },
		error: undefined,
	});
```

- [ ] **Step 2: Write the failing test**

Append a new `describe` block to `frontend/src/renderer/components/WorkCardFocusPanel.test.tsx`:

```tsx
describe("WorkCardFocusPanel commander display", () => {
	const runningCard: WorkCard = { ...scheduledCard, id: "card_2", status: "running", scheduledAt: undefined, sessionId: "test-13" };

	it("shows a Director status summary instead of a commander terminal", async () => {
		renderPanel(runningCard, { id: "test-13", kind: "orchestrator", harness: "director" } as WorkspaceSession);

		expect(await screen.findByText("Commander")).toBeInTheDocument();
		expect(screen.getByText(/director/)).toBeInTheDocument();
		expect(screen.getByText("running")).toBeInTheDocument();
		expect(screen.queryByRole("button", { name: /commander terminal/i })).not.toBeInTheDocument();
		expect(screen.queryByRole("button", { name: /live terminal/i })).not.toBeInTheDocument();
	});

	it("reports the project's board counts for a commander", async () => {
		renderPanel(runningCard, { id: "test-13", kind: "orchestrator", harness: "director" } as WorkspaceSession);

		await waitFor(() => expect(screen.getByText(/1 running · 2 to do · WIP 4/)).toBeInTheDocument());
	});

	it("still offers a live terminal for a worker session", async () => {
		const workerCard: WorkCard = { ...runningCard, sessionId: "test-11" };
		renderPanel(workerCard, { id: "test-11", kind: "worker", harness: "codex" } as WorkspaceSession);

		expect(await screen.findByRole("button", { name: /show live terminal/i })).toBeInTheDocument();
	});
});
```

- [ ] **Step 3: Run test to verify it fails**

```bash
cd frontend && npm test -- WorkCardFocusPanel
```

Expected: FAIL — the "Commander" heading is not found, and the commander-terminal button still is.

- [ ] **Step 4: Keep the terminal off for commanders**

In `frontend/src/renderer/components/WorkCardFocusPanel.tsx`, change line 64:

```tsx
	// A commander is coordinated with, not watched: its terminal is a raw agent
	// log, and a crashed agent leaves a live-looking shell behind it (the tmux
	// runtime keeps the pane alive after the process exits). Show its state as
	// facts instead; the terminal stays reachable from Sessions.
	const canShowTerminal = LIVE_WORKFLOW_STATUSES.has(card.status) && Boolean(card.sessionId && session) && session?.kind !== "orchestrator";
```

- [ ] **Step 5: Add the status section**

Add the import at the top of the file, alongside the other `useWorkboardQuery` imports:

```tsx
	useDirectorStatus,
```

Add the query next to the existing `redoQuery` / `failureQuery` declarations (~line 177):

```tsx
	const directorStatus = useDirectorStatus(isCommander ? projectId : undefined);
```

Replace the `card.sessionId ? (...)` block inside the Status section (lines 224-232) with:

```tsx
						{card.sessionId ? (
							<div className="mt-3 space-y-1 border-t border-border pt-3">
								<p className="font-mono text-[10px] uppercase tracking-[0.08em] text-passive">{isCommander ? "Commander" : "Linked session"}</p>
								{session ? (
									isCommander ? (
										<>
											<p className="text-[12px] leading-[1.45] text-foreground">
												{session.harness} <span className="text-muted-foreground">· {card.sessionId}</span>
											</p>
											<dl className="mt-2 grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-[11px]">
												<dt className="text-passive">Phase</dt>
												<dd className="text-foreground">{card.status}</dd>
												<dt className="text-passive">Waiting on you</dt>
												<dd className="text-foreground">{card.waitingForInput ? "Yes" : "No"}</dd>
												{directorStatus.data ? (
													<>
														<dt className="text-passive">Board</dt>
														<dd className="text-foreground">{directorStatus.data.runningCount} running · {directorStatus.data.todoCount} to do · WIP {directorStatus.data.wipLimit}</dd>
														{directorStatus.data.lastDispatchAttempt?.result ? (
															<>
																<dt className="text-passive">Last dispatch</dt>
																<dd className={directorStatus.data.lastDispatchAttempt.result === "error" ? "text-destructive" : "text-foreground"}>
																	{directorStatus.data.lastDispatchAttempt.result}
																	{directorStatus.data.lastDispatchAttempt.attemptedAt ? ` · ${new Date(directorStatus.data.lastDispatchAttempt.attemptedAt).toLocaleTimeString()}` : ""}
																</dd>
															</>
														) : null}
													</>
												) : null}
											</dl>
											<p className="mt-2 text-[11px] leading-[1.45] text-muted-foreground">The commander stays responsible through review and testing. Open its terminal from Sessions.</p>
										</>
									) : (
										<p className="text-[12px] leading-[1.45] text-foreground">Session {card.sessionId}</p>
									)
								) : (
									<p className="text-[12px] leading-[1.45] text-foreground">The linked session is unavailable.</p>
								)}
							</div>
						) : <p className="mt-3 border-t border-border pt-3 text-[11px] leading-[1.45] text-passive">No session is linked to this card yet.</p>}
```

- [ ] **Step 6: Drop the commander branches from the remaining terminal copy**

Line 297 — the toggle button no longer needs a commander label, because `canShowTerminal` is now false for commanders:

```tsx
							{canShowTerminal ? <Button onClick={() => setLivePreviewOpen((open) => !open)} size="sm" variant="outline">{showTerminal ? "Hide live terminal" : "Show live terminal"}</Button> : null}
```

Lines 319-320 — same reason:

```tsx
						<p className="font-mono text-[10px] font-semibold uppercase tracking-[0.08em] text-accent">Live terminal</p>
						<p className="text-[10px] text-passive">Interactive session output</p>
```

- [ ] **Step 7: Retire the remaining "Hermes" copy**

The panel still names Hermes in three places that now describe the Director too:

- Line 242: `"Hermes commander unavailable"` → `"Commander unavailable"`
- Line 391 (`NudgeSheet`): `"Send an instruction to Hermes without changing the card column."` → `"Send an instruction to the commander without changing the card column."`
- Line 438 (`RetargetSheet`): `"Update the card goal and hand off to Hermes while keeping the card running."` → `"Update the card goal and hand it to the commander while keeping the card running."`

- [ ] **Step 8: Run tests to verify they pass**

```bash
cd frontend && npm test -- WorkCardFocusPanel
```

Expected: PASS, including both new tests.

- [ ] **Step 9: Typecheck and lint**

```bash
cd frontend && npm run typecheck && npm run lint
```

Expected: both PASS.

- [ ] **Step 10: Look at it**

From inside this session:

```bash
ao preview
```

Open a running card's focus panel in the Browser tab of the inspector rail and confirm: the Commander block shows harness, session id, phase, and board counts; no commander-terminal button; a worker-linked card still offers "Show live terminal".

- [ ] **Step 11: Commit**

```bash
git add frontend/src/renderer/components/WorkCardFocusPanel.tsx frontend/src/renderer/components/WorkCardFocusPanel.test.tsx
git commit -m "feat(workboard): show Director status instead of the commander terminal

The commander terminal is a raw agent log, and a crashed agent leaves a
live-looking shell behind it — so it read as a healthy commander when it
was nothing. Show harness, phase, waiting state and the project's last
dispatch attempt instead; the terminal stays reachable from Sessions."
```

---

### Task 6: Give the Director's provider API key a field in the app

`createDirectorModel` (`director/src/model.ts`) throws at startup unless the configured provider's key is in the process environment:

| `AO_DIRECTOR_MODEL` prefix | Required env var |
|---|---|
| `anthropic:` (the default, `anthropic:claude-sonnet-4-6`) | `ANTHROPIC_API_KEY` |
| `openai:` | `OPENAI_API_KEY` |
| `openrouter:` | `OPENROUTER_API_KEY` |

There is no field for any of them anywhere in the renderer. The only way to supply one today is to export it in the shell that launches the app and rely on tmux inheriting it — invisible, and lost the moment the app is started from Finder.

The backend already carries this: `domain.ProjectConfig.Env` (`backend/internal/domain/projectconfig.go:32`) is written verbatim into every session's environment by `spawnEnv` (`backend/internal/session_manager/manager.go:1528-1532`), and `PUT /api/v1/projects/{id}/config` accepts the whole `ProjectConfig` including `env`. This task is renderer-only.

`SettingsBody`'s save already spreads `...config` first and its comment says so explicitly — *"PUT replaces the whole config; merge the edited fields over what loaded so we don't drop env/symlinks/postCreate the form doesn't expose"* — so writing into `env` is a one-key merge, not a new plumbing path.

**Files:**
- Modify: `frontend/src/renderer/components/ProjectSettingsForm.tsx`
- Test: `frontend/src/renderer/components/ProjectSettingsForm.test.tsx`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `directorKeyEnvVar(model: string): string` — module-private to `ProjectSettingsForm.tsx`; no other task depends on it.

- [ ] **Step 1: Write the failing test**

Read `frontend/src/renderer/components/ProjectSettingsForm.test.tsx` first and reuse its existing render helper, `apiClient` mock and project fixture verbatim. Append:

```tsx
describe("ProjectSettingsForm director API key", () => {
	it("saves the key under the env var the configured provider needs", async () => {
		renderSettings({
			config: {
				worker: { agent: "codex" },
				director: { agent: "director" },
				agentConfig: { model: "anthropic:claude-sonnet-4-6" },
			},
		});

		const input = await screen.findByLabelText(/director api key/i);
		await userEvent.type(input, "sk-ant-test");
		await userEvent.click(screen.getByRole("button", { name: /save/i }));

		await waitFor(() => expect(putMock).toHaveBeenCalled());
		const body = putMock.mock.calls.at(-1)?.[1]?.body;
		expect(body.config.env).toMatchObject({ ANTHROPIC_API_KEY: "sk-ant-test" });
	});

	it("switches the env var with the provider", async () => {
		renderSettings({
			config: {
				worker: { agent: "codex" },
				director: { agent: "director" },
				agentConfig: { model: "openrouter:minimax/minimax-m2" },
			},
		});

		const input = await screen.findByLabelText(/director api key/i);
		await userEvent.type(input, "sk-or-test");
		await userEvent.click(screen.getByRole("button", { name: /save/i }));

		await waitFor(() => expect(putMock).toHaveBeenCalled());
		const body = putMock.mock.calls.at(-1)?.[1]?.body;
		expect(body.config.env).toMatchObject({ OPENROUTER_API_KEY: "sk-or-test" });
		expect(body.config.env.ANTHROPIC_API_KEY).toBeUndefined();
	});

	it("does not offer the field when the Director is not the commander", async () => {
		renderSettings({ config: { worker: { agent: "codex" }, orchestrator: { agent: "hermes" } } });

		await screen.findByLabelText(/default worker agent/i);
		expect(screen.queryByLabelText(/director api key/i)).not.toBeInTheDocument();
	});
});
```

If the existing file's helper is not named `renderSettings` or its PUT spy not `putMock`, adapt these three tests to whatever that file already uses — do not add a second set of mocks.

- [ ] **Step 2: Run test to verify it fails**

```bash
cd frontend && npm test -- ProjectSettingsForm
```

Expected: FAIL — `Unable to find a label with the text of: /director api key/i`.

- [ ] **Step 3: Add the provider → env-var mapping**

In `frontend/src/renderer/components/ProjectSettingsForm.tsx`, add near the other module-level helpers (above `SettingsBody`):

```tsx
/** The env var the Director's bundled model factory reads for a given engine
 * string. Mirrors createDirectorModel in director/src/model.ts, which throws at
 * startup when the key is absent — so a wrong name here is a Director that will
 * not boot. Keep the two in step. */
function directorKeyEnvVar(model: string): string {
	const provider = model.split(":")[0]?.trim().toLowerCase();
	if (provider === "openai") return "OPENAI_API_KEY";
	if (provider === "openrouter") return "OPENROUTER_API_KEY";
	return "ANTHROPIC_API_KEY"; // the Director's default engine is anthropic:
}

const DIRECTOR_KEY_ENV_VARS = ["ANTHROPIC_API_KEY", "OPENAI_API_KEY", "OPENROUTER_API_KEY"];
```

- [ ] **Step 4: Track the key in form state**

In `SettingsBody`, add to the `useState` initialiser (after `permissions:`, line ~92):

```tsx
		directorKey: config.env?.[directorKeyEnvVar(config.agentConfig?.model ?? "")] ?? "",
```

- [ ] **Step 5: Write the key into the saved config**

In `SettingsBody`'s `mutationFn`, immediately before `const next: ProjectConfig = {`:

```tsx
				// One key at a time: switching the engine must not leave the previous
				// provider's key behind, where it would look configured and never be read.
				const keyVar = directorKeyEnvVar(form.model);
				const env: Record<string, string> = { ...config.env };
				for (const name of DIRECTOR_KEY_ENV_VARS) delete env[name];
				if (workboardEnabled && form.directorKey.trim()) env[keyVar] = form.directorKey.trim();
```

Then add to the `next` object literal, after the `...config` spread and alongside the other edited fields:

```tsx
					env: Object.keys(env).length > 0 ? env : undefined,
```

- [ ] **Step 6: Add the field**

In the Agents card, immediately after the `Permission mode` `Field` (line ~323):

```tsx
						{workboardEnabled ? (
							<Field label="Director API key" htmlFor="directorKey">
								<input
									id="directorKey"
									type="password"
									autoComplete="off"
									className="h-8 w-full rounded-md border border-input bg-transparent px-2.5 text-[13px] text-foreground placeholder:text-passive focus-visible:border-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent-weak"
									value={form.directorKey}
									onChange={(e) => setForm((f) => ({ ...f, directorKey: e.target.value }))}
									placeholder={directorKeyEnvVar(form.model)}
								/>
								<p className="mt-1 text-[12px] leading-5 text-muted-foreground">
									Stored as <code>{directorKeyEnvVar(form.model)}</code> in this project's environment. The Director will not start without it.
								</p>
							</Field>
						) : null}
```

- [ ] **Step 7: Run tests to verify they pass**

```bash
cd frontend && npm test -- ProjectSettingsForm
```

Expected: PASS, including the three new tests.

- [ ] **Step 8: Typecheck and lint**

```bash
cd frontend && npm run typecheck && npm run lint
```

Expected: both PASS.

- [ ] **Step 9: Verify the key actually reaches a Director session**

Save a key in project settings, then confirm it landed in the stored config:

```bash
sqlite3 ~/.ao/data/ao.db "select json_extract(config, '\$.env') from projects where id='test';"
```

Expected: `{"ANTHROPIC_API_KEY":"..."}` (or the provider you configured).

- [ ] **Step 10: Commit**

```bash
git add frontend/src/renderer/components/ProjectSettingsForm.tsx frontend/src/renderer/components/ProjectSettingsForm.test.tsx
git commit -m "feat(settings): add a Director API key field

createDirectorModel throws at startup without the configured provider's
key, and the app offered no way to supply one — the only route was
exporting it in the shell that launched the app. ProjectConfig.Env
already flows into every session's environment; this exposes the one key
the Director needs, named after the engine actually selected."
```

---

## Verification

Run after Task 6.

- [ ] **Full suites**

```bash
cd backend && go test ./internal/... && \
cd ../director && npm run typecheck && npm test && \
cd ../frontend && npm run typecheck && npm test
```

Expected: all PASS.

- [ ] **Live: a bad Director spawn is now refused, not silently dead**

```bash
ao spawn --project test --name dir-probe --agent director --prompt "probe"
```

Expected: a non-zero exit naming `director harness requires a work card id`. Confirm no new session row:

```bash
sqlite3 ~/.ao/data/ao.db "select id, harness, is_terminated from sessions where harness='director' order by created_at desc limit 3;"
```

- [ ] **Live: a Director-dispatched card reaches its commander**

Precondition: the project has a Director API key saved (Task 6). Without it the Director exits at startup with `ANTHROPIC_API_KEY is required for the configured Director model` and the checks below cannot pass.

Create a card on a project whose config has `director.agent = "director"`, let dispatch claim it, then confirm the Director process is actually running and only one commander exists:

```bash
ps aux | grep -c "[d]irector/dist/index.js"          # expect 1
tmux list-panes -a -F "#{session_name} #{pane_current_command}"   # the Director's pane is node, not zsh
```

- [ ] **Live: a phase completion moves the card**

Have the card's worker record and report a handoff, then confirm the Director acted on it:

```bash
sqlite3 ~/.ao/data/ao.db "select kind, substr(payload,1,80), created_at from work_card_events where card_id='<card>' order by created_at;"
```

Expected: an `agent_handoff` row followed by an `agent_transition` row. Both counts are `0` today — a non-zero `agent_transition` is the proof this plan set out to produce.

## Out of scope

- **The tmux `; exec $SHELL` tail** (`backend/internal/adapters/runtime/tmux/tmux.go:484`) keeps a pane alive after the agent exits, so every crashed agent leaves a session that reports itself live. It is deliberate — an operator can inspect the terminal afterwards — and it affects all ten harnesses, not just the Director. Task 1 removes the one case that produced a dead commander by construction; Task 5 stops the UI implying liveness it cannot know. A real fix (record the agent's exit separately from pane liveness) is its own change.
- **Stall detection for the Director.** Task 3 makes the Director eligible for stall nudges, but the nudge text (`stall_nudge.go`) is written for a Hermes CLI: *"Run `ao workboard get ...`"*. The Director takes tool calls, not shell commands. The message should be rewritten for it — separate change.
- **`ProjectConfig` validation** rejecting `orchestrator.agent` and `director.agent` both set. Task 2 makes the Director win at runtime, which resolves the conflict; refusing the combination at config-write time is a separate, larger change across the config surface and its migration.
