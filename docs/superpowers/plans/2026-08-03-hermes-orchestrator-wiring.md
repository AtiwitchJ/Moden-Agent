# Hermes Director Orchestrator: Last-Mile Wiring Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Finish connecting the already-built pieces of `.hermes/plans/2026-08-02_223800-hermes-director-orchestrator.md` (Tasks 1–6, 10-client, 12, 13 are committed on this branch) so a work card actually cycles `Running → Review → Testing → Done` with the daemon spawning the right agent per phase, instead of the orchestrator being permanently disabled (`Orchestrator: nil`) and its launcher discarding every spawn.

**Architecture:** This does not design anything new. It closes five concrete, verified gaps between code that already exists and code that already expects it:

1. A test bug (`TestOrchestratorWiring_PeriodicTickFires` panics — negative `WaitGroup` counter) blocks a clean baseline.
2. `commander/orchestrator.ConfiguredOrchestrator.getCard` calls `ListWorkCards(ctx, "", "")`, which returns zero rows against real SQLite (`WHERE project_id = '' AND board_id = ''` never matches). `OnAgentFailed`/`OnAgentCompleted`/`OnSignal` are unreachable against real storage today.
3. `*sqlite.Store` has no `GetActiveSession`/`InsertActiveSession`/`DeleteActiveSession` methods — only the raw sqlc `Queries` do. `commander.OrchestratorStore` and `commander/spawner.ActiveSessionStore` cannot be satisfied by real storage yet.
4. `commander/spawner.RegistryLauncher.Spawn` builds a launch command and then explicitly discards it (`_ = argv`), returning a fake `SessionHandle{ID: spec.CardID}`. No process is ever started.
5. `daemon.go:105` passes `OrchestratorConfig{Orchestrator: nil}` — `orchestrator.New(...)` is never called, so none of the above matters at runtime regardless.
6. `POST /workboard/cards/{cardId}/events` (Task 10's server half) is not mounted, so the five `ao workboard card ...` CLI commands that already ship (Task 10 client) all return "not yet implemented." Without this, an agent has no way to report a phase done and move the card forward.

**Tech Stack:** Go 1.x, chi router, sqlc + SQLite, existing daemon CDC/tick wiring in `internal/daemon`, table tests in the style of `internal/commander/orchestrator/orchestrator_test.go` and `internal/commander/spawner/spawner_test.go`.

## Global Constraints

- Keep every change surgical and tied to the task; no drive-by cleanup, renames, or speculative abstractions.
- Do not modify already-merged SQLite migrations. This plan adds none — `active_session` already exists (migration `0036_active_session.sql`).
- Do not hand-edit `backend/internal/storage/sqlite/gen/*`. It is already correct; this plan only adds `*sqlite.Store` wrapper methods around the existing generated queries.
- API contracts are code-first: edit `backend/internal/httpd/controllers/dto.go` and `backend/internal/httpd/apispec/specgen/build.go`, then run `npm run api` from the repo root and commit `backend/internal/httpd/apispec/openapi.yaml` plus `frontend/src/api/schema.ts` with the Go change.
- Conventional commit messages (`feat:`, `fix:`, `docs:`, `test:`, `chore:`).
- The daemon is loopback-only; do not touch bind-host configuration.
- `storage/sqlite/store` must not import `commander` or `commander/*`. Any type conversion between storage-native shapes and `commander/orchestrator` named types belongs in a `daemon`-package adapter, not in the store.
- **Explicitly out of scope** (do not touch): `commander/orchestrator/recovery.go`'s `checkOrphans`/`spawnOrphanReplacement` (defined and unit-tested, but never called from `Tick` — `tickActiveCards`'s own `isSessionLive` timeout check is the orphan-recovery path that is actually wired; `checkOrphans` is redundant leftover from an earlier iteration of Task 6 and is a separate cleanup, not part of last-mile wiring). Tasks 7–9 (PR/CI watcher, reviewer invoker, testing invoker) and the three `TestLifecycleDispatcherIsUnwired_*` tests in `internal/service/workboard/lifecycle_dispatcher_test.go` — those tests assert automatic, signal-driven phase advancement, which is exactly what Tasks 7–9 build; they stay red until that separate, larger effort lands. Do not modify, delete, or "fix" them in this plan.

---

## File Structure

- `backend/internal/daemon/orchestrator_wiring_test.go` (modify) — fix the panicking test.
- `backend/internal/commander/orchestrator/orchestrator.go` (modify) — fix `getCard`; add `GetWorkCard` to `OrchestratorStore`.
- `backend/internal/commander/orchestrator/orchestrator_test.go` (modify) — extend `fakeStore` with `GetWorkCard`.
- `backend/internal/storage/sqlite/store/active_session_store.go` (create) — `*sqlite.Store` wrapper methods for the `active_session` table.
- `backend/internal/storage/sqlite/store/active_session_store_test.go` (create) — round-trip tests.
- `backend/internal/commander/spawner/spawner.go` (modify) — replace `RegistryLauncher` with a launcher that spawns a real session.
- `backend/internal/commander/spawner/spawner_test.go` (modify) — update the launcher construction in existing tests to the new type.
- `backend/internal/daemon/orchestrator_store_adapter.go` (create) — adapts `*sqlite.Store`'s plain row type to `commander/orchestrator.OrchestratorStore` and `commander/spawner.ActiveSessionStore`.
- `backend/internal/daemon/orchestrator_store_adapter_test.go` (create) — adapter round-trip test against a real `sqlite.Open` store.
- `backend/internal/daemon/daemon.go` (modify) — construct the real orchestrator and pass it instead of `nil`.
- `backend/internal/service/workboard/agent_events.go` (create) — `Service.RecordAgentEvent`, the durable write behind the events route (Task 10 server half).
- `backend/internal/service/workboard/agent_events_test.go` (create) — tests for kind validation, payload validation, and the transition side effect.
- `backend/internal/httpd/controllers/dto.go` (modify) — request shape for the events route.
- `backend/internal/httpd/controllers/workboard.go` (modify) — mount and handle `POST /api/v1/workboard/cards/{cardId}/events`.
- `backend/internal/httpd/apispec/specgen/build.go` (modify) — register the new operation and its named schema.

---

### Task 1: Fix the `OrchestratorWiring` test panic

**Files:**
- Modify: `backend/internal/daemon/orchestrator_wiring_test.go`

**Interfaces:** None — this is a self-contained test fix, no exported surface changes.

The test constructs `OrchestratorWiring{...}` as a struct literal and starts `wiring.run(...)` in a goroutine directly, bypassing `WireOrchestrator`'s `wiring.wg.Add(1)` call. `run`'s `defer w.wg.Done()` then calls `Done()` on a `WaitGroup` that was never `Add`ed to, which panics ("negative WaitGroup counter").

- [ ] **Step 1: Reproduce the failure**

Run: `cd backend && go test ./internal/daemon/ -run TestOrchestratorWiring_PeriodicTickFires -v`
Expected: FAIL — panic: `sync: negative WaitGroup counter`, stack trace through `orchestrator_wiring.go:76` (`defer w.wg.Done()` inside `run`).

- [ ] **Step 2: Fix the test**

In `backend/internal/daemon/orchestrator_wiring_test.go`, find where `TestOrchestratorWiring_PeriodicTickFires` builds the wiring:

```go
	// Create wiring directly with a short tick interval for testing
	wiring := &OrchestratorWiring{
		orchestrator:  fake,
		cdcSub:       SubscribeWorkCardChanges(ctx, bcast, log),
		tickInterval: 100 * time.Millisecond,
		stopCh:       make(chan struct{}),
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		wiring.run(ctx, store, log)
	}()
```

Replace with a version that mirrors what `WireOrchestrator` itself does — call `wiring.wg.Add(1)` before starting `run`, so `run`'s own `defer w.wg.Done()` has a matching `Add`. Keep the test's own `wg`/goroutine wrapper as-is; it is a separate, outer synchronization point the test uses to know when `run` returned, not a substitute for the wiring's own internal accounting:

```go
	// Create wiring directly with a short tick interval for testing
	wiring := &OrchestratorWiring{
		orchestrator:  fake,
		cdcSub:       SubscribeWorkCardChanges(ctx, bcast, log),
		tickInterval: 100 * time.Millisecond,
		stopCh:       make(chan struct{}),
	}
	wiring.wg.Add(1) // run()'s own defer wg.Done() requires a matching Add, same as WireOrchestrator does.

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		wiring.run(ctx, store, log)
	}()
```

- [ ] **Step 3: Verify the fix**

Run: `cd backend && go test ./internal/daemon/ -run TestOrchestratorWiring -v`
Expected: PASS — both `TestOrchestratorWiring_NilOrchestratorReturnsNilWiring` (or whatever the sibling test is named) and `TestOrchestratorWiring_PeriodicTickFires`.

- [ ] **Step 4: Run the full daemon package suite**

Run: `cd backend && go test ./internal/daemon/...`
Expected: PASS, no new failures.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/daemon/orchestrator_wiring_test.go
git commit -m "fix(daemon): call wg.Add before starting OrchestratorWiring.run in test"
```

---

### Task 2: Fix `getCard` against real storage and extend `OrchestratorStore`

**Files:**
- Modify: `backend/internal/commander/orchestrator/orchestrator.go`
- Modify: `backend/internal/commander/orchestrator/orchestrator_test.go`

**Interfaces:**
- Consumes: `*sqlite.Store`'s existing `GetWorkCard(ctx context.Context, id string) (domain.WorkCard, bool, error)` (already implemented, `backend/internal/storage/sqlite/store/workboard_store.go:32`).
- Produces: `OrchestratorStore` interface gains one method:
  `GetWorkCard(ctx context.Context, id string) (domain.WorkCard, bool, error)`.
  `getCard`'s behavior is unchanged from the caller's perspective (same return shape), only its implementation changes.

`getCard` currently does:

```go
func (o *ConfiguredOrchestrator) getCard(ctx context.Context, cardID string) (domain.WorkCard, bool, error) {
	cards, err := o.store.ListWorkCards(ctx, "", "")
	if err != nil {
		return domain.WorkCard{}, false, err
	}
	for _, c := range cards {
		if c.ID == cardID {
			return c, true, nil
		}
	}
	return domain.WorkCard{}, false, nil
}
```

`ListWorkCards(ctx, "", "")` maps to `ListWorkCardsByProject` (`backend/internal/storage/sqlite/queries/workboard.sql:13`): `WHERE project_id = ? AND board_id = ?`. Called with two empty strings, it matches nothing in real SQLite — every card has a non-empty `project_id`/`board_id`. The in-package test fake (`orchestrator_test.go`'s `fakeStore.ListWorkCards`) happens to special-case an empty `projectID` as "return everything," which is why the existing orchestrator tests pass today despite this bug — they never exercise real storage.

- [ ] **Step 1: Write the failing test**

Add to `backend/internal/commander/orchestrator/orchestrator_test.go`, alongside the other `OnAgentCompleted`/`OnAgentFailed` tests in that file (reuse the existing `fakeStore`/`fakeSpawner` construction helpers already in the file — do not add new fakes):

```go
func TestGetCardUsesGetWorkCard_NotListWorkCardsWithEmptyFilters(t *testing.T) {
	store := newFakeStore()
	store.cards["card-1"] = domain.WorkCard{
		ID: "card-1", ProjectID: "p1", Status: domain.CardStatusRunning,
		CodingAgent: "hermes",
	}
	// listWorkCardsCalls with empty projectID/boardID must be zero: getCard must
	// go through GetWorkCard, not ListWorkCards(ctx, "", "").
	orc := New(Config{Store: store, Spawner: &fakeSpawner{}})

	err := orc.OnAgentCompleted(context.Background(), "card-1", commander.AgentResult{
		Phase: commander.PhaseCoding, Verdict: "approved",
	})
	if err != nil {
		t.Fatalf("OnAgentCompleted: %v", err)
	}
	if got := store.cards["card-1"].Status; got != domain.CardStatusReview {
		t.Fatalf("status = %s, want review", got)
	}
}
```

This test alone does not prove the fix (the existing `fakeStore.ListWorkCards` already tolerates empty filters, so it would pass either way). The real proof is the interface change forcing `fakeStore` to implement `GetWorkCard` — the compiler is the check here, not a runtime assertion. Add `GetWorkCard` to `fakeStore` in the same file:

```go
func (s *fakeStore) GetWorkCard(_ context.Context, id string) (domain.WorkCard, bool, error) {
	c, ok := s.cards[id]
	return c, ok, nil
}
```

- [ ] **Step 2: Run the test to verify it fails to compile**

Run: `cd backend && go test ./internal/commander/orchestrator/... -run TestGetCardUsesGetWorkCard -v`
Expected: FAIL to build — `*fakeStore does not implement OrchestratorStore (missing method GetWorkCard)` is not yet true (fakeStore doesn't declare it as required yet); instead this step should currently pass trivially since `getCard` isn't calling `GetWorkCard` yet. Skip ahead: the real verification is Step 4 after the interface change forces every `OrchestratorStore` implementer to add the method.

- [ ] **Step 3: Change the interface and `getCard`**

In `backend/internal/commander/orchestrator/orchestrator.go`, add to `OrchestratorStore`:

```go
type OrchestratorStore interface {
	ListWorkCards(ctx context.Context, projectID, boardID string) ([]domain.WorkCard, error)
	GetWorkCard(ctx context.Context, id string) (domain.WorkCard, bool, error)
	GetActiveSession(ctx context.Context, cardID string) (ActiveSessionRecord, bool, error)
	InsertActiveSession(ctx context.Context, s spawner.InsertActiveSession) error
	DeleteActiveSession(ctx context.Context, cardID string) error
	UpdateWorkCard(ctx context.Context, card domain.WorkCard) error
	AppendWorkCardEvent(ctx context.Context, event domain.WorkCardEvent) error
	ListRedoCycles(ctx context.Context, cardID string) ([]domain.RedoCycle, error)
	ListSessions(ctx context.Context, projectID domain.ProjectID) ([]domain.SessionRecord, error)
}
```

Replace `getCard`'s body:

```go
func (o *ConfiguredOrchestrator) getCard(ctx context.Context, cardID string) (domain.WorkCard, bool, error) {
	return o.store.GetWorkCard(ctx, cardID)
}
```

- [ ] **Step 4: Run the full orchestrator package test suite**

Run: `cd backend && go test ./internal/commander/orchestrator/... -v`
Expected: compile error until `fakeStore` (added in Step 1) implements `GetWorkCard` — confirm it does, then PASS for every test in the package, including the new one.

- [ ] **Step 5: Verify the whole commander tree still builds**

Run: `cd backend && go build ./... && go test ./internal/commander/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/commander/orchestrator/orchestrator.go \
        backend/internal/commander/orchestrator/orchestrator_test.go
git commit -m "fix(commander): getCard uses GetWorkCard, not ListWorkCards with empty filters"
```

---

### Task 3: Add `active_session` wrapper methods to `*sqlite.Store`

**Files:**
- Create: `backend/internal/storage/sqlite/store/active_session_store.go`
- Test: `backend/internal/storage/sqlite/store/active_session_store_test.go`

**Interfaces:**
- Consumes: the existing sqlc-generated `gen.Queries.GetActiveSession(ctx, cardID) (gen.ActiveSession, error)`, `gen.Queries.InsertActiveSession(ctx, gen.InsertActiveSessionParams) error`, `gen.Queries.DeleteActiveSession(ctx, cardID) error` (`backend/internal/storage/sqlite/gen/active_session.sql.go`), and the `Store` struct's existing `qr`, `qw`, `writeMu` fields (`backend/internal/storage/sqlite/store/store.go:21-26`).
- Produces:
  - `store.ActiveSessionRow{CardID, SessionID, Phase, Agent string; CreatedAt time.Time}`
  - `(*Store).GetActiveSession(ctx context.Context, cardID string) (ActiveSessionRow, bool, error)`
  - `(*Store).InsertActiveSession(ctx context.Context, cardID, sessionID, phase, agent string, at time.Time) error`
  - `(*Store).DeleteActiveSession(ctx context.Context, cardID string) error`

This package must not import `commander` or `commander/spawner` — these methods return a plain, storage-owned `ActiveSessionRow`, not `orchestrator.ActiveSessionRecord`. Task 5's adapter converts between them.

- [ ] **Step 1: Write the failing test**

Create `backend/internal/storage/sqlite/store/active_session_store_test.go`. Follow the pattern of other store tests in this package (`t.TempDir()` + `sqlite.Open`) — check `workboard_store_test.go` or `session_store_test.go` in this same package for the exact `Open`/cleanup idiom before writing this file, and match it rather than inventing a new setup pattern.

```go
package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/modernagent/modern-agent/backend/internal/storage/sqlite"
)

func TestActiveSessionRoundTrip(t *testing.T) {
	s, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	ctx := context.Background()

	at := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	if err := s.InsertActiveSession(ctx, "card-1", "sess-1", "coding", "hermes", at); err != nil {
		t.Fatalf("InsertActiveSession: %v", err)
	}

	row, ok, err := s.GetActiveSession(ctx, "card-1")
	if err != nil {
		t.Fatalf("GetActiveSession: %v", err)
	}
	if !ok {
		t.Fatal("GetActiveSession: ok = false, want true")
	}
	if row.CardID != "card-1" || row.SessionID != "sess-1" || row.Phase != "coding" || row.Agent != "hermes" {
		t.Fatalf("row = %+v, want card-1/sess-1/coding/hermes", row)
	}
	if !row.CreatedAt.Equal(at) {
		t.Fatalf("createdAt = %v, want %v", row.CreatedAt, at)
	}

	if err := s.DeleteActiveSession(ctx, "card-1"); err != nil {
		t.Fatalf("DeleteActiveSession: %v", err)
	}
	if _, ok, err := s.GetActiveSession(ctx, "card-1"); err != nil || ok {
		t.Fatalf("after delete: ok=%v err=%v, want ok=false err=nil", ok, err)
	}
}

func TestGetActiveSessionMissingReturnsNotOK(t *testing.T) {
	s, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	_, ok, err := s.GetActiveSession(context.Background(), "no-such-card")
	if err != nil {
		t.Fatalf("GetActiveSession: %v", err)
	}
	if ok {
		t.Fatal("ok = true for a card with no active session, want false")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd backend && go test ./internal/storage/sqlite/store/ -run TestActiveSession -v`
Expected: FAIL to build — `s.InsertActiveSession undefined`, `s.GetActiveSession undefined`, `s.DeleteActiveSession undefined`.

- [ ] **Step 3: Write the implementation**

Create `backend/internal/storage/sqlite/store/active_session_store.go`:

```go
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/modernagent/modern-agent/backend/internal/storage/sqlite/gen"
)

// ActiveSessionRow is the store's plain projection of an active_session row.
// It is deliberately storage-native (no commander/* types) so this package
// never has to import commander — callers that need commander/orchestrator's
// ActiveSessionRecord convert at their own boundary.
type ActiveSessionRow struct {
	CardID    string
	SessionID string
	Phase     string
	Agent     string
	CreatedAt time.Time
}

// GetActiveSession returns the active session linked to a card, or ok=false
// when the card has none.
func (s *Store) GetActiveSession(ctx context.Context, cardID string) (ActiveSessionRow, bool, error) {
	row, err := s.qr.GetActiveSession(ctx, cardID)
	if errors.Is(err, sql.ErrNoRows) {
		return ActiveSessionRow{}, false, nil
	}
	if err != nil {
		return ActiveSessionRow{}, false, fmt.Errorf("get active session for card %s: %w", cardID, err)
	}
	return ActiveSessionRow{
		CardID:    row.CardID,
		SessionID: row.SessionID,
		Phase:     row.Phase,
		Agent:     row.Agent,
		CreatedAt: time.UnixMilli(row.CreatedAt),
	}, true, nil
}

// InsertActiveSession records a (card_id -> session) fact. active_session.card_id
// is unique, so a second insert for a card that already has one fails.
func (s *Store) InsertActiveSession(ctx context.Context, cardID, sessionID, phase, agent string, at time.Time) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := s.qw.InsertActiveSession(ctx, gen.InsertActiveSessionParams{
		CardID:    cardID,
		SessionID: sessionID,
		Phase:     phase,
		Agent:     agent,
		CreatedAt: at.UnixMilli(),
	}); err != nil {
		return fmt.Errorf("insert active session for card %s: %w", cardID, err)
	}
	return nil
}

// DeleteActiveSession removes the active-session fact for a card. It is a
// no-op (not an error) when the card has none.
func (s *Store) DeleteActiveSession(ctx context.Context, cardID string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := s.qw.DeleteActiveSession(ctx, cardID); err != nil {
		return fmt.Errorf("delete active session for card %s: %w", cardID, err)
	}
	return nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd backend && go test ./internal/storage/sqlite/store/ -run TestActiveSession -v && go test ./internal/storage/sqlite/store/ -run TestGetActiveSessionMissing -v`
Expected: PASS.

- [ ] **Step 5: Run the whole store package**

Run: `cd backend && go test ./internal/storage/sqlite/...`
Expected: PASS, no new failures.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/storage/sqlite/store/active_session_store.go \
        backend/internal/storage/sqlite/store/active_session_store_test.go
git commit -m "feat(storage): add active_session Store wrapper methods"
```

---

### Task 4: Make the spawner launcher actually start a session

**Files:**
- Modify: `backend/internal/commander/spawner/spawner.go`
- Modify: `backend/internal/commander/spawner/spawner_test.go`

**Interfaces:**
- Consumes: `ports.SpawnConfig{ProjectID domain.ProjectID, Kind domain.SessionKind, Harness domain.AgentHarness, Prompt, TargetPath, DisplayName string}` and a `Spawn(ctx, ports.SpawnConfig) (domain.Session, error)` method — the exact shape `*sessionsvc.Service.Spawn` already implements (`backend/internal/service/session/service.go:166`) and that `internal/service/workboard/dispatch.go`'s non-Hermes worker path already calls this same way.
- Produces: `spawner.SessionServiceLauncher{Sessions SessionSpawner}` replacing `RegistryLauncher`, where `SessionSpawner` is a new interface in this file:
  `Spawn(ctx context.Context, cfg ports.SpawnConfig) (domain.Session, error)`.

`RegistryLauncher.Spawn` currently builds `argv` via the adapter registry's `GetLaunchCommand` and then discards it (`_ = argv`), returning `SessionHandle{ID: spec.CardID}` — no process starts, and every card gets a fake "session" whose ID is just its own card ID. `RegistryLauncher` has no callers anywhere in the tree yet (`grep -rn "RegistryLauncher"` matches only its own definition file) — nothing depends on preserving its exact shape, and `spawner_test.go` tests the `AgentLauncher` interface via a `fakeLauncher`, not `RegistryLauncher` concretely, so this is a clean replacement, not a breaking change to any working caller.

- [ ] **Step 1: Write the failing test**

Add to `backend/internal/commander/spawner/spawner_test.go` (this file uses `package spawner_test`; match that):

```go
type fakeSessionSpawner struct {
	calls []ports.SpawnConfig
	out   domain.Session
	err   error
}

func (f *fakeSessionSpawner) Spawn(_ context.Context, cfg ports.SpawnConfig) (domain.Session, error) {
	f.calls = append(f.calls, cfg)
	return f.out, f.err
}

func TestSessionServiceLauncher_SpawnsRealSession(t *testing.T) {
	t.Parallel()
	sessions := &fakeSessionSpawner{out: domain.Session{
		SessionRecord: domain.SessionRecord{ID: "sess-real-1"},
	}}
	launcher := &spawner.SessionServiceLauncher{Sessions: sessions}

	card := makeCard("card-1", "proj-1", "Fix bug", "details", "/repo/app")
	handle, err := launcher.Spawn(context.Background(), spawner.SpawnSpec{
		CardID: "card-1", ProjectID: "proj-1", Phase: spawner.PhaseCoding,
		Agent: "hermes", Briefing: "do the work", ParentCard: card,
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if handle.ID != "sess-real-1" {
		t.Fatalf("handle.ID = %q, want sess-real-1 (the real session ID, not the card ID)", handle.ID)
	}
	if len(sessions.calls) != 1 {
		t.Fatalf("Sessions.Spawn calls = %d, want 1", len(sessions.calls))
	}
	got := sessions.calls[0]
	if got.ProjectID != "proj-1" || got.Harness != "hermes" || got.Prompt != "do the work" || got.TargetPath != "/repo/app" {
		t.Fatalf("SpawnConfig = %+v, want project=proj-1 harness=hermes prompt='do the work' targetPath=/repo/app", got)
	}
	if got.Kind != domain.KindWorker {
		t.Fatalf("Kind = %s, want %s", got.Kind, domain.KindWorker)
	}
}

func TestSessionServiceLauncher_RequiresAgent(t *testing.T) {
	t.Parallel()
	launcher := &spawner.SessionServiceLauncher{Sessions: &fakeSessionSpawner{}}
	_, err := launcher.Spawn(context.Background(), spawner.SpawnSpec{CardID: "card-1"})
	if err == nil {
		t.Fatal("Spawn: want error for empty Agent, got nil")
	}
}

func TestSessionServiceLauncher_PropagatesSpawnError(t *testing.T) {
	t.Parallel()
	sessions := &fakeSessionSpawner{err: errors.New("boom")}
	launcher := &spawner.SessionServiceLauncher{Sessions: sessions}
	_, err := launcher.Spawn(context.Background(), spawner.SpawnSpec{
		CardID: "card-1", ProjectID: "proj-1", Phase: spawner.PhaseReview, Agent: "codex",
	})
	if err == nil {
		t.Fatal("Spawn: want propagated error, got nil")
	}
}
```

This needs `"github.com/modernagent/modern-agent/backend/internal/ports"` added to the test file's imports if not already present — check the existing import block first.

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd backend && go test ./internal/commander/spawner/ -run TestSessionServiceLauncher -v`
Expected: FAIL to build — `undefined: spawner.SessionServiceLauncher`.

- [ ] **Step 3: Replace `RegistryLauncher` with `SessionServiceLauncher`**

In `backend/internal/commander/spawner/spawner.go`, replace the entire `RegistryLauncher` type and its `Spawn` method:

```go
// RegistryLauncher looks up the harness adapter from the agent registry and
// delegates to the adapter's launch command.
type RegistryLauncher struct {
	Reg *adapters.Registry
}

// Spawn implements AgentLauncher. It resolves the harness adapter, builds the
// launch argv via GetLaunchCommand, and returns a handle with the session ID.
// The actual process start is handled by the session runtime (tmux/pty) which
// is owned by the session service — RegistryLauncher only produces the argv.
// TODO(Task 6): replace the CardID placeholder with the real session-service
// handle once the orchestrator is wired to session creation.
func (l *RegistryLauncher) Spawn(ctx context.Context, spec SpawnSpec) (SessionHandle, error) {
	if spec.Agent == "" {
		return SessionHandle{}, errors.New("agent harness is required")
	}
	a, ok := l.Reg.Get(string(domain.AgentHarness(spec.Agent)))
	if !ok {
		return SessionHandle{}, fmt.Errorf("agent harness %q not found", spec.Agent)
	}
	agent, ok := a.(ports.Agent)
	if !ok {
		return SessionHandle{}, fmt.Errorf("adapter for %q is not an agent", spec.Agent)
	}

	argv, err := agent.GetLaunchCommand(ctx, ports.LaunchConfig{
		SessionID: spec.CardID,
		Prompt:    spec.Briefing,
		WorkspacePath: func() string {
			if spec.ParentCard != nil {
				return spec.ParentCard.TargetPath
			}
			return ""
		}(),
	})
	if err != nil {
		return SessionHandle{}, fmt.Errorf("build launch command for %s: %w", spec.Agent, err)
	}

	_ = argv // argv is produced but the actual process spawn is handled by the session runtime
	return SessionHandle{
		ID:       spec.CardID,
		NativeID: "",
	}, nil
}
```

with:

```go
// SessionSpawner is the session-service operation that actually starts a
// worker process (worktree creation, runtime launch, everything). It is the
// exact shape *sessionsvc.Service.Spawn already implements — the same one
// service/workboard's non-Hermes dispatch path uses.
type SessionSpawner interface {
	Spawn(ctx context.Context, cfg ports.SpawnConfig) (domain.Session, error)
}

// SessionServiceLauncher starts a real worker session through the session
// service for each phase spawn the orchestrator requests.
type SessionServiceLauncher struct {
	Sessions SessionSpawner
}

// Spawn implements AgentLauncher by delegating to the session service, so a
// commander spawn request produces an actual running process instead of a
// fabricated handle.
func (l *SessionServiceLauncher) Spawn(ctx context.Context, spec SpawnSpec) (SessionHandle, error) {
	if spec.Agent == "" {
		return SessionHandle{}, errors.New("agent harness is required")
	}
	var targetPath string
	if spec.ParentCard != nil {
		targetPath = spec.ParentCard.TargetPath
	}
	session, err := l.Sessions.Spawn(ctx, ports.SpawnConfig{
		ProjectID:  domain.ProjectID(spec.ProjectID),
		Kind:       domain.KindWorker,
		Harness:    domain.AgentHarness(spec.Agent),
		Prompt:     spec.Briefing,
		TargetPath: targetPath,
	})
	if err != nil {
		return SessionHandle{}, fmt.Errorf("spawn %s session for card %s: %w", spec.Phase, spec.CardID, err)
	}
	return SessionHandle{ID: string(session.ID)}, nil
}
```

Remove the now-unused `"github.com/modernagent/modern-agent/backend/internal/adapters"` import from `spawner.go` if `adapters.Registry` was only referenced by `RegistryLauncher` — check the rest of the file (the `Spawner` struct still has a `reg *adapters.Registry` field used elsewhere in `Spawn`'s harness-existence check; keep the import if that field is still there, remove only if it is not).

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd backend && go test ./internal/commander/spawner/... -v`
Expected: PASS — all three new tests plus every pre-existing test in the package (`TestSpawn_Hermes`, `TestSpawn_ClaudeCode`, etc., which exercise `Spawner` + `fakeLauncher` and are unaffected by this change).

- [ ] **Step 5: Verify the commander tree builds**

Run: `cd backend && go build ./... && go vet ./internal/commander/...`
Expected: PASS, no unused-import errors.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/commander/spawner/spawner.go \
        backend/internal/commander/spawner/spawner_test.go
git commit -m "feat(commander): spawn real sessions through the session service"
```

---

### Task 5: Wire the orchestrator into the daemon

**Files:**
- Create: `backend/internal/daemon/orchestrator_store_adapter.go`
- Test: `backend/internal/daemon/orchestrator_store_adapter_test.go`
- Modify: `backend/internal/daemon/daemon.go`

**Interfaces:**
- Consumes: `sessionSvc *sessionsvc.Service` (already constructed at `daemon.go:143`, satisfies Task 4's `spawner.SessionSpawner`), `store *sqlite.Store` (Task 3's new methods), `commander/spawner.New(launcher AgentLauncher, store ActiveSessionStore, reg *adapters.Registry, clock func() int64, newID func() string) *Spawner`, `commander/orchestrator.New(cfg orchestrator.Config) *orchestrator.ConfiguredOrchestrator`, `WireOrchestrator(ctx, OrchestratorConfig, bcast, store, log) (*OrchestratorWiring, error)` (unchanged, `backend/internal/daemon/orchestrator_wiring.go:45`).
- Produces: `daemon.orchestratorStoreAdapter{store *sqlite.Store}` implementing both `commander/orchestrator.OrchestratorStore` and `commander/spawner.ActiveSessionStore`. `daemon.go` constructs a real orchestrator instead of passing `nil`.

The adapter exists so `storage/sqlite/store` never imports `commander/*` (Global Constraints). It converts `store.ActiveSessionRow` (Task 3) to `orchestrator.ActiveSessionRecord` and adapts `spawner.InsertActiveSession`'s fields to the `(cardID, sessionID, phase, agent string, at time.Time)` signature Task 3 defined.

- [ ] **Step 1: Write the failing test**

Create `backend/internal/daemon/orchestrator_store_adapter_test.go`:

```go
package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/modernagent/modern-agent/backend/internal/commander/spawner"
	"github.com/modernagent/modern-agent/backend/internal/domain"
	"github.com/modernagent/modern-agent/backend/internal/storage/sqlite"
)

func TestOrchestratorStoreAdapter_ActiveSessionRoundTrip(t *testing.T) {
	store, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	adapter := orchestratorStoreAdapter{store: store}
	ctx := context.Background()

	if err := adapter.InsertActiveSession(ctx, spawner.InsertActiveSession{
		CardID: "card-1", SessionID: "sess-1", Phase: spawner.PhaseCoding, Agent: "hermes",
	}); err != nil {
		t.Fatalf("InsertActiveSession: %v", err)
	}

	rec, ok, err := adapter.GetActiveSession(ctx, "card-1")
	if err != nil {
		t.Fatalf("GetActiveSession: %v", err)
	}
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if rec.CardID != "card-1" || rec.SessionID != "sess-1" || rec.Phase != string(spawner.PhaseCoding) || rec.Agent != "hermes" {
		t.Fatalf("rec = %+v, want card-1/sess-1/coding/hermes", rec)
	}
	if rec.CreatedAt.IsZero() {
		t.Fatal("CreatedAt is zero, want a real timestamp")
	}
	if time.Since(rec.CreatedAt) > time.Minute {
		t.Fatalf("CreatedAt = %v, too old for a just-inserted row", rec.CreatedAt)
	}

	if err := adapter.DeleteActiveSession(ctx, "card-1"); err != nil {
		t.Fatalf("DeleteActiveSession: %v", err)
	}
	if _, ok, err := adapter.GetActiveSession(ctx, "card-1"); err != nil || ok {
		t.Fatalf("after delete: ok=%v err=%v, want ok=false err=nil", ok, err)
	}
}

func TestOrchestratorStoreAdapter_GetWorkCardDelegates(t *testing.T) {
	store, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.UpsertProject(context.Background(), domain.ProjectRecord{ID: "p1", Path: "/repo", RegisteredAt: time.Now()}); err != nil {
		t.Fatalf("UpsertProject: %v", err)
	}
	if err := store.CreateWorkCard(context.Background(), domain.WorkCard{
		ID: "card-1", ProjectID: "p1", BoardID: "default", Title: "t", Notes: "n",
		Status: domain.CardStatusRunning, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("CreateWorkCard: %v", err)
	}
	adapter := orchestratorStoreAdapter{store: store}

	card, ok, err := adapter.GetWorkCard(context.Background(), "card-1")
	if err != nil {
		t.Fatalf("GetWorkCard: %v", err)
	}
	if !ok || card.ID != "card-1" {
		t.Fatalf("card = %+v ok=%v, want card-1", card, ok)
	}
}
```

Check `store.CreateWorkCard`'s exact required fields against `backend/internal/service/workboard/service.go`'s `Create` before assuming the literal above is complete — add any field that method's tests show is required (e.g. `Priority`) so the insert does not fail validation at the DB layer.

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd backend && go test ./internal/daemon/ -run TestOrchestratorStoreAdapter -v`
Expected: FAIL to build — `undefined: orchestratorStoreAdapter`.

- [ ] **Step 3: Write the adapter**

Create `backend/internal/daemon/orchestrator_store_adapter.go`:

```go
package daemon

import (
	"context"
	"time"

	"github.com/modernagent/modern-agent/backend/internal/commander/orchestrator"
	"github.com/modernagent/modern-agent/backend/internal/commander/spawner"
	"github.com/modernagent/modern-agent/backend/internal/domain"
	"github.com/modernagent/modern-agent/backend/internal/storage/sqlite"
)

// orchestratorStoreAdapter adapts *sqlite.Store's plain row types to the
// named types commander/orchestrator.OrchestratorStore and
// commander/spawner.ActiveSessionStore require, so the storage package never
// has to import the commander package tree (Global Constraints).
type orchestratorStoreAdapter struct {
	store *sqlite.Store
}

func (a orchestratorStoreAdapter) ListWorkCards(ctx context.Context, projectID, boardID string) ([]domain.WorkCard, error) {
	return a.store.ListWorkCards(ctx, projectID, boardID)
}

func (a orchestratorStoreAdapter) GetWorkCard(ctx context.Context, id string) (domain.WorkCard, bool, error) {
	return a.store.GetWorkCard(ctx, id)
}

func (a orchestratorStoreAdapter) GetActiveSession(ctx context.Context, cardID string) (orchestrator.ActiveSessionRecord, bool, error) {
	row, ok, err := a.store.GetActiveSession(ctx, cardID)
	if err != nil || !ok {
		return orchestrator.ActiveSessionRecord{}, ok, err
	}
	return orchestrator.ActiveSessionRecord{
		CardID:    row.CardID,
		SessionID: row.SessionID,
		Phase:     row.Phase,
		Agent:     row.Agent,
		CreatedAt: row.CreatedAt,
	}, true, nil
}

func (a orchestratorStoreAdapter) InsertActiveSession(ctx context.Context, s spawner.InsertActiveSession) error {
	return a.store.InsertActiveSession(ctx, s.CardID, s.SessionID, string(s.Phase), s.Agent, time.Now())
}

func (a orchestratorStoreAdapter) DeleteActiveSession(ctx context.Context, cardID string) error {
	return a.store.DeleteActiveSession(ctx, cardID)
}

func (a orchestratorStoreAdapter) UpdateWorkCard(ctx context.Context, card domain.WorkCard) error {
	return a.store.UpdateWorkCard(ctx, card)
}

func (a orchestratorStoreAdapter) AppendWorkCardEvent(ctx context.Context, event domain.WorkCardEvent) error {
	return a.store.AppendWorkCardEvent(ctx, event)
}

func (a orchestratorStoreAdapter) ListRedoCycles(ctx context.Context, cardID string) ([]domain.RedoCycle, error) {
	return a.store.ListRedoCycles(ctx, cardID)
}

func (a orchestratorStoreAdapter) ListSessions(ctx context.Context, projectID domain.ProjectID) ([]domain.SessionRecord, error) {
	return a.store.ListSessions(ctx, projectID)
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd backend && go test ./internal/daemon/ -run TestOrchestratorStoreAdapter -v`
Expected: PASS.

- [ ] **Step 5: Wire the real orchestrator into `daemon.go`**

In `backend/internal/daemon/daemon.go`, the orchestrator wiring currently sits right after `cdcPipe` is created and before `runtimeAdapter`/`termMgr`/`messenger` exist — but constructing a real orchestrator needs `sessionSvc` (built later, at line 143) as the thing behind `SessionServiceLauncher`. Move the `WireOrchestrator` call to just after `sessionSvc` is constructed instead of where it is today.

Replace:

```go
	cdcPipe, err := startCDC(ctx, store, log)
	if err != nil {
		return err
	}

	// Orchestrator wiring: subscribes to CDC card-change events and drives periodic
	// ticks. Nil orchestrator is tolerated before Task 6 lands so boot never blocks.
	orchWiring, err := WireOrchestrator(ctx, OrchestratorConfig{Orchestrator: nil}, cdcPipe.Broadcaster, store, log)
	if err != nil {
		return fmt.Errorf("wire orchestrator: %w", err)
	}
```

with:

```go
	cdcPipe, err := startCDC(ctx, store, log)
	if err != nil {
		return err
	}
```

Then, after the block that constructs `sessionSvc` (currently ending at):

```go
	sessionSvc, reviewSvc, sessMgr, err := startSession(cfg, runtimeAdapter, store, lcStack.LCM, messenger, telemetrySink, log)
	if err != nil {
		stop()
		lcStack.Stop()
		if cdcErr := cdcPipe.Stop(); cdcErr != nil {
			log.Error("cdc pipeline shutdown", "err", cdcErr)
		}
		return fmt.Errorf("wire session service: %w", err)
	}
```

insert:

```go
	// Orchestrator wiring: subscribes to CDC card-change events and drives
	// periodic ticks that advance work cards through their phases, spawning
	// the right agent session per phase via the session service.
	orchStore := orchestratorStoreAdapter{store: store}
	orchSpawner := spawner.New(
		&spawner.SessionServiceLauncher{Sessions: sessionSvc},
		orchStore,
		nil,
		nil,
		nil,
	)
	orch := orchestrator.New(orchestrator.Config{
		Spawner:  orchSpawner,
		Store:    orchStore,
		WIPLimit: domain.DefaultWorkboardConfig().WIPLimit,
	})
	orchWiring, err := WireOrchestrator(ctx, OrchestratorConfig{Orchestrator: orch}, cdcPipe.Broadcaster, store, log)
	if err != nil {
		stop()
		lcStack.Stop()
		if cdcErr := cdcPipe.Stop(); cdcErr != nil {
			log.Error("cdc pipeline shutdown", "err", cdcErr)
		}
		return fmt.Errorf("wire orchestrator: %w", err)
	}
```

Add the two new imports this needs to `daemon.go`'s import block:

```go
	"github.com/modernagent/modern-agent/backend/internal/commander/orchestrator"
	"github.com/modernagent/modern-agent/backend/internal/commander/spawner"
```

Check `spawner.New`'s exact parameter order and types before pasting the call above — re-read `backend/internal/commander/spawner/spawner.go`'s `func New(launcher AgentLauncher, store ActiveSessionStore, reg *adapters.Registry, clock func() int64, newID func() string) *Spawner` signature (Task 4 leaves this unchanged) and pass `nil` only for the parameters that are genuinely optional (`reg`, `clock`, `newID` all nil-tolerant per `New`'s existing defaulting logic — confirm this by reading `New`'s body, not by assuming).

**`orchWiring` shutdown ordering:** the existing shutdown block near the end of `Run` already has:

```go
	if orchWiring != nil {
		orchWiring.Stop()
	}
```

placed after `<-heartbeatDone`. Leave that exactly where it is — moving the *construction* of `orchWiring` later in the function does not require moving its shutdown call, since Go closures/variables in the same function scope remain valid regardless of where in the function they were assigned, as long as assignment happens before use. Verify this compiles (Step 7) rather than assuming.

- [ ] **Step 6: Verify daemon.go still builds**

Run: `cd backend && go build ./...`
Expected: PASS. Fix any import-order or unused-variable issues gofmt/govet surfaces.

- [ ] **Step 7: Run the daemon package tests**

Run: `cd backend && go test ./internal/daemon/... -v`
Expected: PASS, including `TestOrchestratorWiring_PeriodicTickFires` (Task 1) and both new adapter tests.

- [ ] **Step 8: Run the full backend suite**

Run: `cd backend && go build ./... && go test ./...`
Expected: PASS except the three `TestLifecycleDispatcherIsUnwired_*` tests in `internal/service/workboard`, which remain red per Global Constraints (out of scope, Tasks 7-9 territory). No other package should newly fail.

- [ ] **Step 9: Commit**

```bash
git add backend/internal/daemon/orchestrator_store_adapter.go \
        backend/internal/daemon/orchestrator_store_adapter_test.go \
        backend/internal/daemon/daemon.go
git commit -m "feat(daemon): construct and wire the real Hermes director orchestrator"
```

---

### Task 6: Mount the agent report-back route

**Files:**
- Create: `backend/internal/service/workboard/agent_events.go`
- Test: `backend/internal/service/workboard/agent_events_test.go`
- Modify: `backend/internal/httpd/controllers/dto.go`
- Modify: `backend/internal/httpd/controllers/workboard.go`
- Modify: `backend/internal/httpd/apispec/specgen/build.go`

**Interfaces:**
- Consumes: `Service.Get`, `Service.Update(ctx, id string, in UpdateInput) (domain.WorkCard, error)`, `Service.store`, `Service.clock`, `Service.newID` (existing private fields on `workboard.Service`, `backend/internal/service/workboard/service.go:108-117`), and `domain.ValidateWorkflowTransition(from, to domain.CardStatus, actor string) error`.
- Produces:
  - `workboard.AgentEventInput{Kind, Payload string}`
  - `(*workboard.Service).RecordAgentEvent(ctx context.Context, cardID string, in AgentEventInput) (domain.WorkCard, error)`
  - `controllers.RecordCardEventRequest{Kind, Payload string}`
  - The route `POST /api/v1/workboard/cards/{cardId}/events`, responding `200` with `controllers.WorkCardResponse`.

Per the original Task 10 spec (`.hermes/plans/2026-08-02_223800-hermes-director-orchestrator.md:344-349`), every command "Validate transition via `ValidateWorkflowTransition` (actor=`agent`)... Write `WorkCardEvent` audit row." This route does exactly that — it is the durable-audit-plus-validated-transition boundary, not a call into the commander orchestrator's `OnAgentCompleted`/`OnAgentFailed`/`OnSignal` (those remain for a PR/CI-signal-driven path that Tasks 7-9 build later; wiring them from this route would be new design, out of scope here). The five event kinds the CLI already sends are `agent_transition`, `agent_verdict`, `agent_finding`, `test_result`, and `agent_failed` (`backend/internal/cli/workboard_card.go`). Only `agent_transition` has a side effect: it carries `{"status": "..."}`. The other four are append-only audit facts for now.

- [ ] **Step 1: Write the failing service test**

Create `backend/internal/service/workboard/agent_events_test.go`. It reuses `actionsStoreFake` from `actions_test.go`, which already satisfies `Store`, implements `AppendWorkCardEvent`, and exposes an `events` slice — do not add a second fake.

```go
package workboard

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/modernagent/modern-agent/backend/internal/domain"
)

func newAgentEventService(status domain.CardStatus) (*Service, *actionsStoreFake) {
	now := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	card := domain.WorkCard{
		ID: "card-1", ProjectID: "p1", BoardID: defaultBoardID, Title: "Fix", Notes: "Details",
		Status: status, Agent: "codex", SessionID: "sess-1",
	}
	store := &actionsStoreFake{cards: map[string]domain.WorkCard{"card-1": card}}
	svc := NewWithDeps(Deps{
		Store: store, Clock: func() time.Time { return now }, NewID: func() string { return "evt-1" },
	})
	return svc, store
}

func TestRecordAgentEventAppendsVerdict(t *testing.T) {
	svc, store := newAgentEventService(domain.CardStatusRunning)
	card, err := svc.RecordAgentEvent(context.Background(), "card-1", AgentEventInput{
		Kind:    "agent_verdict",
		Payload: `{"verdict":"approved"}`,
	})
	if err != nil {
		t.Fatalf("RecordAgentEvent: %v", err)
	}
	if card.Status != domain.CardStatusRunning {
		t.Fatalf("status = %s, want running (unchanged)", card.Status)
	}
	if len(store.events) != 1 || store.events[0].Kind != "agent_verdict" {
		t.Fatalf("events = %+v, want one agent_verdict", store.events)
	}
}

func TestRecordAgentEventAppliesTransition(t *testing.T) {
	svc, store := newAgentEventService(domain.CardStatusRunning)
	card, err := svc.RecordAgentEvent(context.Background(), "card-1", AgentEventInput{
		Kind:    "agent_transition",
		Payload: `{"status":"review","position":0}`,
	})
	if err != nil {
		t.Fatalf("RecordAgentEvent: %v", err)
	}
	if card.Status != domain.CardStatusReview {
		t.Fatalf("status = %s, want review", card.Status)
	}
	if got := store.cards["card-1"].Status; got != domain.CardStatusReview {
		t.Fatalf("persisted status = %s, want review", got)
	}
	if len(store.events) != 1 || store.events[0].Kind != "agent_transition" {
		t.Fatalf("events = %+v, want one agent_transition", store.events)
	}
}

func TestRecordAgentEventRejectsIllegalTransition(t *testing.T) {
	svc, store := newAgentEventService(domain.CardStatusRunning)
	_, err := svc.RecordAgentEvent(context.Background(), "card-1", AgentEventInput{
		Kind:    "agent_transition",
		Payload: `{"status":"done"}`,
	})
	if err == nil {
		t.Fatal("RecordAgentEvent: want error for running->done, got nil")
	}
	if !strings.Contains(err.Error(), "running") {
		t.Fatalf("error = %v, want it to name the rejected transition", err)
	}
	if got := store.cards["card-1"].Status; got != domain.CardStatusRunning {
		t.Fatalf("status = %s, want the card left in running", got)
	}
	if len(store.events) != 1 {
		t.Fatalf("events = %+v, want the attempt still recorded", store.events)
	}
}

func TestRecordAgentEventRejectsUnknownKind(t *testing.T) {
	svc, store := newAgentEventService(domain.CardStatusRunning)
	if _, err := svc.RecordAgentEvent(context.Background(), "card-1", AgentEventInput{
		Kind: "please_delete_everything",
	}); err == nil {
		t.Fatal("RecordAgentEvent: want error for unknown kind, got nil")
	}
	if len(store.events) != 0 {
		t.Fatalf("events = %+v, want nothing recorded for a rejected kind", store.events)
	}
}

func TestRecordAgentEventRejectsNonJSONPayload(t *testing.T) {
	svc, _ := newAgentEventService(domain.CardStatusRunning)
	if _, err := svc.RecordAgentEvent(context.Background(), "card-1", AgentEventInput{
		Kind:    "agent_finding",
		Payload: "not json",
	}); err == nil {
		t.Fatal("RecordAgentEvent: want error for non-JSON payload, got nil")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd backend && go test ./internal/service/workboard/ -run TestRecordAgentEvent -v`
Expected: FAIL to build — `undefined: AgentEventInput`, `svc.RecordAgentEvent undefined`.

- [ ] **Step 3: Write the service implementation**

Create `backend/internal/service/workboard/agent_events.go`:

```go
package workboard

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modernagent/modern-agent/backend/internal/domain"
	"github.com/modernagent/modern-agent/backend/internal/httpd/apierr"
)

// workCardEventAppender is the optional durable capability behind agent
// reporting, matching the cardDeleter/workCardEventLister pattern in
// service.go. *sqlite.Store satisfies it.
type workCardEventAppender interface {
	AppendWorkCardEvent(ctx context.Context, event domain.WorkCardEvent) error
}

// agentEventKinds are the reports `ao workboard card ...` can send. The set is
// closed so an agent cannot invent audit kinds the board does not understand.
var agentEventKinds = map[string]bool{
	"agent_transition": true,
	"agent_verdict":    true,
	"agent_finding":    true,
	"test_result":      true,
	"agent_failed":     true,
}

// AgentEventInput is one report an agent makes against a card.
type AgentEventInput struct {
	Kind    string
	Payload string
}

// RecordAgentEvent appends an agent report to the card's audit trail and, for
// agent_transition, applies the requested status through the same validated
// write path every other move uses. The trail is written first so a rejected
// transition still leaves evidence the agent tried.
func (s *Service) RecordAgentEvent(ctx context.Context, cardID string, in AgentEventInput) (domain.WorkCard, error) {
	card, err := s.Get(ctx, cardID)
	if err != nil {
		return domain.WorkCard{}, err
	}
	kind := strings.TrimSpace(in.Kind)
	if !agentEventKinds[kind] {
		return domain.WorkCard{}, apierr.Invalid("WORK_CARD_EVENT_KIND_INVALID", "Unknown agent event kind", nil)
	}
	if in.Payload != "" && !json.Valid([]byte(in.Payload)) {
		return domain.WorkCard{}, apierr.Invalid("WORK_CARD_EVENT_PAYLOAD_INVALID", "Payload must be JSON", nil)
	}
	appender, ok := s.store.(workCardEventAppender)
	if !ok {
		return domain.WorkCard{}, apierr.Internal("WORK_CARD_EVENTS_UNAVAILABLE", "Work card events are unavailable")
	}
	if err := appender.AppendWorkCardEvent(ctx, domain.WorkCardEvent{
		ID:        s.newID(),
		CardID:    card.ID,
		ProjectID: card.ProjectID,
		Kind:      kind,
		Payload:   in.Payload,
		CreatedAt: s.clock().UTC(),
	}); err != nil {
		return domain.WorkCard{}, fmt.Errorf("append %s event for card %s: %w", kind, card.ID, err)
	}
	if kind != "agent_transition" {
		return card, nil
	}

	var body struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(in.Payload), &body); err != nil {
		return domain.WorkCard{}, apierr.Invalid("WORK_CARD_EVENT_PAYLOAD_INVALID", "agent_transition payload needs a status", nil)
	}
	next := domain.CardStatus(strings.TrimSpace(body.Status))
	if err := domain.ValidateWorkflowTransition(card.Status, next, "agent"); err != nil {
		return domain.WorkCard{}, apierr.Invalid("WORK_CARD_TRANSITION_INVALID", err.Error(), nil)
	}
	return s.Update(ctx, cardID, UpdateInput{Status: &next})
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd backend && go test ./internal/service/workboard/ -run TestRecordAgentEvent -v`
Expected: PASS — all five tests.

- [ ] **Step 5: Add the controller DTO**

In `backend/internal/httpd/controllers/dto.go`, add next to the other work-card request shapes:

```go
// RecordCardEventRequest is the body of POST /api/v1/workboard/cards/{cardId}/events.
// Kind is one of agent_transition, agent_verdict, agent_finding, test_result,
// or agent_failed. Payload is the kind-specific JSON document, sent as a string
// so the daemon stores exactly what the agent reported.
type RecordCardEventRequest struct {
	Kind    string `json:"kind"`
	Payload string `json:"payload"`
}
```

- [ ] **Step 6: Mount the route and add the handler**

In `backend/internal/httpd/controllers/workboard.go`, add to `Register`, after the `split` line:

```go
	r.Post("/workboard/cards/{cardId}/events", c.recordEvent)
```

Extend the `WorkboardService` interface with:

```go
	RecordAgentEvent(ctx context.Context, cardID string, in workboardsvc.AgentEventInput) (domain.WorkCard, error)
```

Add the handler next to `update`:

```go
func (c *WorkboardController) recordEvent(w http.ResponseWriter, r *http.Request) {
	if c.Svc == nil {
		apispec.NotImplemented(w, r, http.MethodPost, "/api/v1/workboard/cards/{cardId}/events")
		return
	}
	var req RecordCardEventRequest
	if err := decodeJSONStrict(r, &req); err != nil {
		envelope.WriteAPIError(w, r, http.StatusBadRequest, "bad_request", "INVALID_JSON", "Invalid JSON body", nil)
		return
	}
	card, err := c.Svc.RecordAgentEvent(r.Context(), chi.URLParam(r, "cardId"), workboardsvc.AgentEventInput{
		Kind:    req.Kind,
		Payload: req.Payload,
	})
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, newWorkCardResponse(card))
}
```

- [ ] **Step 7: Register the operation in the spec generator**

In `backend/internal/httpd/apispec/specgen/build.go`, add an entry alongside the other work-card operations (model it on the `nudgeWorkCard` entry):

```go
		{
			method: http.MethodPost, path: "/api/v1/workboard/cards/{cardId}/events", id: "recordWorkCardEvent", tag: "workboard",
			summary:    "Record an agent report against a work card",
			pathParams: []any{controllers.WorkCardIDParam{}},
			reqBody:    controllers.RecordCardEventRequest{},
			resps: []respUnit{
				{http.StatusOK, controllers.WorkCardResponse{}},
				{http.StatusBadRequest, envelope.APIError{}},
				{http.StatusNotFound, envelope.APIError{}},
				{http.StatusInternalServerError, envelope.APIError{}},
				{http.StatusNotImplemented, envelope.APIError{}},
			},
		},
```

Then add the schema-name mapping to the `schemaNames` map in the same file, next to the existing `ControllersNudgeWorkCardRequest` entry:

```go
	"ControllersRecordCardEventRequest":           "RecordCardEventRequest",
```

The comment above `schemaNames` states the rule: every type reflected by `projectOperations()` needs an entry, and the drift test fails until the spec is regenerated.

- [ ] **Step 8: Regenerate the API artifacts**

Run from the repo root: `npm run api`
Expected: `backend/internal/httpd/apispec/openapi.yaml` and `frontend/src/api/schema.ts` both gain the `recordWorkCardEvent` operation.

- [ ] **Step 9: Verify spec parity and the full backend**

Run: `cd backend && go test ./internal/httpd/... && go build ./... && go test ./...`
Expected: PASS except the pre-existing three `TestLifecycleDispatcherIsUnwired_*` tests (out of scope, unchanged by this task).

- [ ] **Step 10: Verify the CLI commands no longer report "not yet implemented"**

Start the daemon (`cd backend && go run ./cmd/ao start`), then against a real card id on a registered project:

```bash
ao workboard card set-verdict <card-id> --verdict approved
```

Expected stdout: `verdict approved recorded for card <card-id>` — not the `not yet implemented` diagnostic.

- [ ] **Step 11: Commit**

```bash
git add backend/internal/service/workboard/agent_events.go \
        backend/internal/service/workboard/agent_events_test.go \
        backend/internal/httpd/controllers/dto.go \
        backend/internal/httpd/controllers/workboard.go \
        backend/internal/httpd/apispec/specgen/build.go \
        backend/internal/httpd/apispec/openapi.yaml \
        frontend/src/api/schema.ts
git commit -m "feat(workboard): mount the agent card-event reporting route (Task 10 server half)"
```

---

### Task 7: Prove the loop on the desktop

Automated tests do not cover the full daemon → orchestrator → session → CLI → daemon round trip. This task is the only evidence the wiring actually works end to end.

**Files:**
- Modify: `memory-bank/activeContext.md`
- Modify: `memory-bank/progress.md`
- Modify: `docs/STATUS.md`

- [ ] **Step 1: Run the full local verification suite**

Run from the repo root:

```bash
npm run lint
npm run frontend:typecheck
```

Expected: both pass (accounting for the three known-red `TestLifecycleDispatcherIsUnwired_*` tests, which `npm run lint`'s `go test ./...` will still report — confirm no other test newly fails).

- [ ] **Step 2: Start the app and open a project with a coding agent chain configured**

Launch the desktop app, open (or register) a project, and create one Todo card with `CodingAgent` set to an available harness (e.g. `codex`).

- [ ] **Step 3: Verify the card reaches Running with a live session**

Confirm the card auto-dispatches to Running (existing shipped auto-dispatch) and that within ~30s (the orchestrator's periodic tick) a real session appears for it — check `ao session list` or the Director card panel for an active session whose ID is a real session ID, not the card ID.

- [ ] **Step 4: Verify the agent report-back path advances the card**

From inside that live session (or via `ao` directly against the card), run:

```bash
ao workboard card transition <card-id> --to review --reason "coding phase complete"
```

Expected: succeeds, and within one orchestrator tick (~30s, or immediately via the CDC-triggered tick on the status change) a new session spawns for the Review phase — confirm via `ao session list` that a second, different session ID appears linked to the card.

- [ ] **Step 5: Verify a full card completes**

Drive the card Review → Testing → Done using `ao workboard card transition` at each phase. Confirm each transition spawns the next phase's session and the card reaches Done.

- [ ] **Step 6: Record the result in the memory bank and status doc**

Update `memory-bank/progress.md` and `memory-bank/activeContext.md` with the observed result (what worked, what didn't), and update `docs/STATUS.md` to note that the orchestrator is wired and spawning real sessions, that Tasks 7-9 (PR/CI-driven advancement) remain unbuilt, and that phase advancement today is agent-initiated via `ao workboard card transition`, not automatic.

If any step fails, record the exact failure under Blockers in `memory-bank/activeContext.md` and stop — do not mark the loop verified.

- [ ] **Step 7: Commit**

```bash
git add memory-bank/activeContext.md memory-bank/progress.md docs/STATUS.md
git commit -m "docs: record the orchestrator wiring desktop smoke result"
```

**Result of the first run of this task:** BLOCKED. Verified headlessly via CLI/API (no GUI available) using AO's inert `command` harness to avoid burning real agent API cost. Confirmed: daemon boots with the orchestrator wired, no panics; Todo→Running auto-dispatch produces a real (non-card-id) session id; `ao workboard card transition` works end-to-end. But found a reproducible, unbounded runaway-spawn bug: a single card sitting in `running` accumulated 6 live real sessions in ~12 seconds, and a 7th on transitioning to `review`. Root-caused to two compounding defects in `commander/orchestrator/tick.go` and `commander/orchestrator/orchestrator.go` (both from this plan's own Tasks 2-5, not pre-existing). Task 8 fixes them; Task 9 re-runs this verification.

---

### Task 8: Fix the runaway session-respawn bug

**Files:**
- Modify: `backend/internal/commander/orchestrator/tick.go`
- Modify: `backend/internal/commander/orchestrator/orchestrator.go`
- Modify: `backend/internal/commander/orchestrator/orchestrator_test.go`

**Interfaces:** No exported signatures change. This removes two redundant calls and simplifies one internal predicate; every caller of the touched functions is unaffected.

**Root cause (from Task 7's report, read the full report at `.superpowers/sdd/task-7-report.md` in this worktree for the complete evidence trail before starting):**

1. `commander/spawner.Spawner.Spawn` (`backend/internal/commander/spawner/spawner.go`, unchanged by this task) already calls `s.store.InsertActiveSession(ctx, insert)` on a successful launch. `tick.go`'s `spawnSession` helper — used by `spawnCodingSession`/`spawnReviewSession`/`spawnTestingSession` — calls `o.spawner.Spawn(ctx, spec)` (which does the above) and then calls `o.store.InsertActiveSession(ctx, insert)` **again** with the same `CardID`. `active_session.card_id` is a bare `PRIMARY KEY` (migration `0036_active_session.sql`) with no upsert, so this second insert always fails with `UNIQUE constraint failed: active_session.card_id`. The identical redundant-insert pattern exists in `orchestrator.go`'s `OnAgentFailed` fallback-spawn branch.
2. Because that failure propagates as an error, the tick treats the card as having no live session and retries — spawning a brand-new real process — on every subsequent tick (30s periodic, or immediately on every CDC card-change event). Even if the redundant insert were merely ignored rather than fixed, `isSessionLive` (`tick.go`) additionally compares the recorded `active_session` row's `SessionID` against `domain.WorkCard.SessionID` — a field the orchestrator's own spawn path never writes to (only the pre-existing, unrelated `dispatch.go` auto-dispatcher writes it, for a *different* session entirely). The two values can never match, so `isSessionLive` always returns `false` regardless of the insert bug, guaranteeing endless respawn on its own.

**The fix has two independent parts, both required:**

A. Remove the redundant `InsertActiveSession` call in `tick.go`'s `spawnSession` and in `orchestrator.go`'s `OnAgentFailed` fallback branch — `spawner.Spawn` already performs it.

B. In `isSessionLive`, drop the `card.SessionID != session.SessionID` comparison. `active_session` is already uniquely scoped to one row per card (`card_id` is its `PRIMARY KEY`), so `GetActiveSession(ctx, card.ID)` already returns the right row without any need to cross-check it against a field (`WorkCard.SessionID`) that belongs to a completely different spawn system (`dispatch.go`'s initial worker dispatch) and was never meant to track the orchestrator's own per-phase sessions. Only the timeout check is meaningful here.

- [ ] **Step 1: Write the failing regression test**

Add to `backend/internal/commander/orchestrator/orchestrator_test.go`, reusing the existing `fakeStore`/`fakeSpawner` in that file — do not add new fakes. Check `fakeSpawner`'s current `Spawn` method first: it needs to also call `InsertActiveSession` on the store, mirroring what the real `spawner.Spawner.Spawn` does, so this test can actually detect the double-write bug. If `fakeSpawner.Spawn` does not currently do this, add that one line to it (this makes the fake accurately model production behavior, not a scope violation — the existing fake wrongly gave the double-insert bug a place to hide because it never itself wrote to `activeSessions`).

```go
func TestTickDoesNotDoubleInsertActiveSession(t *testing.T) {
	store := newFakeStore()
	store.cards["card-1"] = domain.WorkCard{
		ID: "card-1", ProjectID: "p1", Status: domain.CardStatusRunning,
		CodingAgent: "hermes",
	}
	spawner := &fakeSpawner{store: store} // fakeSpawner.Spawn must insert into store.activeSessions, matching production spawner.Spawner.Spawn
	orc := New(Config{Store: store, Spawner: spawner})

	if err := orc.Tick(context.Background(), "p1"); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	rec, ok := store.activeSessions["card-1"]
	if !ok {
		t.Fatal("no active session recorded for card-1 after first tick")
	}
	firstSessionID := rec.SessionID

	// A second tick immediately after must NOT spawn another session: the
	// first spawn is live (fresh, well within any timeout), so tickActiveCards
	// must recognize it and do nothing.
	if err := orc.Tick(context.Background(), "p1"); err != nil {
		t.Fatalf("second Tick: %v", err)
	}
	if len(spawner.Calls) != 1 {
		t.Fatalf("spawner.Spawn called %d times across two ticks, want 1 (no runaway respawn)", len(spawner.Calls))
	}
	if store.activeSessions["card-1"].SessionID != firstSessionID {
		t.Fatalf("active session changed across ticks: %s -> %s, want stable", firstSessionID, store.activeSessions["card-1"].SessionID)
	}
}
```

Check `fakeSpawner`'s exact current field names (`Calls` or similar) in the existing file before using them literally — match what's actually there.

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd backend && go test ./internal/commander/orchestrator/ -run TestTickDoesNotDoubleInsertActiveSession -v`
Expected: FAIL. Either a build error (if `fakeSpawner.Spawn` didn't yet write to `store.activeSessions` and needs the one-line addition from Step 1), or — once that's in place — a runtime failure: `spawner.Spawn called 2 times across two ticks, want 1`, reproducing the exact runaway behavior Task 7 observed against real infrastructure.

- [ ] **Step 3: Remove the redundant insert in `tick.go`**

In `backend/internal/commander/orchestrator/tick.go`, `spawnSession` currently ends with:

```go
	handle, err := o.spawner.Spawn(ctx, spec)
	if err != nil {
		return fmt.Errorf("spawn %s agent %s for %s: %w", phase, agent, card.ID, err)
	}

	insert := spawner.InsertActiveSession{
		CardID:    card.ID,
		SessionID: handle.ID,
		Phase:     spawner.Phase(phase),
		Agent:     agent,
	}
	if err := o.store.InsertActiveSession(ctx, insert); err != nil {
		return fmt.Errorf("insert active session for %s: %w", card.ID, err)
	}

	return nil
}
```

Replace with:

```go
	// spawner.Spawn already records the (card, session, phase, agent) fact in
	// active_session on success — active_session.card_id is a PRIMARY KEY, so
	// inserting it again here always fails and (before this fix) made every
	// tick believe the spawn never happened, triggering an unbounded respawn
	// loop. Do not re-insert.
	if _, err := o.spawner.Spawn(ctx, spec); err != nil {
		return fmt.Errorf("spawn %s agent %s for %s: %w", phase, agent, card.ID, err)
	}

	return nil
}
```

If `spawner.InsertActiveSession` (the type) or the `spawner` package import become unused elsewhere in `tick.go` after this change, remove the now-dead import — check the rest of the file first; `spawner.Phase` is still used elsewhere in the file's other functions, so the import itself almost certainly stays, only this specific `insert := spawner.InsertActiveSession{...}` construction goes away.

- [ ] **Step 4: Remove the redundant insert in `orchestrator.go`'s `OnAgentFailed` fallback branch**

In `backend/internal/commander/orchestrator/orchestrator.go`, find the fallback-spawn branch inside `OnAgentFailed` (the `else` branch that spawns the next agent in the chain — currently ends with the same `spawner.InsertActiveSession` + `o.store.InsertActiveSession` pattern as Step 3). Apply the identical fix: keep the `handle, err := o.spawner.Spawn(ctx, spec)` call (the `handle` variable becomes unused if nothing else in that branch reads it — check, and if so, use `_, err :=` instead of `handle, err :=`), remove the `insert := spawner.InsertActiveSession{...}` block and its `o.store.InsertActiveSession` call, with the same explanatory comment as Step 3.

- [ ] **Step 5: Simplify `isSessionLive`**

In `backend/internal/commander/orchestrator/tick.go`, replace:

```go
// isSessionLive checks whether the active session is still running and not orphaned.
func (o *ConfiguredOrchestrator) isSessionLive(session ActiveSessionRecord, card domain.WorkCard, timeout time.Duration) bool {
	if card.SessionID == "" || card.SessionID != session.SessionID {
		return false
	}
	if o.clock().Sub(session.CreatedAt) > timeout {
		return false
	}
	return true
}
```

with:

```go
// isSessionLive checks whether the active session is still within its
// phase-appropriate timeout. active_session.card_id uniquely identifies the
// row (it is the table's primary key), so GetActiveSession(ctx, card.ID)
// already scopes to the right session — there is nothing to cross-check
// against domain.WorkCard.SessionID, which tracks a different spawn system's
// (dispatch.go's initial auto-dispatch) session, not the orchestrator's own
// per-phase spawns.
func (o *ConfiguredOrchestrator) isSessionLive(session ActiveSessionRecord, timeout time.Duration) bool {
	return o.clock().Sub(session.CreatedAt) <= timeout
}
```

Update both call sites in `tick.go`'s `tickActiveCards` (currently `o.isSessionLive(session, card, 30*time.Minute)` and `o.isSessionLive(session, card, 10*time.Minute)`, two occurrences each style) to drop the now-removed `card` argument: `o.isSessionLive(session, 30*time.Minute)` / `o.isSessionLive(session, 10*time.Minute)`.

- [ ] **Step 6: Run the regression test to verify it passes**

Run: `cd backend && go test ./internal/commander/orchestrator/ -run TestTickDoesNotDoubleInsertActiveSession -v`
Expected: PASS.

- [ ] **Step 7: Run the whole orchestrator package and the wider commander tree**

Run: `cd backend && go test ./internal/commander/... -v`
Expected: PASS — every pre-existing test in `orchestrator_test.go`, `spawner_test.go`, plus the new regression test. Pay particular attention to any existing test that constructed a `domain.WorkCard` with a `SessionID` set and asserted on `isSessionLive`/`tickActiveCards` behavior tied to that field — if one exists and now fails, read it: it was very likely asserting the buggy cross-check behavior itself, and should be updated to assert the corrected (timeout-only) behavior, not treated as a sign this fix is wrong. Do not weaken the new regression test to make an old, bug-dependent test pass.

- [ ] **Step 8: Run the full backend suite**

Run: `cd backend && go build ./... && go test ./...`
Expected: PASS except the three pre-existing, explicitly out-of-scope `TestLifecycleDispatcherIsUnwired_*` tests in `internal/service/workboard` (unaffected by this task).

- [ ] **Step 9: Commit**

```bash
git add backend/internal/commander/orchestrator/tick.go \
        backend/internal/commander/orchestrator/orchestrator.go \
        backend/internal/commander/orchestrator/orchestrator_test.go
git commit -m "fix(commander): stop the orchestrator from spawning a new session every tick"
```

---

### Task 9: Re-run the desktop smoke test

Repeat Task 7's verification (same brief: `.superpowers/sdd/task-7-brief.md`, same headless CLI/API adaptation, same isolated daemon setup, same inert `command` harness — do not use a real paid coding-agent harness for this check either, the point is to confirm the runaway is gone, not to exercise a real agent) with one additional, specific check:

- [ ] **Step 1: Repeat Task 7's steps 3-5** (Todo→Running auto-dispatch, orchestrator tick spawn, `ao workboard card transition` through review/testing/done).

- [ ] **Step 2: Specifically verify no runaway spawn** — after the card enters `running` and the orchestrator's first tick fires, wait at least 90 seconds (three full 30s tick intervals) and confirm via `ao session ls --project <id> --json` that **exactly one** session exists for the card, not a growing count. Check the daemon log for the absence of any `UNIQUE constraint failed` or repeated `insert active session` warnings.

- [ ] **Step 3: Verify a full card completes** without spawning more than one session per phase (one for coding, one for review, one for testing — three total across the whole lifecycle, not per-tick multiples of that).

- [ ] **Step 4: Clean up** exactly as Task 7's report did (kill any real sessions/processes spawned during this check, stop the daemon, confirm nothing under `~/.ao` was touched).

- [ ] **Step 5: Update the memory bank and STATUS.md** with the final, real result — supersede Task 7's BLOCKED entry with this task's outcome. If this task ALSO finds a real bug, stop and report BLOCKED again with the same precision Task 7 used — do not force a PASS.

- [ ] **Step 6: Commit**

```bash
git add memory-bank/activeContext.md memory-bank/progress.md docs/STATUS.md
git commit -m "docs: confirm the orchestrator wiring loop after the respawn-bug fix"
```

**Result of the first run of this task:** BLOCKED (commit `854cedfa`, docs-only). Task 8's fix holds for a card's first phase: 90+ seconds / three tick intervals with a flat session count and zero `UNIQUE constraint failed` errors. But transitioning the card via `ao workboard card transition` (the only phase-advance mechanism actually reachable today) exposed a second, related, still-unbounded bug: the stale `active_session` row from the completed phase is never cleaned up on manual transition — `DeleteActiveSession` is only called from `OnAgentCompleted`/`OnAgentFailed`, neither of which the transition route invokes. Once that stale row's phase-appropriate timeout elapsed (~10 minutes), the orchestrator began spawning a brand-new real session on every 30s tick indefinitely, each failing the identical `UNIQUE constraint failed` bookkeeping insert Task 7 originally reported. Task 10 fixes this; Task 11 re-runs this verification a second time.

---

### Task 10: Clear the stale `active_session` row on a manual phase transition

**Files:**
- Modify: `backend/internal/service/workboard/agent_events.go`
- Modify: `backend/internal/service/workboard/agent_events_test.go`

**Interfaces:** No exported signatures change on `RecordAgentEvent` itself. One new unexported optional-interface type, matching the existing `workCardEventAppender`/`cardDeleter` pattern already in this package.

**Root cause (from Task 9's report — read the full report at `.superpowers/sdd/task-9-report.md` in this worktree before starting):** `active_session` is keyed one row per **card** (`card_id PRIMARY KEY`, migration `0036_active_session.sql`), not per card+phase. The only code that clears or replaces that row across a phase change is `DeleteActiveSession`, called solely from `commander/orchestrator.ConfiguredOrchestrator.OnAgentCompleted` and `OnAgentFailed` (`backend/internal/commander/orchestrator/orchestrator.go`, near the top of each). Those are driven by real agent-completion signals that this plan's own scope explicitly leaves unwired (Tasks 7-9 of the original design). The only phase-advance path actually reachable today is `RecordAgentEvent`'s `agent_transition` handling (Task 6), which updates `work_cards.status` directly and never touches `active_session` — so a stale row from the completed phase survives, and the next phase's orchestrator spawn collides with it forever, exactly reproducing Task 7's original failure mode on a delay instead of immediately.

Confirmed by re-reading `domain.ValidateWorkflowTransition`: manual (`actor == "user"`) transitions are already rejected outright except `from == to`, so `RecordAgentEvent`'s `agent_transition` path (`actor == "agent"`) is the *only* place a card's status changes today — there is no other route to patch.

**The fix:** after `RecordAgentEvent` successfully applies an `agent_transition`'s status change via `s.Update`, also delete the card's `active_session` row — mirroring exactly what `OnAgentCompleted`/`OnAgentFailed` already do, using the same `DeleteActiveSession(ctx, cardID string) error` method Task 3 already added to `*sqlite.Store`. This clears the way for the orchestrator's next tick to spawn a fresh session for the new phase with a single, uncontested insert.

- [ ] **Step 1: Write the failing test**

Add to `backend/internal/service/workboard/agent_events_test.go`, reusing `actionsStoreFake` (already in the package, per the existing tests in this file) — extend it with a `DeleteActiveSession` method and a way to observe it was called, following the same minimal-fake-extension pattern used elsewhere in this codebase (e.g. `store.appended`/`store.events` fields already on this fake).

```go
func TestRecordAgentEventTransitionClearsActiveSession(t *testing.T) {
	svc, store := newAgentEventService(domain.CardStatusRunning)
	if _, err := svc.RecordAgentEvent(context.Background(), "card-1", AgentEventInput{
		Kind:    "agent_transition",
		Payload: `{"status":"review"}`,
	}); err != nil {
		t.Fatalf("RecordAgentEvent: %v", err)
	}
	if len(store.deletedActiveSessions) != 1 || store.deletedActiveSessions[0] != "card-1" {
		t.Fatalf("deletedActiveSessions = %v, want [card-1]", store.deletedActiveSessions)
	}
}

func TestRecordAgentEventNonTransitionDoesNotClearActiveSession(t *testing.T) {
	svc, store := newAgentEventService(domain.CardStatusRunning)
	if _, err := svc.RecordAgentEvent(context.Background(), "card-1", AgentEventInput{
		Kind:    "agent_verdict",
		Payload: `{"verdict":"approved"}`,
	}); err != nil {
		t.Fatalf("RecordAgentEvent: %v", err)
	}
	if len(store.deletedActiveSessions) != 0 {
		t.Fatalf("deletedActiveSessions = %v, want none for a non-transition event", store.deletedActiveSessions)
	}
}

func TestRecordAgentEventRejectedTransitionDoesNotClearActiveSession(t *testing.T) {
	svc, store := newAgentEventService(domain.CardStatusRunning)
	if _, err := svc.RecordAgentEvent(context.Background(), "card-1", AgentEventInput{
		Kind:    "agent_transition",
		Payload: `{"status":"done"}`,
	}); err == nil {
		t.Fatal("RecordAgentEvent: want error for running->done, got nil")
	}
	if len(store.deletedActiveSessions) != 0 {
		t.Fatalf("deletedActiveSessions = %v, want none for a rejected transition", store.deletedActiveSessions)
	}
}
```

Check `actionsStoreFake`'s exact current fields/methods in `actions_test.go` before adding to it — add a `deletedActiveSessions []string` field and a `DeleteActiveSession(ctx context.Context, cardID string) error` method that appends `cardID` to it and returns `nil`, matching the style of its existing methods.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd backend && go test ./internal/service/workboard/ -run TestRecordAgentEventTransitionClearsActiveSession -v`
Expected: FAIL to build (`actionsStoreFake` has no `DeleteActiveSession` method yet) until Step 1's fake extension is in place; then FAIL at runtime (`deletedActiveSessions = [], want [card-1]`) until the service change in Step 3 lands.

- [ ] **Step 3: Add the fix to `RecordAgentEvent`**

In `backend/internal/service/workboard/agent_events.go`, add the new optional-interface type near `workCardEventAppender`:

```go
// activeSessionDeleter is the optional durable capability that clears the
// orchestrator's per-card session bookkeeping when a phase transition
// happens. commander/orchestrator's own OnAgentCompleted/OnAgentFailed
// already do this for the signal-driven path; RecordAgentEvent's
// agent_transition is the only other place a card's phase changes today
// (ValidateWorkflowTransition rejects every actor="user" move), so it must
// do the same or the next phase's spawn collides with the stale row and
// never recovers. *sqlite.Store satisfies it.
type activeSessionDeleter interface {
	DeleteActiveSession(ctx context.Context, cardID string) error
}
```

Replace the function's final block:

```go
	next := domain.CardStatus(strings.TrimSpace(body.Status))
	if err := domain.ValidateWorkflowTransition(card.Status, next, "agent"); err != nil {
		return domain.WorkCard{}, apierr.Invalid("WORK_CARD_TRANSITION_INVALID", err.Error(), nil)
	}
	return s.Update(ctx, cardID, UpdateInput{Status: &next})
}
```

with:

```go
	next := domain.CardStatus(strings.TrimSpace(body.Status))
	if err := domain.ValidateWorkflowTransition(card.Status, next, "agent"); err != nil {
		return domain.WorkCard{}, apierr.Invalid("WORK_CARD_TRANSITION_INVALID", err.Error(), nil)
	}
	updated, err := s.Update(ctx, cardID, UpdateInput{Status: &next})
	if err != nil {
		return domain.WorkCard{}, err
	}
	if deleter, ok := s.store.(activeSessionDeleter); ok {
		if err := deleter.DeleteActiveSession(ctx, cardID); err != nil {
			return domain.WorkCard{}, fmt.Errorf("clear active session for card %s after transition to %s: %w", cardID, next, err)
		}
	}
	return updated, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd backend && go test ./internal/service/workboard/ -run TestRecordAgentEvent -v`
Expected: PASS — all eight tests now in `agent_events_test.go` (the five from Task 6 plus these three).

- [ ] **Step 5: Run the whole workboard package and the full backend suite**

Run: `cd backend && go build ./... && go test ./...`
Expected: PASS except the three pre-existing, explicitly out-of-scope `TestLifecycleDispatcherIsUnwired_*` tests.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/service/workboard/agent_events.go \
        backend/internal/service/workboard/agent_events_test.go
git commit -m "fix(workboard): clear active_session on agent card transitions"
```

---

### Task 11: Re-run the desktop smoke test a second time

Repeat Task 9's exact verification (same brief and setup: `.superpowers/sdd/task-9-brief.md`, headless CLI/API only, isolated daemon under a fresh scratch dir, AO's inert `command` harness — not a real paid coding-agent harness) with the fix from Task 10 in place.

- [ ] **Step 1: Repeat Task 9's steps 1-4 in full** — Todo→Running auto-dispatch, the 90-second/three-tick no-runaway check on the coding phase, then `ao workboard card transition` through review, testing, and done this time (Task 9 stopped at review once the bug was confirmed; this run should go all the way to done since the fix should hold at every phase).

- [ ] **Step 2: The critical new check** — after each transition (`--to review`, `--to testing`, `--to done`), confirm within one tick interval that exactly one new session appears for that phase, and separately confirm (via a direct query, e.g. `sqlite3 <data-dir>/ao.db "SELECT * FROM active_session"`, or the closest equivalent inspection available) that the `active_session` row for the card reflects the *current* phase, not a stale earlier one — this is the specific thing Task 9 found broken.

- [ ] **Step 3: Extend the no-runaway wait past 10 minutes on at least one phase** — Task 9's bug only manifested once a stale row's phase-appropriate timeout (10 minutes for review/testing) elapsed. After transitioning to `review`, wait at least 11 minutes (past that window) before checking, to specifically prove the timeout-elapse trigger from Task 9 no longer causes a respawn. This is slower than Task 9's original 90-second check but is the only way to prove this specific failure mode is actually closed, not just delayed further.

- [ ] **Step 4: Clean up** exactly as Tasks 7 and 9 did.

- [ ] **Step 5: Update the memory bank and STATUS.md** with the final result, superseding Task 9's BLOCKED entry. If this run ALSO finds a problem, stop and report BLOCKED with the same precision as before — do not force a PASS, and do not attempt a fix in this task.

- [ ] **Step 6: Commit**

```bash
git add memory-bank/activeContext.md memory-bank/progress.md docs/STATUS.md
git commit -m "docs: confirm the orchestrator wiring loop holds across a full phase cycle"
```

**Result of the first run of this task:** BLOCKED (no commit, verification-only). Task 8's and Task 10's fixes both confirmed still holding at their exact original trigger points — 90s/3-tick no-runaway on the coding phase, and exactly one new session with a correctly-updated `active_session` row immediately after a manual transition. But waiting past the review session's *own* 10-minute phase timeout — with no transition, no completion, the session still legitimately alive — reproduced the same failure class a third time: `isSessionLive` correctly judges a session "not live" (it is a pure age check, with no way to distinguish a stale leftover from a session that is legitimately still running past its configured ceiling), but nothing deletes the existing `active_session` row before the resulting respawn attempt, so the respawn's `InsertActiveSession` collides with the still-current, still-correct row — the identical `UNIQUE constraint failed` signature as Tasks 7 and 9, now on a third, structurally different trigger. Task 12 fixes the root cause structurally (an atomic upsert, so no future trigger path can hit this collision); Task 13 re-runs this verification a fourth and final time, covering all three known trigger paths in one pass.

---

### Task 12: Make `active_session` writes collision-safe at the root

**Files:**
- Modify: `backend/internal/storage/sqlite/queries/active_session.sql`
- Regenerate: `backend/internal/storage/sqlite/gen/active_session.sql.go` (via `npm run sqlc`, not hand-edited)
- Test: `backend/internal/storage/sqlite/store/active_session_store_test.go`

**Interfaces:** No Go signature changes anywhere. `InsertActiveSessionParams`, `Store.InsertActiveSession`, and every caller (`commander/spawner.Spawner.Spawn`, the `daemon.orchestratorStoreAdapter`) keep their exact current shape. Only the underlying SQL text changes.

**Why the root, not another per-trigger patch:** three tasks in this plan (7→8, 9→10, 11→this one) have each found and fixed one specific code path that could leave `active_session`'s single, `card_id`-keyed row stale or attempt a second insert against it — a redundant insert call (Task 8), a stale row surviving a manual transition (Task 10), and now a session's own phase timeout elapsing while nothing ever deletes its row before the resulting respawn (found by Task 11). All three are the same underlying defect wearing different triggers: `active_session.card_id` is a bare `PRIMARY KEY` (migration `0036_active_session.sql`, unchanged by this task) and `InsertActiveSession` is a plain, non-upsert `INSERT`. Every one of the orchestrator's "this session is no longer live, spawn a replacement" decisions — regardless of *why* it decided that — ends in exactly one `InsertActiveSession` call (after Task 8 removed the redundant second one), and that call has always needed to either replace whatever row is already there or fail loudly; today it does neither, it just fails silently-to-the-tick-loop and gets retried forever. Task 11's own report recommends this exact fix. A fourth trigger path will surface again if this stays patched per-site instead of fixed at the write itself.

**The fix:** change `InsertActiveSession`'s SQL from a plain `INSERT` to an upsert (`INSERT ... ON CONFLICT(card_id) DO UPDATE SET ...`). This makes every call — the original spawn, an orphan-timeout replacement, a redo/fallback respawn, anything — authoritative: whichever spawn most recently succeeded is what `active_session` reflects, unconditionally, with no possible collision. This does not require the per-card lock in `commander/spawner.Spawner` (already present, unchanged) to change — the lock already serializes concurrent `Spawn` calls for the same card, so there is no concurrent-writer race for the upsert to resolve; it exists purely to make a *sequential* second insert for the same card succeed-as-replace instead of fail-as-duplicate.

- [ ] **Step 1: Write the failing test**

Add to `backend/internal/storage/sqlite/store/active_session_store_test.go`, alongside the existing `TestActiveSessionRoundTrip`/`TestGetActiveSessionMissingReturnsNotOK` tests — reuse whatever project/card seeding helper those tests already use (check the file first, do not invent a new one):

```go
func TestInsertActiveSessionUpsertsOnConflict(t *testing.T) {
	s, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	ctx := context.Background()
	// seed whatever project + work card the existing round-trip test needs
	// for the FK on active_session.card_id -> work_cards.id — match that
	// test's exact seeding call(s) here.

	first := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	if err := s.InsertActiveSession(ctx, "card-1", "sess-1", "coding", "hermes", first); err != nil {
		t.Fatalf("first InsertActiveSession: %v", err)
	}

	second := first.Add(time.Minute)
	if err := s.InsertActiveSession(ctx, "card-1", "sess-2", "review", "codex", second); err != nil {
		t.Fatalf("second InsertActiveSession (same card_id) should upsert, not error: %v", err)
	}

	row, ok, err := s.GetActiveSession(ctx, "card-1")
	if err != nil {
		t.Fatalf("GetActiveSession: %v", err)
	}
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if row.SessionID != "sess-2" || row.Phase != "review" || row.Agent != "codex" {
		t.Fatalf("row = %+v, want the SECOND insert's values (sess-2/review/codex), not the first", row)
	}
	if !row.CreatedAt.Equal(second) {
		t.Fatalf("createdAt = %v, want %v (the second insert's timestamp)", row.CreatedAt, second)
	}

	// Exactly one row for this card_id — the upsert must not create a duplicate.
	var count int
	// Use whatever direct-query mechanism this test file already has access
	// to (if any), or add a minimal count check via a second GetActiveSession
	// call plus this package's existing test conventions — do not add a new
	// sqlc query just to count rows in a test; the not-two-rows guarantee
	// already comes from card_id being a PRIMARY KEY, so this assertion is
	// a sanity check on that constraint, not new coverage of new logic.
	_ = count
}
```

Simplify the last paragraph's assertion if this package's existing test helpers don't have a natural way to count rows directly — the `PRIMARY KEY` constraint itself is what guarantees no duplicate row can exist, so the `GetActiveSession` returning the second insert's values (not erroring, not returning the first insert's stale values) is already sufficient proof of the upsert working correctly. Do not add a new sqlc query for this test alone.

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd backend && go test ./internal/storage/sqlite/store/ -run TestInsertActiveSessionUpsertsOnConflict -v`
Expected: FAIL — `second InsertActiveSession (same card_id) should upsert, not error: ... UNIQUE constraint failed: active_session.card_id`, reproducing the exact collision Tasks 7, 9, and 11 each found in production.

- [ ] **Step 3: Change the query**

In `backend/internal/storage/sqlite/queries/active_session.sql`, replace:

```sql
-- name: InsertActiveSession :exec
INSERT INTO active_session (card_id, session_id, phase, agent, created_at)
VALUES (?, ?, ?, ?, ?);
```

with:

```sql
-- name: InsertActiveSession :exec
INSERT INTO active_session (card_id, session_id, phase, agent, created_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(card_id) DO UPDATE SET
  session_id = excluded.session_id,
  phase = excluded.phase,
  agent = excluded.agent,
  created_at = excluded.created_at;
```

Leave `GetActiveSession`, `ListActiveSessionsByPhase`, `ListActiveSessionsByAgent`, and `DeleteActiveSession` untouched — none of them need to change.

- [ ] **Step 4: Regenerate sqlc code**

Run from the repo root: `npm run sqlc`
Expected: `backend/internal/storage/sqlite/gen/active_session.sql.go` regenerates with the new SQL text embedded in its `insertActiveSession` constant; `InsertActiveSessionParams` and the `Queries.InsertActiveSession` method signature are unchanged (only the SQL string literal inside the generated file changes).

- [ ] **Step 5: Run the test to verify it passes**

Run: `cd backend && go test ./internal/storage/sqlite/store/ -run TestInsertActiveSessionUpsertsOnConflict -v`
Expected: PASS.

- [ ] **Step 6: Run the whole store package and the full backend suite**

Run: `cd backend && go test ./internal/storage/sqlite/... && go build ./... && go test ./...`
Expected: PASS except the three pre-existing, explicitly out-of-scope `TestLifecycleDispatcherIsUnwired_*` tests. Pay attention to `TestActiveSessionRoundTrip` (Task 3) and `TestOrchestratorStoreAdapter_ActiveSessionRoundTrip` (Task 5) specifically — both call `InsertActiveSession` once each and assert on the result; a single-insert upsert must behave identically to a single-insert plain `INSERT` for those, so they should pass unmodified. If either fails, that is a real regression to investigate, not something to paper over.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/storage/sqlite/queries/active_session.sql \
        backend/internal/storage/sqlite/gen/active_session.sql.go \
        backend/internal/storage/sqlite/store/active_session_store_test.go
git commit -m "fix(storage): make active_session inserts an upsert, closing every respawn-collision trigger"
```

---

**Result of Task 12's review:** Approved, and confirmed correct and necessary. But the reviewer independently traced the full respawn path and found a fourth, related, real gap: when `tickActiveCards` replaces a session because `isSessionLive` returned `false` (a phase-timeout-triggered replacement, not the initial spawn), nothing stops the superseded session — grepped `commander/orchestrator` and `commander/spawner` for `.Stop(`/`.Kill(`: zero matches. Because each successful replacement resets the new row's `created_at` to "now" (Task 12's upsert), the *crash-loop* is gone — but the codebase now leaks one real, never-terminated orphaned session every time a card's phase runs longer than its timeout (30 min for coding, 10 min for review/testing), indefinitely, for as long as that phase stays open. `internal/service/session.Service.Kill(ctx, id domain.SessionID) (bool, error)` already exists and is the established teardown path used elsewhere in this codebase (e.g. `stallmon`'s auto-kill). Task 14 wires it in; Task 15 is the final re-verification, now covering four known triggers instead of three.

---

### Task 14: Stop the superseded session on a timeout-triggered replacement

**Files:**
- Modify: `backend/internal/commander/orchestrator/orchestrator.go`
- Modify: `backend/internal/commander/orchestrator/tick.go`
- Modify: `backend/internal/commander/orchestrator/orchestrator_test.go`
- Modify: `backend/internal/daemon/daemon.go`

**Interfaces:**
- Consumes: `*sessionsvc.Service`'s existing `Kill(ctx context.Context, id domain.SessionID) (bool, error)` (`backend/internal/service/session/service.go:432-435`), already available in `daemon.go` as `sessionSvc`.
- Produces:
  - `orchestrator.SessionKiller` interface (`Kill(ctx context.Context, id domain.SessionID) (bool, error)`).
  - `orchestrator.Config` gains a `Killer SessionKiller` field (optional — nil-tolerant, matching how `Config.Spawner`'s `reg *adapters.Registry` dependency is nil-checked elsewhere in this codebase, since not every construction site needs cleanup wired, e.g. tests that don't care about it).
  - `ConfiguredOrchestrator` gains an unexported `killer SessionKiller` field, set from `cfg.Killer` in `New`.

`tickActiveCards`'s three "session exists but is not live, spawn a replacement" branches (Running/Review/Testing) currently go straight to `spawnCodingSession`/`spawnReviewSession`/`spawnTestingSession` with no cleanup step. This task adds a best-effort kill of the old session immediately before that respawn, in each of the three branches. A kill failure (the old session may already be gone — a normal, harmless case) must **not** block the replacement spawn, so it is not returned as an error.

- [ ] **Step 1: Write the failing test**

Add to `backend/internal/commander/orchestrator/orchestrator_test.go`, reusing the existing `fakeStore`/`fakeSpawner` — add a small `fakeKiller` alongside them, following the same minimal-fake style as the rest of the file:

```go
type fakeKiller struct {
	killed []domain.SessionID
	err    error
}

func (f *fakeKiller) Kill(_ context.Context, id domain.SessionID) (bool, error) {
	f.killed = append(f.killed, id)
	return f.err == nil, f.err
}

func TestTickKillsSupersededSessionOnTimeoutReplacement(t *testing.T) {
	store := newFakeStore()
	store.cards["card-1"] = domain.WorkCard{
		ID: "card-1", ProjectID: "p1", Status: domain.CardStatusRunning,
		CodingAgent: "hermes",
	}
	// Pre-seed a stale active session, older than the 30-minute Running timeout.
	store.activeSessions["card-1"] = ActiveSessionRecord{
		CardID: "card-1", SessionID: "old-sess", Phase: "coding", Agent: "hermes",
		CreatedAt: time.Now().Add(-31 * time.Minute),
	}
	spawner := &fakeSpawner{store: store}
	killer := &fakeKiller{}
	orc := New(Config{Store: store, Spawner: spawner, Killer: killer})

	if err := orc.Tick(context.Background(), "p1"); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	if len(killer.killed) != 1 || killer.killed[0] != domain.SessionID("old-sess") {
		t.Fatalf("killed = %v, want [old-sess]", killer.killed)
	}
	if len(spawner.Calls) != 1 {
		t.Fatalf("spawner.Spawn called %d times, want 1 (the replacement)", len(spawner.Calls))
	}
}

func TestTickReplacementSpawnProceedsWhenKillFails(t *testing.T) {
	store := newFakeStore()
	store.cards["card-1"] = domain.WorkCard{
		ID: "card-1", ProjectID: "p1", Status: domain.CardStatusRunning,
		CodingAgent: "hermes",
	}
	store.activeSessions["card-1"] = ActiveSessionRecord{
		CardID: "card-1", SessionID: "old-sess", Phase: "coding", Agent: "hermes",
		CreatedAt: time.Now().Add(-31 * time.Minute),
	}
	spawner := &fakeSpawner{store: store}
	killer := &fakeKiller{err: errors.New("session already gone")}
	orc := New(Config{Store: store, Spawner: spawner, Killer: killer})

	// A kill failure must not block the replacement spawn.
	if err := orc.Tick(context.Background(), "p1"); err != nil {
		t.Fatalf("Tick: %v, want nil (kill failure should not propagate)", err)
	}
	if len(spawner.Calls) != 1 {
		t.Fatalf("spawner.Spawn called %d times, want 1 (replacement still proceeds)", len(spawner.Calls))
	}
}
```

Check `fakeSpawner`'s and `fakeStore`'s exact current field names before using them literally — match what Task 8 already established in this file (`fakeSpawner{store: store}`, `store.activeSessions`, `spawner.Calls`).

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd backend && go test ./internal/commander/orchestrator/ -run TestTickKillsSupersededSession -v` and `-run TestTickReplacementSpawnProceedsWhenKillFails`
Expected: FAIL to build — `undefined: fakeKiller` / `Config` has no field `Killer` — until Step 3 lands.

- [ ] **Step 3: Add `SessionKiller` and wire it through**

In `backend/internal/commander/orchestrator/orchestrator.go`, add near the other collaborator interfaces:

```go
// SessionKiller stops a previously spawned session. Used when the
// orchestrator supersedes a session on a phase-timeout-triggered
// replacement, so the old real process doesn't leak. A Kill failure (the
// old session may already be gone — a normal, harmless case) is handled by
// the caller and never blocks the replacement spawn.
type SessionKiller interface {
	Kill(ctx context.Context, id domain.SessionID) (bool, error)
}
```

Add `Killer SessionKiller` to the `Config` struct and `killer SessionKiller` to `ConfiguredOrchestrator`, and set it in `New`:

```go
	return &ConfiguredOrchestrator{
		spawner:  cfg.Spawner,
		store:    cfg.Store,
		clock:    clock,
		newID:    newID,
		wipLimit: cfg.WIPLimit,
		killer:   cfg.Killer,
	}
```

- [ ] **Step 4: Kill the superseded session before each replacement spawn**

In `backend/internal/commander/orchestrator/tick.go`, add a helper near `isSessionLive`:

```go
// killSupersededSession best-effort stops the session a replacement spawn is
// about to replace. Nil killer or an already-gone session are both normal,
// harmless cases — this never returns an error to its caller.
func (o *ConfiguredOrchestrator) killSupersededSession(ctx context.Context, session ActiveSessionRecord) {
	if o.killer == nil || session.SessionID == "" {
		return
	}
	_, _ = o.killer.Kill(ctx, domain.SessionID(session.SessionID))
}
```

In `tickActiveCards`, each of the three `else if !o.isSessionLive(session, <timeout>) { spawnXSession(...) }` branches (Running/Review/Testing) calls `o.killSupersededSession(ctx, session)` immediately before the `spawnXSession` call. For example, the Running branch changes from:

```go
			} else if !o.isSessionLive(session, 30*time.Minute) {
				if err := o.spawnCodingSession(ctx, card, nil); err != nil {
					return err
				}
			}
```

to:

```go
			} else if !o.isSessionLive(session, 30*time.Minute) {
				o.killSupersededSession(ctx, session)
				if err := o.spawnCodingSession(ctx, card, nil); err != nil {
					return err
				}
			}
```

Apply the identical change to the Review and Testing branches (their own timeouts, their own `spawnReviewSession`/`spawnTestingSession` calls).

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd backend && go test ./internal/commander/orchestrator/ -run TestTick -v`
Expected: PASS — the two new tests plus every pre-existing `Tick`-related test in the file (including Task 8's `TestTickDoesNotDoubleInsertActiveSession`, which constructs `Config` without a `Killer` — confirm it still passes, proving `Killer` is genuinely optional/nil-tolerant, not a newly-required field that would break existing callers).

- [ ] **Step 6: Wire `sessionSvc` as the daemon's orchestrator killer**

In `backend/internal/daemon/daemon.go`, in the `orchestrator.Config{...}` construction (added by an earlier task in this plan), add:

```go
	orch := orchestrator.New(orchestrator.Config{
		Spawner:  orchSpawner,
		Store:    orchStore,
		Killer:   sessionSvc,
		WIPLimit: domain.DefaultWorkboardConfig().WIPLimit,
	})
```

`*sessionsvc.Service` already has `Kill(ctx context.Context, id domain.SessionID) (bool, error)` — confirm it satisfies `orchestrator.SessionKiller` structurally by building, not by assuming.

- [ ] **Step 7: Run the whole commander tree, then the full backend suite**

Run: `cd backend && go test ./internal/commander/... -v && go build ./... && go test ./...`
Expected: PASS except the three pre-existing, explicitly out-of-scope `TestLifecycleDispatcherIsUnwired_*` tests.

- [ ] **Step 8: Commit**

```bash
git add backend/internal/commander/orchestrator/orchestrator.go \
        backend/internal/commander/orchestrator/tick.go \
        backend/internal/commander/orchestrator/orchestrator_test.go \
        backend/internal/daemon/daemon.go
git commit -m "fix(commander): stop the superseded session on a timeout-triggered replacement"
```

---

### Task 15: Re-run the desktop smoke test a final time

Repeat Task 11's exact verification (same brief and setup: `.superpowers/sdd/task-11-brief.md`, headless CLI/API only, isolated daemon under a fresh scratch dir, AO's inert `command` harness) with both Task 12's and Task 14's fixes in place. This run should stress all four known trigger paths in one pass:

- [ ] **Step 1: Repeat Task 11's steps 1-3** (Todo→Running auto-dispatch with the pre-existing, documented, out-of-scope one-time double-spawn; 90s/3-tick no-runaway hold on coding; transition to review with exactly one new session and a correctly-updated `active_session` row).

- [ ] **Step 2: Repeat Task 11's step 4** — wait past the review phase's 10-minute timeout (11+ minutes) *without* transitioning. Confirm the orchestrator correctly replaces the "expired" session: exactly one new session appears once the timeout elapses (Task 12's fix), `active_session` updates to point at it, the daemon log shows zero `UNIQUE constraint failed` entries throughout, **and** the superseded session is actually terminated (Task 14's fix) — check `ao session ls --project <id> --json` for the old session's `isTerminated` flag flipping to `true` (or, if the read model doesn't surface that promptly, confirm via `tmux ls` that the old session's pane/process is gone) shortly after the replacement spawns, not left running alongside the new one.

- [ ] **Step 3: Continue the transition through testing and done** (`--to testing`, `--to done`), confirming one new session per phase and a correctly-updated `active_session` row after each.

- [ ] **Step 4: Clean up** exactly as Tasks 7, 9, and 11 did — this time, the cleanup step itself is part of the verification: if Task 14's fix works, there should be little or nothing left to manually kill beyond the final phase's still-active session and the documented, accepted double-spawn-on-first-entry session, since every earlier phase's session should have already been terminated by the orchestrator itself. Note in the report if anything unexpected is still running at cleanup time.

- [ ] **Step 5: Update the memory bank and STATUS.md** with the final result, superseding Task 11's BLOCKED entry. If this run finds yet another problem, stop and report BLOCKED with the same precision as before — do not force a PASS, and do not attempt a fix in this task. If everything holds, be explicit that this closes the bug class at its root (an atomic upsert for the bookkeeping collision, plus proper teardown of superseded sessions) rather than another per-trigger patch, and name the one remaining known, accepted limitation (the double-spawn-on-first-entry coordination gap between `dispatch.go` and the orchestrator, already documented in this plan's "Explicitly out of scope" section) so it isn't mistaken for a new issue by anyone reading the status later.

- [ ] **Step 6: Commit**

```bash
git add memory-bank/activeContext.md memory-bank/progress.md docs/STATUS.md
git commit -m "docs: confirm the orchestrator wiring loop holds across all known respawn triggers"
```

---

## Explicitly out of scope

- **`commander/orchestrator/recovery.go`'s `checkOrphans`.** Defined and unit-tested but never called from `Tick`. `tickActiveCards`'s own `isSessionLive` timeout check already provides orphan recovery for the phases it covers. Wiring the separate `checkOrphans` path in addition would be redundant and is a distinct cleanup decision, not part of finishing existing wiring.
- **Tasks 7-9 of the official plan** (PR/CI watcher, reviewer invoker, testing invoker) — automatic, signal-driven phase advancement. Not built; this plan does not build it. The three `TestLifecycleDispatcherIsUnwired_*` tests stay red until that separate effort lands.
- **`OnAgentCompleted`/`OnAgentFailed`/`OnSignal` wiring from the `/events` route.** Task 10's own spec text describes the route as audit-plus-validated-transition only; wiring verdict/test-result/fail-attempt into the orchestrator's fallback-chain logic is Tasks 7-9 territory (reviewer/testing invokers own reading these signals), not this plan's job.
- **Per-project WIP limit for the orchestrator.** `orchestrator.Config.WIPLimit` is a single value fixed at construction (`domain.DefaultWorkboardConfig().WIPLimit`, i.e. 4), not read per-project like `dispatch.go`'s `DispatchOnce` does. This is an existing limitation in code Tasks 2-6 already built; changing it is a design decision outside "last-mile wiring."
- **Frontend changes.** The board already renders durable facts and reacts to CDC; new sessions and transitions surface without renderer work.
- **`dispatch.go`'s auto-dispatcher and the orchestrator's tick both independently spawn a worker the first time a card enters `running`.** Found during Task 7's smoke test: `dispatch.go`'s shipped, non-Hermes worker-spawn path (Ready→Running) writes `WorkCard.SessionID` and creates a session via the `sessions` table; the orchestrator's tick loop, on the very next tick, sees no `active_session` row for that card (a separate table `dispatch.go` never writes to) and spawns its own coding session — a one-time double-spawn per card on first entry into `running`, distinct from the unbounded runaway Task 8 fixes. Task 8's fix makes the orchestrator's *own* respawning stable (one session, not infinite), but does not make it aware that `dispatch.go` may have already spawned a worker for the same card. Resolving this requires deciding which system owns the initial coding-phase spawn — a real design question, not a mechanical fix, and out of scope here.
