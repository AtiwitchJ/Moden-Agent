# Workboard Stall-Nudge Reconciler Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: use `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Cards stuck in `running`/`review`/`testing` because the Hermes commander went idle without advancing them get automatically nudged — the daemon detects the stall and sends the commander a reminder message to re-read the card and advance or block it.

**Architecture:** A new `StallNudger` reconciler in `backend/internal/service/workboard/` implementing the existing `projectReconciler` interface (`ReconcileProject(ctx, projectID) ([]string, error)`), wired into the daemon with one call to the existing `startProjectReconciler` helper in `backend/internal/daemon/workboard_wiring.go` — exactly how the Answerer and AgentSwitcher already run. Every poll tick (1 min, existing `workboardDispatchInterval`) it scans active-phase cards, finds ones whose linked commander session has been **idle** past a threshold, and sends a nudge message via the existing `Send` boundary. A `stall_nudged` work-card event provides audit + cooldown so the same card is not re-nudged every tick.

**Root cause this fixes:** The daemon only drives `todo → running` (dispatch). Advancing past `running` depends entirely on the Hermes session remembering to run `ao workboard status`. If the session goes idle first, the card sits in Running forever — nothing notices. (Full orchestrator tick-loop per `.hermes/plans/2026-08-02_223800-hermes-director-orchestrator.md` Task 6 remains the long-term fix; this is the minimal safety net until then.)

**Tech Stack:** Go (backend only). No API-contract, SQL, or frontend changes — the nudge event uses the existing `work_card_events` table via `AppendWorkCardEvent`.

## Global Constraints

- **Safety mirrors `stallmon`'s rules** (`backend/internal/observe/stallmon/stallmon.go`): `ActivityWaitingInput` is sticky — a session waiting for human input is NEVER nudged. Only `ActivityIdle` sessions are nudge candidates; `ActivityActive` means it's working — leave it alone.
- **Nudge only, never kill/respawn.** No session termination, no replacement spawning in this plan.
- **Skip `card.PausedRetarget`** — same rule the Answerer follows (`answer.go:110`).
- Thresholds are package constants (stall = 10 min idle, cooldown = 15 min between nudges for the same card). Mark with `ponytail:` comment; config knobs come later if real usage needs them.
- Follow existing package conventions: constructor with `Deps` struct + nil-default `Clock`/`NewID`, narrow store interface, table tests with fake store (see `NewAnswerer`, `answer.go:40-68`).

---

### Task 1: StallNudger reconciler with tests

**Files:**
- Create: `backend/internal/service/workboard/stall_nudge.go`
- Test: `backend/internal/service/workboard/stall_nudge_test.go`
- Modify: `backend/internal/daemon/workboard_wiring.go` (wire in — Step 7)

**Interfaces:**
- Consumes: `domain.WorkCard`, `domain.SessionRecord`, `domain.WorkCardEvent`, `domain.ActivityIdle`/`ActivityWaitingInput` (`backend/internal/domain/activity.go:11-13`), `isHermesCommander(session)` helper (`actions.go:333`, same package), `defaultBoardID` (same package).
- Produces: `NewStallNudger(d StallNudgeDeps) *StallNudger` with `ReconcileProject(ctx context.Context, projectID string) ([]string, error)` — satisfies daemon's `projectReconciler` interface (`workboard_wiring.go:45-47`) structurally.

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/service/workboard/stall_nudge_test.go`:

```go
package workboard

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/modernagent/modern-agent/backend/internal/domain"
)

type stallNudgeStore struct {
	cards    map[string]domain.WorkCard
	sessions []domain.SessionRecord
	events   map[string][]domain.WorkCardEvent
	appended []domain.WorkCardEvent
}

func (s *stallNudgeStore) ListWorkCards(_ context.Context, projectID, boardID string) ([]domain.WorkCard, error) {
	var out []domain.WorkCard
	for _, c := range s.cards {
		if c.ProjectID == projectID && c.BoardID == boardID {
			out = append(out, c)
		}
	}
	return out, nil
}

func (s *stallNudgeStore) ListSessions(_ context.Context, _ domain.ProjectID) ([]domain.SessionRecord, error) {
	return s.sessions, nil
}

func (s *stallNudgeStore) ListWorkCardEvents(_ context.Context, cardID string) ([]domain.WorkCardEvent, error) {
	return s.events[cardID], nil
}

func (s *stallNudgeStore) AppendWorkCardEvent(_ context.Context, event domain.WorkCardEvent) error {
	s.appended = append(s.appended, event)
	if s.events == nil {
		s.events = map[string][]domain.WorkCardEvent{}
	}
	s.events[event.CardID] = append(s.events[event.CardID], event)
	return nil
}

type stallNudgeSender struct {
	sent []struct {
		session domain.SessionID
		message string
	}
}

func (s *stallNudgeSender) Send(_ context.Context, id domain.SessionID, message string, _ domain.SessionID) error {
	s.sent = append(s.sent, struct {
		session domain.SessionID
		message string
	}{id, message})
	return nil
}

func stallCard(id, status string, sessionID string) domain.WorkCard {
	return domain.WorkCard{
		ID: id, ProjectID: "p1", BoardID: defaultBoardID, Title: id + " title",
		Status: domain.CardStatus(status), SessionID: sessionID,
	}
}

func idleCommander(id string, idleSince time.Time) domain.SessionRecord {
	return domain.SessionRecord{
		ID: domain.SessionID(id), ProjectID: "p1",
		Kind: domain.KindOrchestrator, Harness: domain.HarnessHermes,
		Activity: domain.Activity{State: domain.ActivityIdle, LastActivityAt: idleSince},
	}
}

func TestStallNudge_IdlePastThresholdGetsNudged(t *testing.T) {
	now := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	store := &stallNudgeStore{
		cards:    map[string]domain.WorkCard{"c1": stallCard("c1", "running", "hermes-1")},
		sessions: []domain.SessionRecord{idleCommander("hermes-1", now.Add(-11*time.Minute))},
	}
	sender := &stallNudgeSender{}
	n := NewStallNudger(StallNudgeDeps{Store: store, Sender: sender, Clock: func() time.Time { return now }, NewID: func() string { return "ev-1" }})

	nudged, err := n.ReconcileProject(context.Background(), "p1")
	if err != nil {
		t.Fatalf("ReconcileProject: %v", err)
	}
	if len(nudged) != 1 || nudged[0] != "c1" {
		t.Fatalf("nudged = %v, want [c1]", nudged)
	}
	if len(sender.sent) != 1 || sender.sent[0].session != "hermes-1" {
		t.Fatalf("sent = %+v, want one message to hermes-1", sender.sent)
	}
	if !strings.Contains(sender.sent[0].message, "c1") || !strings.Contains(sender.sent[0].message, "ao workboard get c1 --json") {
		t.Fatalf("message = %q, want card id and refresh command", sender.sent[0].message)
	}
	if len(store.appended) != 1 || store.appended[0].Kind != workCardEventStallNudged {
		t.Fatalf("events = %+v, want one stall_nudged event", store.appended)
	}
}

func TestStallNudge_ActivePhasesAllCovered(t *testing.T) {
	now := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	store := &stallNudgeStore{
		cards: map[string]domain.WorkCard{
			"r1": stallCard("r1", "running", "h1"),
			"r2": stallCard("r2", "review", "h1"),
			"r3": stallCard("r3", "testing", "h1"),
			"r4": stallCard("r4", "done", "h1"), // terminal — never nudged
			"r5": stallCard("r5", "todo", ""),   // not dispatched — never nudged
		},
		sessions: []domain.SessionRecord{idleCommander("h1", now.Add(-11*time.Minute))},
	}
	sender := &stallNudgeSender{}
	n := NewStallNudger(StallNudgeDeps{Store: store, Sender: sender, Clock: func() time.Time { return now }, NewID: func() string { return "ev" }})

	nudged, err := n.ReconcileProject(context.Background(), "p1")
	if err != nil {
		t.Fatalf("ReconcileProject: %v", err)
	}
	if len(nudged) != 3 {
		t.Fatalf("nudged = %v, want exactly r1 r2 r3", nudged)
	}
}

func TestStallNudge_NeverNudgesWaitingInputOrActive(t *testing.T) {
	now := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	waiting := idleCommander("h-wait", now.Add(-1*time.Hour))
	waiting.Activity.State = domain.ActivityWaitingInput
	active := idleCommander("h-active", now.Add(-1*time.Hour))
	active.Activity.State = domain.ActivityActive
	store := &stallNudgeStore{
		cards: map[string]domain.WorkCard{
			"cw": stallCard("cw", "running", "h-wait"),
			"ca": stallCard("ca", "running", "h-active"),
		},
		sessions: []domain.SessionRecord{waiting, active},
	}
	sender := &stallNudgeSender{}
	n := NewStallNudger(StallNudgeDeps{Store: store, Sender: sender, Clock: func() time.Time { return now }, NewID: func() string { return "ev" }})

	nudged, err := n.ReconcileProject(context.Background(), "p1")
	if err != nil {
		t.Fatalf("ReconcileProject: %v", err)
	}
	if len(nudged) != 0 || len(sender.sent) != 0 {
		t.Fatalf("nudged=%v sent=%v, want none — waiting_input is sticky, active is working", nudged, sender.sent)
	}
}

func TestStallNudge_IdleUnderThresholdNotNudged(t *testing.T) {
	now := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	store := &stallNudgeStore{
		cards:    map[string]domain.WorkCard{"c1": stallCard("c1", "running", "h1")},
		sessions: []domain.SessionRecord{idleCommander("h1", now.Add(-5*time.Minute))},
	}
	sender := &stallNudgeSender{}
	n := NewStallNudger(StallNudgeDeps{Store: store, Sender: sender, Clock: func() time.Time { return now }, NewID: func() string { return "ev" }})

	nudged, _ := n.ReconcileProject(context.Background(), "p1")
	if len(nudged) != 0 {
		t.Fatalf("nudged = %v, want none under 10-min threshold", nudged)
	}
}

func TestStallNudge_CooldownPreventsRepeatNudge(t *testing.T) {
	now := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	store := &stallNudgeStore{
		cards:    map[string]domain.WorkCard{"c1": stallCard("c1", "running", "h1")},
		sessions: []domain.SessionRecord{idleCommander("h1", now.Add(-1*time.Hour))},
		events: map[string][]domain.WorkCardEvent{
			"c1": {{CardID: "c1", Kind: workCardEventStallNudged, CreatedAt: now.Add(-5 * time.Minute)}},
		},
	}
	sender := &stallNudgeSender{}
	n := NewStallNudger(StallNudgeDeps{Store: store, Sender: sender, Clock: func() time.Time { return now }, NewID: func() string { return "ev" }})

	nudged, _ := n.ReconcileProject(context.Background(), "p1")
	if len(nudged) != 0 || len(sender.sent) != 0 {
		t.Fatalf("nudged=%v sent=%v, want none inside 15-min cooldown", nudged, sender.sent)
	}
}

func TestStallNudge_CooldownExpiredNudgesAgain(t *testing.T) {
	now := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	store := &stallNudgeStore{
		cards:    map[string]domain.WorkCard{"c1": stallCard("c1", "running", "h1")},
		sessions: []domain.SessionRecord{idleCommander("h1", now.Add(-1*time.Hour))},
		events: map[string][]domain.WorkCardEvent{
			"c1": {{CardID: "c1", Kind: workCardEventStallNudged, CreatedAt: now.Add(-20 * time.Minute)}},
		},
	}
	sender := &stallNudgeSender{}
	n := NewStallNudger(StallNudgeDeps{Store: store, Sender: sender, Clock: func() time.Time { return now }, NewID: func() string { return "ev" }})

	nudged, _ := n.ReconcileProject(context.Background(), "p1")
	if len(nudged) != 1 {
		t.Fatalf("nudged = %v, want repeat nudge after cooldown", nudged)
	}
}

func TestStallNudge_SkipsPausedRetargetAndMissingSession(t *testing.T) {
	now := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	paused := stallCard("cp", "running", "h1")
	paused.PausedRetarget = true
	store := &stallNudgeStore{
		cards: map[string]domain.WorkCard{
			"cp": paused,
			"cm": stallCard("cm", "running", "gone-session"),
		},
		sessions: []domain.SessionRecord{idleCommander("h1", now.Add(-1*time.Hour))},
	}
	sender := &stallNudgeSender{}
	n := NewStallNudger(StallNudgeDeps{Store: store, Sender: sender, Clock: func() time.Time { return now }, NewID: func() string { return "ev" }})

	nudged, err := n.ReconcileProject(context.Background(), "p1")
	if err != nil {
		t.Fatalf("ReconcileProject: %v", err)
	}
	if len(nudged) != 0 {
		t.Fatalf("nudged = %v, want none (paused skipped, missing session skipped)", nudged)
	}
}
```

Type note: `SessionRecord.Activity` is `domain.Activity` (`internal/domain/activity.go:25`, `session.go:56`) with fields `State`/`LastActivityAt` — verified against the codebase.

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd backend && go test ./internal/service/workboard/ -run TestStallNudge -v`
Expected: FAIL — `NewStallNudger`, `StallNudgeDeps`, `workCardEventStallNudged` undefined.

- [ ] **Step 3: Implement `stall_nudge.go`**

Create `backend/internal/service/workboard/stall_nudge.go`:

```go
package workboard

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/modernagent/modern-agent/backend/internal/domain"
)

const (
	workCardEventStallNudged = "stall_nudged"

	// ponytail: fixed thresholds; promote to WorkboardConfig knobs if real
	// projects need different pacing.
	stallNudgeIdleThreshold = 10 * time.Minute
	stallNudgeCooldown      = 15 * time.Minute
)

// StallNudgeStore is the narrow durable surface the nudger reads and audits
// through. *sqlite.Store satisfies it structurally.
type StallNudgeStore interface {
	ListWorkCards(ctx context.Context, projectID, boardID string) ([]domain.WorkCard, error)
	ListSessions(ctx context.Context, projectID domain.ProjectID) ([]domain.SessionRecord, error)
	ListWorkCardEvents(ctx context.Context, cardID string) ([]domain.WorkCardEvent, error)
	AppendWorkCardEvent(ctx context.Context, event domain.WorkCardEvent) error
}

// StallNudgeDeps configures a StallNudger.
type StallNudgeDeps struct {
	Store  StallNudgeStore
	Sender HermesSender
	Clock  func() time.Time
	NewID  func() string
}

// StallNudger watches cards in active phases (running/review/testing) whose
// linked commander session has gone idle past a threshold, and sends the
// session a reminder to re-read the card and advance or block it. It never
// kills or respawns anything; ActivityWaitingInput is sticky and never
// nudged, ActivityActive means the session is working and is left alone.
// This is the minimal safety net until the full orchestrator tick loop
// (hermes-director-orchestrator plan, Task 6) ships.
type StallNudger struct {
	store  StallNudgeStore
	sender HermesSender
	clock  func() time.Time
	newID  func() string
}

// NewStallNudger constructs the stall-nudge reconciler.
func NewStallNudger(d StallNudgeDeps) *StallNudger {
	clock := d.Clock
	if clock == nil {
		clock = time.Now
	}
	newID := d.NewID
	if newID == nil {
		newID = func() string { return "wce_" + uuid.NewString() }
	}
	return &StallNudger{store: d.Store, sender: d.Sender, clock: clock, newID: newID}
}

func stallNudgeActivePhase(status domain.CardStatus) bool {
	return status == domain.CardStatusRunning || status == domain.CardStatusReview || status == domain.CardStatusTesting
}

// ReconcileProject scans one project's active-phase cards and nudges idle
// commanders. It returns the card IDs that were nudged this pass.
func (n *StallNudger) ReconcileProject(ctx context.Context, projectID string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if n.store == nil || n.sender == nil {
		return nil, nil
	}
	cards, err := n.store.ListWorkCards(ctx, projectID, defaultBoardID)
	if err != nil {
		return nil, fmt.Errorf("list work cards for project %s: %w", projectID, err)
	}
	sessions, err := n.store.ListSessions(ctx, domain.ProjectID(projectID))
	if err != nil {
		return nil, fmt.Errorf("list sessions for project %s: %w", projectID, err)
	}
	byID := make(map[domain.SessionID]domain.SessionRecord, len(sessions))
	for _, s := range sessions {
		byID[s.ID] = s
	}

	now := n.clock().UTC()
	var nudged []string
	for _, card := range cards {
		if !stallNudgeActivePhase(card.Status) || card.SessionID == "" || card.PausedRetarget {
			continue
		}
		session, ok := byID[domain.SessionID(card.SessionID)]
		if !ok || session.IsTerminated {
			// Orphan (no live session) is a respawn problem, not a nudge
			// problem — out of scope here, the orchestrator plan's recovery
			// task owns it.
			continue
		}
		if session.Activity.State != domain.ActivityIdle {
			// WaitingInput is sticky (human needed); Active means working.
			continue
		}
		if session.Activity.LastActivityAt.IsZero() || now.Sub(session.Activity.LastActivityAt) < stallNudgeIdleThreshold {
			continue
		}
		events, err := n.store.ListWorkCardEvents(ctx, card.ID)
		if err != nil {
			return nudged, fmt.Errorf("list events for card %s: %w", card.ID, err)
		}
		if withinStallNudgeCooldown(events, now) {
			continue
		}
		message := stallNudgeMessage(card)
		if err := n.sender.Send(ctx, session.ID, message, ""); err != nil {
			return nudged, fmt.Errorf("nudge stalled card %s: %w", card.ID, err)
		}
		if err := n.store.AppendWorkCardEvent(ctx, domain.WorkCardEvent{
			ID: n.newID(), CardID: card.ID, ProjectID: card.ProjectID,
			Kind: workCardEventStallNudged, CreatedAt: now,
		}); err != nil {
			return nudged, fmt.Errorf("record stall nudge for card %s: %w", card.ID, err)
		}
		nudged = append(nudged, card.ID)
	}
	return nudged, nil
}

func withinStallNudgeCooldown(events []domain.WorkCardEvent, now time.Time) bool {
	for _, ev := range events {
		if ev.Kind == workCardEventStallNudged && now.Sub(ev.CreatedAt) < stallNudgeCooldown {
			return true
		}
	}
	return false
}

func stallNudgeMessage(card domain.WorkCard) string {
	return fmt.Sprintf(
		"Stall check: work card %s (%q) is still in %s and this session has been idle. "+
			"Run `ao workboard get %s --json` now, then either advance the card with "+
			"`ao workboard status %s <next-status>` after finishing the phase, or move it "+
			"to blocked with the exact reason if you cannot proceed.",
		card.ID, card.Title, card.Status, card.ID, card.ID)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && go test ./internal/service/workboard/ -run TestStallNudge -v`
Expected: PASS, all 7 tests.

- [ ] **Step 5: Full package + build check**

Run: `cd backend && go build ./... && go vet ./... && go test ./internal/service/workboard/...`
Expected: all green — no existing test in the package touches the new event kind.

- [ ] **Step 6: Commit the reconciler**

```bash
git add backend/internal/service/workboard/stall_nudge.go backend/internal/service/workboard/stall_nudge_test.go
git commit -m "feat(workboard): stall-nudge reconciler for idle commanders

Cards in running/review/testing whose Hermes commander goes idle past 10
minutes now get an automatic reminder message to re-read the card and
advance or block it, with a 15-minute per-card cooldown. Minimal safety
net until the full orchestrator tick loop (Task 6) ships; waiting_input
sessions are sticky and never nudged, active sessions are left alone."
```

- [ ] **Step 7: Wire into the daemon**

Modify `backend/internal/daemon/workboard_wiring.go` — add the nudger next to the answerer/switcher (inside `startWorkboardDispatcher`):

```go
	nudger := workboardsvc.NewStallNudger(workboardsvc.StallNudgeDeps{Store: store, Sender: sessions})
```

and start + join it exactly like the others:

```go
	answererDone := startProjectReconciler(ctx, store, answerer, logger, "workboard answerer")
	switcherDone := startProjectReconciler(ctx, store, switcher, logger, "workboard switcher")
	nudgerDone := startProjectReconciler(ctx, store, nudger, logger, "workboard stall nudger")

	allDone := make(chan struct{})
	go func() {
		defer close(allDone)
		<-trigger.Done()
		<-answererDone
		<-switcherDone
		<-nudgerDone
	}()
```

- [ ] **Step 8: Verify daemon wiring compiles and daemon tests pass**

Run: `cd backend && go build ./... && go test ./internal/daemon/...`
Expected: all green. (`*sqlite.Store` and `*sessionsvc.Service` satisfy `StallNudgeStore`/`HermesSender` structurally — if the compiler disagrees, the interface method sets in Step 3 must be adjusted to match the store's actual signatures, not the other way around.)

- [ ] **Step 9: Commit the wiring**

```bash
git add backend/internal/daemon/workboard_wiring.go
git commit -m "feat(daemon): run workboard stall nudger alongside answerer/switcher"
```

---

## Out of scope

- Orphan recovery (card whose session is terminated/missing) — needs respawn-with-briefing, owned by the orchestrator plan's Task 6/17. The nudger explicitly skips these.
- Killing or replacing sessions — never.
- Config knobs for thresholds — constants first; promote to `WorkboardConfig` only if real usage demands it.
- Frontend display of `stall_nudged` events — the event lands in the existing card-event history; dedicated UI treatment can ride a later card-detail task.
- Auto-advancing the card status from the daemon — the commander stays the decision-maker; the daemon only reminds.

## Verification plan

```bash
cd backend
go test ./internal/service/workboard/ -run TestStallNudge -v
go build ./... && go vet ./...
go test ./internal/service/workboard/... ./internal/daemon/...
```

Manual end-to-end (optional, isolated daemon per the earlier full-test recipe):
1. Start isolated daemon (`AO_DATA_DIR`/`AO_PORT` in scratchpad) + frontend.
2. Create a card in Director → Hermes commander spawns, card in Running.
3. Kill the Hermes process's work (leave the session record idle) or wait for it to go idle.
4. Within ~11 min the daemon log shows the nudge send; the card's event history contains `stall_nudged`; the commander session receives the reminder message.
5. Confirm no second nudge within the next 15 min (cooldown), then a repeat after.
