package workboard

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/modernagent/modern-agent/backend/internal/domain"
	"github.com/modernagent/modern-agent/backend/internal/ports"
)

var (
	errSpawnFailed = errors.New("spawn failed")
	errKillFailed  = errors.New("kill failed")
)

func TestNudgeRunningCard_SendsAndRecordsEvent(t *testing.T) {
	now := time.Date(2026, time.July, 17, 12, 0, 0, 0, time.UTC)
	card := domain.WorkCard{
		ID: "card-1", ProjectID: "p1", BoardID: defaultBoardID, Title: "Fix", Notes: "Details",
		Status: domain.CardStatusRunning, Agent: "codex", SessionID: "sess-1", TargetPath: "/repo/app",
	}
	store := &actionsStoreFake{cards: map[string]domain.WorkCard{"card-1": card}}
	sender := &actionsSenderFake{}
	svc := NewWithDeps(Deps{
		Store: store, Sender: sender, Clock: func() time.Time { return now }, NewID: func() string { return "evt-1" },
	})

	got, err := svc.Nudge(context.Background(), "card-1", NudgeInput{Message: "Please continue"})
	if err != nil {
		t.Fatalf("Nudge: %v", err)
	}
	if sender.lastTarget != "sess-1" || sender.lastMessage != "Please continue" {
		t.Fatalf("send = target %q message %q", sender.lastTarget, sender.lastMessage)
	}
	if len(store.events) != 1 || store.events[0].Kind != workCardEventNudged {
		t.Fatalf("events = %+v", store.events)
	}
	if !got.UpdatedAt.Equal(now) {
		t.Fatalf("updatedAt = %v", got.UpdatedAt)
	}
}

func TestRetargetRunningCard_HandsOffThroughHermes(t *testing.T) {
	now := time.Date(2026, time.July, 17, 12, 0, 0, 0, time.UTC)
	card := domain.WorkCard{
		ID: "card-1", ProjectID: "p1", BoardID: defaultBoardID, Title: "Old", Notes: "Old notes",
		Status: domain.CardStatusRunning, Agent: "codex", SessionID: "worker-1", TargetPath: "/repo/app", GoalVersion: 1,
	}
	store := &actionsStoreFake{
		cards: map[string]domain.WorkCard{"card-1": card},
		sessions: []domain.SessionRecord{
			{ID: "worker-1", ProjectID: "p1", Kind: domain.KindWorker, Harness: domain.HarnessCodex},
			{ID: "hermes-1", ProjectID: "p1", Kind: domain.KindOrchestrator, Harness: domain.HarnessHermes, UpdatedAt: now},
		},
	}
	sender := &actionsSenderFake{}
	svc := NewWithDeps(Deps{
		Store: store, Sender: sender, Spawner: &actionsSpawnerFake{}, Killer: &actionsKillerFake{},
		Clock: func() time.Time { return now }, NewID: func() string { return "evt-1" },
	})
	title := "New title"
	got, err := svc.Retarget(context.Background(), "card-1", RetargetInput{Title: &title})
	if err != nil {
		t.Fatalf("Retarget: %v", err)
	}
	if sender.lastTarget != "hermes-1" || !strings.Contains(sender.lastMessage, "AO retarget") {
		t.Fatalf("handoff = target %q message %q", sender.lastTarget, sender.lastMessage)
	}
	if got.Title != "New title" || got.GoalVersion != 2 || got.PausedRetarget {
		t.Fatalf("card after retarget = %#v", got)
	}
	if len(store.events) != 1 || store.events[0].Kind != workCardEventRetargeted {
		t.Fatalf("events = %+v", store.events)
	}
}

func TestRetargetHermesLinkedCardDoesNotSpawnOrReplaceCommander(t *testing.T) {
	now := time.Date(2026, time.July, 17, 12, 0, 0, 0, time.UTC)
	card := domain.WorkCard{ID: "card-1", ProjectID: "p1", BoardID: defaultBoardID, Title: "Old", Notes: "Old notes", Status: domain.CardStatusRunning, Agent: "claude-code", SessionID: "hermes-1", GoalVersion: 1}
	store := &actionsStoreFake{cards: map[string]domain.WorkCard{"card-1": card}, sessions: []domain.SessionRecord{{ID: "hermes-1", ProjectID: "p1", Kind: domain.KindOrchestrator, Harness: domain.HarnessHermes}}}
	sender := &actionsSenderFake{}
	svc := NewWithDeps(Deps{Store: store, Sender: sender, Clock: func() time.Time { return now }, NewID: func() string { return "evt-1" }})
	notes := "Narrow the scope"

	got, err := svc.Retarget(context.Background(), card.ID, RetargetInput{Notes: &notes})
	if err != nil {
		t.Fatalf("Retarget: %v", err)
	}
	if sender.lastTarget != "hermes-1" || got.SessionID != "hermes-1" || got.PausedRetarget {
		t.Fatalf("retarget = %#v send=%q", got, sender.lastTarget)
	}
}

func TestRetargetRunningCard_SpawnFailureClearsPause(t *testing.T) {
	now := time.Date(2026, time.July, 17, 12, 0, 0, 0, time.UTC)
	card := domain.WorkCard{
		ID: "card-1", ProjectID: "p1", BoardID: defaultBoardID, Title: "Old", Notes: "Old notes",
		Status: domain.CardStatusRunning, Agent: "codex", SessionID: "worker-1", TargetPath: "/repo/app", GoalVersion: 1,
	}
	store := &actionsStoreFake{cards: map[string]domain.WorkCard{"card-1": card}}
	svc := NewWithDeps(Deps{
		Store: store, Sender: &actionsSenderFake{}, Spawner: &actionsSpawnerFake{err: errSpawnFailed},
		Killer: &actionsKillerFake{}, Clock: func() time.Time { return now },
	})
	title := "New title"
	_, err := svc.Retarget(context.Background(), "card-1", RetargetInput{Title: &title})
	if err == nil {
		t.Fatal("expected retarget spawn failure")
	}
	persisted, ok := store.cards["card-1"]
	if !ok {
		t.Fatal("card missing from store")
	}
	if persisted.PausedRetarget {
		t.Fatalf("pausedRetarget should be cleared after spawn failure, got %#v", persisted)
	}
	if persisted.GoalVersion != 1 || persisted.Title != "Old" {
		t.Fatalf("card should revert to pre-retarget state, got %#v", persisted)
	}
	if persisted.SessionID != "worker-1" {
		t.Fatalf("sessionId = %q", persisted.SessionID)
	}
}

func TestSplitRunningCard_KillFailureBlocksSuccessor(t *testing.T) {
	now := time.Date(2026, time.July, 17, 12, 0, 0, 0, time.UTC)
	card := domain.WorkCard{
		ID: "card-1", ProjectID: "p1", BoardID: defaultBoardID, Title: "Old", Notes: "Old notes",
		Status: domain.CardStatusRunning, Agent: "codex", SessionID: "worker-1", TargetPath: "/repo/app",
	}
	store := &actionsStoreFake{cards: map[string]domain.WorkCard{"card-1": card}}
	svc := NewWithDeps(Deps{
		Store: store, Killer: &actionsKillerFake{err: errKillFailed}, Clock: func() time.Time { return now },
		NewID: func() string { return "card-2" },
	})

	_, err := svc.Split(context.Background(), "card-1", SplitInput{
		Title: "New branch", Notes: "Follow-up work", StartImmediately: true,
	})
	if err == nil {
		t.Fatal("expected split kill failure")
	}
	oldCard := store.cards["card-1"]
	if oldCard.Status != domain.CardStatusRunning || oldCard.SessionID != "worker-1" {
		t.Fatalf("old card should remain running with linked session, got %#v", oldCard)
	}
	successor, ok := store.cards["card-2"]
	if !ok {
		t.Fatal("successor card should exist for compensation")
	}
	if successor.Status != domain.CardStatusBlocked {
		t.Fatalf("successor should be blocked after kill failure, got status %q", successor.Status)
	}
	if successor.ReadyAt != nil {
		t.Fatalf("successor readyAt should be cleared, got %v", successor.ReadyAt)
	}
}

func TestSplitRunningCard_CreatesSuccessorAndArchivesOld(t *testing.T) {
	now := time.Date(2026, time.July, 17, 12, 0, 0, 0, time.UTC)
	card := domain.WorkCard{
		ID: "card-1", ProjectID: "p1", BoardID: defaultBoardID, Title: "Old", Notes: "Old notes",
		Priority: domain.CardPriorityHigh, Labels: []string{"bug"}, Status: domain.CardStatusRunning,
		Agent: "codex", SessionID: "worker-1", TargetPath: "/repo/app",
	}
	store := &actionsStoreFake{cards: map[string]domain.WorkCard{"card-1": card}}
	killer := &actionsKillerFake{}
	svc := NewWithDeps(Deps{
		Store: store, Killer: killer, Clock: func() time.Time { return now },
		NewID: func() string {
			if len(store.created) == 0 {
				return "card-2"
			}
			return "evt-1"
		},
	})

	result, err := svc.Split(context.Background(), "card-1", SplitInput{
		Title: "New branch", Notes: "Follow-up work", StartImmediately: true,
	})
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	if killer.killed != "worker-1" {
		t.Fatalf("killed = %q", killer.killed)
	}
	if result.OldCard.Status != domain.CardStatusTodo || result.OldCard.SupersededByCardID != "card-2" || result.OldCard.SessionID != "" {
		t.Fatalf("old card = %#v", result.OldCard)
	}
	if result.NewCard.Status != domain.CardStatusReady || result.NewCard.Agent != "codex" || result.NewCard.TargetPath != "/repo/app" {
		t.Fatalf("new card = %#v", result.NewCard)
	}
	if len(store.events) != 1 || store.events[0].Kind != workCardEventSplit {
		t.Fatalf("events = %+v", store.events)
	}
}

func TestSplitHermesLinkedCardNotifiesCommanderWithoutKillingIt(t *testing.T) {
	now := time.Date(2026, time.July, 17, 12, 0, 0, 0, time.UTC)
	card := domain.WorkCard{ID: "card-1", ProjectID: "p1", BoardID: defaultBoardID, Title: "Old", Notes: "Old notes", Status: domain.CardStatusRunning, Agent: "claude-code", SessionID: "hermes-1"}
	store := &actionsStoreFake{cards: map[string]domain.WorkCard{"card-1": card}, sessions: []domain.SessionRecord{{ID: "hermes-1", ProjectID: "p1", Kind: domain.KindOrchestrator, Harness: domain.HarnessHermes}}}
	sender := &actionsSenderFake{}
	svc := NewWithDeps(Deps{Store: store, Sender: sender, Clock: func() time.Time { return now }, NewID: func() string {
		if len(store.created) == 0 {
			return "card-2"
		}
		return "evt-1"
	}})

	result, err := svc.Split(context.Background(), card.ID, SplitInput{Title: "Next", Notes: "Next work"})
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	if result.OldCard.SessionID != "" || sender.lastTarget != "hermes-1" || !strings.Contains(sender.lastMessage, "card-2") {
		t.Fatalf("split = %#v send=%q %q", result, sender.lastTarget, sender.lastMessage)
	}
}

type actionsStoreFake struct {
	cards                 map[string]domain.WorkCard
	sessions              []domain.SessionRecord
	events                []domain.WorkCardEvent
	created               []domain.WorkCard
	deletedActiveSessions []string
}

func (f *actionsStoreFake) CreateWorkCard(_ context.Context, card domain.WorkCard) error {
	f.created = append(f.created, card)
	f.cards[card.ID] = card
	return nil
}

func (f *actionsStoreFake) GetWorkCard(_ context.Context, id string) (domain.WorkCard, bool, error) {
	card, ok := f.cards[id]
	return card, ok, nil
}

func (f *actionsStoreFake) UpdateWorkCard(_ context.Context, card domain.WorkCard) error {
	f.cards[card.ID] = card
	return nil
}

func (f *actionsStoreFake) ListWorkCards(context.Context, string, string) ([]domain.WorkCard, error) {
	return nil, nil
}

func (f *actionsStoreFake) ListAllWorkCards(context.Context) ([]domain.WorkCard, error) {
	return nil, nil
}

func (f *actionsStoreFake) InsertRedoCycle(context.Context, domain.RedoCycle) error {
	return nil
}

func (f *actionsStoreFake) GetLatestRedoCycle(context.Context, string) (domain.RedoCycle, bool, error) {
	return domain.RedoCycle{}, false, nil
}

func (f *actionsStoreFake) ListRedoCycles(context.Context, string) ([]domain.RedoCycle, error) {
	return nil, nil
}

func (f *actionsStoreFake) CompleteRedoCycle(context.Context, string, time.Time) error {
	return nil
}

func (f *actionsStoreFake) InsertRedoFinding(context.Context, domain.RedoFinding) error {
	return nil
}

func (f *actionsStoreFake) ListRedoFindings(context.Context, string) ([]domain.RedoFinding, error) {
	return nil, nil
}

func (f *actionsStoreFake) UpdateRedoFindingStatus(context.Context, string, domain.FindingStatus, int, time.Time) error {
	return nil
}

func (f *actionsStoreFake) InsertRedoAttempt(context.Context, domain.RedoAttempt) error {
	return nil
}

func (f *actionsStoreFake) ListRedoAttempts(context.Context, string) ([]domain.RedoAttempt, error) {
	return nil, nil
}

func (f *actionsStoreFake) GetProject(context.Context, string) (domain.ProjectRecord, bool, error) {
	return domain.ProjectRecord{}, false, nil
}

func (f *actionsStoreFake) ListWorkspaceRepos(context.Context, string) ([]domain.WorkspaceRepoRecord, error) {
	return nil, nil
}

func (f *actionsStoreFake) ListSessions(_ context.Context, _ domain.ProjectID) ([]domain.SessionRecord, error) {
	return f.sessions, nil
}

func (f *actionsStoreFake) AppendWorkCardEvent(_ context.Context, event domain.WorkCardEvent) error {
	f.events = append(f.events, event)
	return nil
}

func (f *actionsStoreFake) ListWorkCardEvents(_ context.Context, cardID string) ([]domain.WorkCardEvent, error) {
	var events []domain.WorkCardEvent
	for _, event := range f.events {
		if event.CardID == cardID {
			events = append(events, event)
		}
	}
	return events, nil
}

func (f *actionsStoreFake) DeleteActiveSession(_ context.Context, cardID string) error {
	f.deletedActiveSessions = append(f.deletedActiveSessions, cardID)
	return nil
}

type actionsSenderFake struct {
	lastTarget  domain.SessionID
	lastMessage string
}

func (f *actionsSenderFake) Send(_ context.Context, id domain.SessionID, message string, _ domain.SessionID) error {
	f.lastTarget, f.lastMessage = id, message
	return nil
}

type actionsSpawnerFake struct {
	last ports.SpawnConfig
	err  error
}

func (f *actionsSpawnerFake) Spawn(_ context.Context, cfg ports.SpawnConfig) (domain.Session, error) {
	f.last = cfg
	if f.err != nil {
		return domain.Session{}, f.err
	}
	return domain.Session{SessionRecord: domain.SessionRecord{ID: "worker-new"}}, nil
}

type actionsKillerFake struct {
	killed domain.SessionID
	err    error
}

func (f *actionsKillerFake) Kill(_ context.Context, id domain.SessionID) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	f.killed = id
	return true, nil
}
