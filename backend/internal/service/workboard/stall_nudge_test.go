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

func idleNonCommander(id string, idleSince time.Time, kind domain.SessionKind, harness domain.AgentHarness) domain.SessionRecord {
	return domain.SessionRecord{
		ID: domain.SessionID(id), ProjectID: "p1",
		Kind: kind, Harness: harness,
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

func TestStallNudge_ExcludesNonCommanderSessions(t *testing.T) {
	now := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	store := &stallNudgeStore{
		cards: map[string]domain.WorkCard{
			"c-worker":      stallCard("c-worker", "running", "worker-1"),
			"c-bad-harness": stallCard("c-bad-harness", "running", "orch-1"),
			"c-commander":   stallCard("c-commander", "running", "hermes-1"),
		},
		sessions: []domain.SessionRecord{
			// Plain worker session, idle past threshold
			idleNonCommander("worker-1", now.Add(-11*time.Minute), domain.KindWorker, domain.HarnessAutohand),
			// Orchestrator but not Hermes, idle past threshold
			idleNonCommander("orch-1", now.Add(-11*time.Minute), domain.KindOrchestrator, domain.HarnessAutohand),
			// Hermes commander, idle past threshold (should be nudged)
			idleCommander("hermes-1", now.Add(-11*time.Minute)),
		},
	}
	sender := &stallNudgeSender{}
	n := NewStallNudger(StallNudgeDeps{Store: store, Sender: sender, Clock: func() time.Time { return now }, NewID: func() string { return "ev" }})

	nudged, err := n.ReconcileProject(context.Background(), "p1")
	if err != nil {
		t.Fatalf("ReconcileProject: %v", err)
	}
	// Only the Hermes commander should be nudged
	if len(nudged) != 1 || nudged[0] != "c-commander" {
		t.Fatalf("nudged = %v, want [c-commander] only", nudged)
	}
	if len(sender.sent) != 1 || sender.sent[0].session != "hermes-1" {
		t.Fatalf("sent = %+v, want one message to hermes-1", sender.sent)
	}
	if len(store.appended) != 1 || store.appended[0].Kind != workCardEventStallNudged {
		t.Fatalf("events = %+v, want one stall_nudged event", store.appended)
	}
}
