package workboard

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modernagent/modern-agent/backend/internal/domain"
	"github.com/modernagent/modern-agent/backend/internal/ports"
	sessionsvc "github.com/modernagent/modern-agent/backend/internal/service/session"
	"github.com/modernagent/modern-agent/backend/internal/storage/sqlite"
)

func TestDispatchOnce(t *testing.T) {
	now := time.Date(2026, time.July, 17, 9, 0, 0, 0, time.UTC)

	tests := []struct {
		name        string
		wipLimit    int
		cards       []domain.WorkCard
		spawnErr    error
		wantClaimed []string
		wantStatus  map[string]domain.CardStatus
		wantSpawns  []string
	}{
		{
			name:     "orders priority then ready fifo under WIP cap",
			wipLimit: 2,
			cards: []domain.WorkCard{
				readyCard("normal-old", domain.CardPriorityNormal, now.Add(-3*time.Hour)),
				readyCard("high-new", domain.CardPriorityHigh, now.Add(-time.Hour)),
				readyCard("urgent", domain.CardPriorityUrgent, now.Add(-30*time.Minute)),
				readyCard("high-old", domain.CardPriorityHigh, now.Add(-2*time.Hour)),
			},
			wantClaimed: []string{"urgent", "high-old"},
			wantSpawns:  []string{"urgent", "high-old"},
		},
		{
			name:     "running cards consume project WIP",
			wipLimit: 1,
			cards: []domain.WorkCard{
				{ID: "already-running", ProjectID: "p1", BoardID: defaultBoardID, Status: domain.CardStatusRunning},
				readyCard("ready", domain.CardPriorityUrgent, now.Add(-time.Hour)),
			},
			wantClaimed: nil,
			wantSpawns:  nil,
		},
		{
			name:     "paused retarget card is not claimed",
			wipLimit: 2,
			cards: []domain.WorkCard{
				func() domain.WorkCard {
					c := readyCard("paused", domain.CardPriorityUrgent, now.Add(-time.Hour))
					c.PausedRetarget = true
					return c
				}(),
				readyCard("available", domain.CardPriorityLow, now.Add(-2*time.Hour)),
			},
			wantClaimed: []string{"available"},
			wantSpawns:  []string{"available"},
		},
		{
			name:     "todo card is promoted and claimed when WIP has room",
			wipLimit: 1,
			cards: []domain.WorkCard{
				func() domain.WorkCard {
					c := readyCard("todo", domain.CardPriorityNormal, now)
					c.Status = domain.CardStatusTodo
					c.ReadyAt = nil
					return c
				}(),
			},
			wantClaimed: []string{"todo"},
			wantStatus:  map[string]domain.CardStatus{"todo": domain.CardStatusRunning},
			wantSpawns:  []string{"todo"},
		},
		{
			name:     "due scheduled card is promoted even while WIP is full",
			wipLimit: 1,
			cards: []domain.WorkCard{
				{ID: "already-running", ProjectID: "p1", BoardID: defaultBoardID, Status: domain.CardStatusRunning},
				{ID: "scheduled", ProjectID: "p1", BoardID: defaultBoardID, Status: domain.CardStatusScheduled, ScheduledAt: ptrTime(now.Add(-time.Minute))},
			},
			wantClaimed: nil,
			wantStatus:  map[string]domain.CardStatus{"scheduled": domain.CardStatusReady},
			wantSpawns:  nil,
		},
		{
			name:     "due scheduled card is promoted and claimed when WIP has room",
			wipLimit: 1,
			cards: []domain.WorkCard{
				func() domain.WorkCard {
					c := readyCard("scheduled", domain.CardPriorityNormal, now)
					c.Status = domain.CardStatusScheduled
					c.ReadyAt = nil
					c.ScheduledAt = ptrTime(now.Add(-time.Minute))
					return c
				}(),
			},
			wantClaimed: []string{"scheduled"},
			wantStatus:  map[string]domain.CardStatus{"scheduled": domain.CardStatusRunning},
			wantSpawns:  []string{"scheduled"},
		},
		{
			name:     "future scheduled card stays scheduled",
			wipLimit: 1,
			cards: []domain.WorkCard{
				{ID: "scheduled", ProjectID: "p1", BoardID: defaultBoardID, Status: domain.CardStatusScheduled, ScheduledAt: ptrTime(now.Add(time.Hour))},
			},
			wantClaimed: nil,
			wantStatus:  map[string]domain.CardStatus{"scheduled": domain.CardStatusScheduled},
			wantSpawns:  nil,
		},
		{
			name:     "spawn failure leaves card ready and unlinked",
			wipLimit: 1,
			cards: []domain.WorkCard{
				readyCard("ready", domain.CardPriorityUrgent, now.Add(-time.Hour)),
			},
			spawnErr:    errors.New("runtime unavailable"),
			wantClaimed: nil,
			wantStatus:  map[string]domain.CardStatus{"ready": domain.CardStatusReady},
			wantSpawns:  []string{"ready"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := newDispatchStore(tc.wipLimit, tc.cards)
			spawner := &dispatchSpawner{err: tc.spawnErr}
			dispatcher := NewDispatcher(DispatchDeps{Store: store, Spawner: spawner, Clock: func() time.Time { return now }})

			claimed, err := dispatcher.DispatchOnce(context.Background(), "p1")
			if err != nil {
				t.Fatalf("DispatchOnce: %v", err)
			}
			if !reflect.DeepEqual(claimed, tc.wantClaimed) {
				t.Fatalf("claimed = %v, want %v", claimed, tc.wantClaimed)
			}
			if got := spawner.cardIDs(); !reflect.DeepEqual(got, tc.wantSpawns) {
				t.Fatalf("spawned cards = %v, want %v", got, tc.wantSpawns)
			}
			for id, want := range tc.wantStatus {
				if got := store.cards[id].Status; got != want {
					t.Fatalf("card %s status = %q, want %q", id, got, want)
				}
			}
			for _, id := range claimed {
				card := store.cards[id]
				if card.Status != domain.CardStatusRunning || card.SessionID == "" {
					t.Fatalf("claimed card %s = %#v, want running and linked", id, card)
				}
			}
			if tc.spawnErr != nil {
				card := store.cards["ready"]
				if card.SessionID != "" {
					t.Fatalf("failed card session_id = %q, want empty", card.SessionID)
				}
				if card.ReadyAt == nil || !card.ReadyAt.Equal(now.Add(-time.Hour)) {
					t.Fatalf("failed card ready_at = %v, want original %v", card.ReadyAt, now.Add(-time.Hour))
				}
				events := store.events["ready"]
				if len(events) != 1 || events[0].Kind != workCardEventDispatchFailed || !strings.Contains(events[0].Payload, dispatchFailedReasonSpawnFailed) {
					t.Fatalf("dispatch events = %+v, want one spawn_failed event", events)
				}
			}
		})
	}
}

func TestDispatchOnceSpawnsWorkerWithCardHarnessAndPrompt(t *testing.T) {
	now := time.Date(2026, time.July, 17, 9, 0, 0, 0, time.UTC)
	card := readyCard("card", domain.CardPriorityNormal, now)
	card.TargetPath = "/repo/services/api"
	store := newDispatchStore(1, []domain.WorkCard{card})
	spawner := &dispatchSpawner{}
	dispatcher := NewDispatcher(DispatchDeps{Store: store, Spawner: spawner, Clock: func() time.Time { return now }})

	if _, err := dispatcher.DispatchOnce(context.Background(), "p1"); err != nil {
		t.Fatalf("DispatchOnce: %v", err)
	}
	if len(spawner.configs) != 1 {
		t.Fatalf("spawn count = %d, want 1", len(spawner.configs))
	}
	got := spawner.configs[0]
	if got.ProjectID != "p1" || got.Kind != domain.KindWorker || got.Harness != domain.HarnessCodex || got.Prompt != "card title\n\ncard notes" || got.TargetPath != "/repo/services/api" {
		t.Fatalf("spawn config = %#v", got)
	}
}

func TestDispatchOnce_SpawnFailureDoesNotBlockLaterCards(t *testing.T) {
	now := time.Date(2026, time.July, 17, 9, 0, 0, 0, time.UTC)
	store := newDispatchStore(2, []domain.WorkCard{
		readyCard("bad", domain.CardPriorityUrgent, now.Add(-time.Minute)),
		readyCard("good", domain.CardPriorityNormal, now.Add(-2*time.Minute)),
	})
	spawner := &dispatchSpawner{failCardIDs: map[string]error{"bad": errors.New("agent unavailable")}}
	dispatcher := NewDispatcher(DispatchDeps{Store: store, Spawner: spawner, Clock: func() time.Time { return now }})

	claimed, err := dispatcher.DispatchOnce(context.Background(), "p1")
	if err != nil {
		t.Fatalf("DispatchOnce: %v", err)
	}
	if !reflect.DeepEqual(claimed, []string{"good"}) {
		t.Fatalf("claimed = %v, want good card only", claimed)
	}
	if card := store.cards["bad"]; card.Status != domain.CardStatusReady || card.SessionID != "" {
		t.Fatalf("bad card = %#v, want released ready card", card)
	}
	if events := store.events["bad"]; len(events) != 1 || !strings.Contains(events[0].Payload, dispatchFailedReasonSpawnFailed) {
		t.Fatalf("bad dispatch events = %+v, want one spawn failure", events)
	}
	if card := store.cards["good"]; card.Status != domain.CardStatusRunning || card.SessionID == "" {
		t.Fatalf("good card = %#v, want running linked card", card)
	}
}

func TestDispatchOnce_HermesProjectBriefsCommanderInsteadOfSpawningWorker(t *testing.T) {
	now := time.Date(2026, time.July, 17, 9, 0, 0, 0, time.UTC)
	card := readyCard("card", domain.CardPriorityHigh, now)
	card.CodingAgent = "claude-code"
	card.Labels = []string{"billing"}
	store := newDispatchStore(1, []domain.WorkCard{card})
	store.project.Config.Orchestrator.Harness = domain.HarnessHermes
	spawner := &dispatchSpawner{orchestratorSession: domain.Session{SessionRecord: domain.SessionRecord{
		ID: "hermes-1", ProjectID: "p1", Kind: domain.KindOrchestrator, Harness: domain.HarnessHermes,
	}}}
	dispatcher := NewDispatcher(DispatchDeps{Store: store, Spawner: spawner, Clock: func() time.Time { return now }})

	claimed, err := dispatcher.DispatchOnce(context.Background(), "p1")
	if err != nil {
		t.Fatalf("DispatchOnce: %v", err)
	}
	if !reflect.DeepEqual(claimed, []string{"card"}) || len(spawner.configs) != 0 {
		t.Fatalf("claimed=%v worker spawns=%v", claimed, spawner.configs)
	}
	if got := store.cards["card"].SessionID; got != "hermes-1" {
		t.Fatalf("card session = %q, want Hermes", got)
	}
	if len(spawner.orchestratorPrompts) != 1 || !strings.Contains(spawner.orchestratorPrompts[0], `"cardId":"card"`) || !strings.Contains(spawner.orchestratorPrompts[0], `"codingAgent":"claude-code"`) || !strings.Contains(spawner.orchestratorPrompts[0], "Do not spawn a separate reviewer or testing session") || !strings.Contains(spawner.orchestratorPrompts[0], "even if this card's older reviewer/testing fields name another agent") {
		t.Fatalf("briefing = %q", spawner.orchestratorPrompts)
	}
}

func TestDispatchOnce_HermesUnavailableReleasesCardWithoutWorker(t *testing.T) {
	now := time.Date(2026, time.July, 17, 9, 0, 0, 0, time.UTC)
	store := newDispatchStore(1, []domain.WorkCard{readyCard("card", domain.CardPriorityNormal, now)})
	store.project.Config.Orchestrator.Harness = domain.HarnessHermes
	spawner := &dispatchSpawner{orchestratorErr: ErrHermesUnavailable}
	dispatcher := NewDispatcher(DispatchDeps{Store: store, Spawner: spawner, Clock: func() time.Time { return now }})

	claimed, err := dispatcher.DispatchOnce(context.Background(), "p1")
	if err != nil || len(claimed) != 0 || len(spawner.configs) != 0 {
		t.Fatalf("claimed=%v err=%v worker spawns=%v", claimed, err, spawner.configs)
	}
	if card := store.cards["card"]; card.Status != domain.CardStatusTodo || card.SessionID != "" {
		t.Fatalf("card = %#v, want Todo and unlinked", card)
	}
	events := store.events["card"]
	if len(events) != 1 || !strings.Contains(events[0].Payload, dispatchFailedReasonHermesUnavailable) {
		t.Fatalf("dispatch events = %+v, want Hermes unavailable event", events)
	}
}

func TestDispatchOnce_HermesProjectBlockedByNonHermesOrchestratorReturnsCardToTodo(t *testing.T) {
	now := time.Date(2026, time.July, 17, 9, 0, 0, 0, time.UTC)
	store := newDispatchStore(1, []domain.WorkCard{readyCard("card", domain.CardPriorityNormal, now)})
	store.project.Config.Orchestrator.Harness = domain.HarnessHermes
	store.sessions = []domain.SessionRecord{{ID: "old-1", ProjectID: "p1", Kind: domain.KindOrchestrator, Harness: domain.HarnessClaudeCode}}
	spawner := &dispatchSpawner{}
	dispatcher := NewDispatcher(DispatchDeps{Store: store, Spawner: spawner, Clock: func() time.Time { return now }})

	claimed, err := dispatcher.DispatchOnce(context.Background(), "p1")
	if err != nil || len(claimed) != 0 || len(spawner.orchestratorPrompts) != 0 || len(spawner.configs) != 0 {
		t.Fatalf("claimed=%v err=%v prompts=%q worker spawns=%v", claimed, err, spawner.orchestratorPrompts, spawner.configs)
	}
	if card := store.cards["card"]; card.Status != domain.CardStatusTodo || card.SessionID != "" {
		t.Fatalf("card = %#v, want todo and unlinked", card)
	}
	events := store.events["card"]
	if len(events) != 1 || !strings.Contains(events[0].Payload, dispatchFailedReasonNonHermesOrchestrator) {
		t.Fatalf("dispatch events = %+v, want non-Hermes orchestrator event", events)
	}
}

func TestDispatchOnce_FiveTodoCardsClaimsFourAndLeavesFifthInTodo(t *testing.T) {
	now := time.Date(2026, time.July, 17, 9, 0, 0, 0, time.UTC)
	store := newDispatchStore(4, []domain.WorkCard{
		todoCard("c1", domain.CardPriorityUrgent, now),
		todoCard("c2", domain.CardPriorityHigh, now),
		todoCard("c3", domain.CardPriorityNormal, now),
		todoCard("c4", domain.CardPriorityLow, now),
		todoCard("c5", domain.CardPriorityNormal, now),
	})
	spawner := &dispatchSpawner{}
	dispatcher := NewDispatcher(DispatchDeps{Store: store, Spawner: spawner, Clock: func() time.Time { return now }})

	claimed, err := dispatcher.DispatchOnce(context.Background(), "p1")
	if err != nil {
		t.Fatalf("DispatchOnce: %v", err)
	}
	wantClaimed := []string{"c1", "c2", "c3", "c5"}
	if !reflect.DeepEqual(claimed, wantClaimed) {
		t.Fatalf("claimed = %v, want %v", claimed, wantClaimed)
	}
	if got := spawner.cardIDs(); !reflect.DeepEqual(got, wantClaimed) {
		t.Fatalf("spawned cards = %v, want %v", got, wantClaimed)
	}
	for _, id := range claimed {
		card := store.cards[id]
		if card.Status != domain.CardStatusRunning || card.SessionID == "" {
			t.Fatalf("claimed card %s = %#v, want running and linked", id, card)
		}
	}
	if card := store.cards["c4"]; card.Status != domain.CardStatusTodo {
		t.Fatalf("fifth card status = %q, want Todo", card.Status)
	}
}

func TestDispatchOnce_HighestPrioritySpawnFailureReturnsTodoAndContinues(t *testing.T) {
	now := time.Date(2026, time.July, 17, 9, 0, 0, 0, time.UTC)
	store := newDispatchStore(4, []domain.WorkCard{
		todoCard("fail", domain.CardPriorityUrgent, now),
		todoCard("a", domain.CardPriorityHigh, now),
		todoCard("b", domain.CardPriorityNormal, now),
		todoCard("c", domain.CardPriorityLow, now),
		todoCard("d", domain.CardPriorityLow, now.Add(-time.Minute)),
	})
	spawner := &dispatchSpawner{failCardIDs: map[string]error{"fail": errors.New("spawn failed")}}
	dispatcher := NewDispatcher(DispatchDeps{Store: store, Spawner: spawner, Clock: func() time.Time { return now }})

	claimed, err := dispatcher.DispatchOnce(context.Background(), "p1")
	if err != nil {
		t.Fatalf("DispatchOnce: %v", err)
	}
	wantClaimed := []string{"a", "b", "c", "d"}
	if !reflect.DeepEqual(claimed, wantClaimed) {
		t.Fatalf("claimed = %v, want %v", claimed, wantClaimed)
	}
	wantSpawns := []string{"fail", "a", "b", "c", "d"}
	if got := spawner.cardIDs(); !reflect.DeepEqual(got, wantSpawns) {
		t.Fatalf("spawned cards = %v, want %v", got, wantSpawns)
	}
	for _, id := range claimed {
		card := store.cards[id]
		if card.Status != domain.CardStatusRunning || card.SessionID == "" {
			t.Fatalf("claimed card %s = %#v, want running and linked", id, card)
		}
	}
	if card := store.cards["fail"]; card.Status != domain.CardStatusTodo || card.SessionID != "" {
		t.Fatalf("failed card = %#v, want todo and unlinked", card)
	}
	events := store.events["fail"]
	if len(events) != 1 || !strings.Contains(events[0].Payload, dispatchFailedReasonSpawnFailed) {
		t.Fatalf("dispatch events = %+v, want one spawn_failed event", events)
	}
}

func TestDispatchOnce_RunningCardsAlwaysHaveLinkedSessionID(t *testing.T) {
	now := time.Date(2026, time.July, 17, 9, 0, 0, 0, time.UTC)
	store := newDispatchStore(2, []domain.WorkCard{
		readyCard("first", domain.CardPriorityHigh, now.Add(-time.Hour)),
		readyCard("second", domain.CardPriorityNormal, now.Add(-2*time.Hour)),
	})
	spawner := &dispatchSpawner{}
	dispatcher := NewDispatcher(DispatchDeps{Store: store, Spawner: spawner, Clock: func() time.Time { return now }})

	claimed, err := dispatcher.DispatchOnce(context.Background(), "p1")
	if err != nil {
		t.Fatalf("DispatchOnce: %v", err)
	}
	if len(claimed) != 2 {
		t.Fatalf("claimed = %v, want 2", claimed)
	}
	for _, id := range claimed {
		card := store.cards[id]
		if card.Status != domain.CardStatusRunning || card.SessionID == "" {
			t.Fatalf("claimed card %s = %#v, want running with linked session", id, card)
		}
	}
}

func TestDispatchOnceDoesNotSpawnWhenDurableClaimFails(t *testing.T) {
	now := time.Date(2026, time.July, 17, 9, 0, 0, 0, time.UTC)
	claimErr := errors.New("database unavailable")
	store := newDispatchStore(1, []domain.WorkCard{readyCard("card", domain.CardPriorityNormal, now)})
	store.failClaimErr = claimErr
	spawner := &dispatchSpawner{}
	dispatcher := NewDispatcher(DispatchDeps{Store: store, Spawner: spawner, Clock: func() time.Time { return now }})

	claimed, err := dispatcher.DispatchOnce(context.Background(), "p1")
	if !errors.Is(err, claimErr) {
		t.Fatalf("DispatchOnce error = %v, want %v", err, claimErr)
	}
	if len(claimed) != 0 {
		t.Fatalf("claimed = %v, want none", claimed)
	}
	if got := spawner.cardIDs(); len(got) != 0 {
		t.Fatalf("spawned cards = %v, want none", got)
	}
	card := store.cards["card"]
	if card.Status != domain.CardStatusReady || card.SessionID != "" {
		t.Fatalf("card after failed claim = %#v, want ready and unlinked", card)
	}
}

func TestDispatchOnce_TwoDispatchersAtomicallyRespectWIPLimit(t *testing.T) {
	now := time.Date(2026, time.July, 17, 9, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.UpsertProject(context.Background(), domain.ProjectRecord{
		ID: "p1", Path: t.TempDir(), RegisteredAt: now,
		Config: domain.ProjectConfig{Workboard: domain.WorkboardConfig{WIPLimit: 1}},
	}); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	for _, card := range []domain.WorkCard{
		readyCard("first", domain.CardPriorityHigh, now.Add(-time.Hour)),
		readyCard("second", domain.CardPriorityNormal, now.Add(-time.Hour)),
	} {
		if err := store.CreateWorkCard(context.Background(), card); err != nil {
			t.Fatalf("seed card %s: %v", card.ID, err)
		}
	}

	barrier := &listBarrierStore{Store: store, arrived: make(chan struct{}, 2), release: make(chan struct{})}
	spawner := &dispatchSpawner{sessionStore: store}
	dispatcherA := NewDispatcher(DispatchDeps{Store: barrier, Spawner: spawner, Clock: func() time.Time { return now }})
	dispatcherB := NewDispatcher(DispatchDeps{Store: barrier, Spawner: spawner, Clock: func() time.Time { return now }})
	type result struct {
		claimed []string
		err     error
	}
	results := make(chan result, 2)
	go func() {
		claimed, err := dispatcherA.DispatchOnce(context.Background(), "p1")
		results <- result{claimed, err}
	}()
	go func() {
		claimed, err := dispatcherB.DispatchOnce(context.Background(), "p1")
		results <- result{claimed, err}
	}()
	<-barrier.arrived
	<-barrier.arrived
	close(barrier.release)

	var totalClaims int
	for range 2 {
		result := <-results
		if result.err != nil {
			t.Fatalf("DispatchOnce: %v", result.err)
		}
		totalClaims += len(result.claimed)
	}
	if totalClaims != 1 {
		t.Fatalf("total claims = %d, want 1", totalClaims)
	}
	if got := spawner.cardIDs(); len(got) != 1 {
		t.Fatalf("spawned cards = %v, want exactly one", got)
	}
	cards, err := store.ListWorkCards(context.Background(), "p1", defaultBoardID)
	if err != nil {
		t.Fatalf("list cards: %v", err)
	}
	running := 0
	for _, card := range cards {
		if card.Status == domain.CardStatusRunning {
			running++
		}
	}
	if running != 1 {
		t.Fatalf("running cards = %d, want 1", running)
	}
}

func TestDispatchOnceRollsBackSpawnWhenSessionLinkFails(t *testing.T) {
	now := time.Date(2026, time.July, 17, 9, 0, 0, 0, time.UTC)
	linkErr := errors.New("database unavailable")
	store := newDispatchStore(1, []domain.WorkCard{readyCard("card", domain.CardPriorityNormal, now)})
	store.failLinkErr = linkErr
	spawner := &dispatchSpawner{}
	dispatcher := NewDispatcher(DispatchDeps{Store: store, Spawner: spawner, Clock: func() time.Time { return now }})

	claimed, err := dispatcher.DispatchOnce(context.Background(), "p1")
	if !errors.Is(err, linkErr) {
		t.Fatalf("DispatchOnce error = %v, want %v", err, linkErr)
	}
	if len(claimed) != 0 {
		t.Fatalf("claimed = %v, want none", claimed)
	}
	if got := spawner.rollbackIDs; !reflect.DeepEqual(got, []domain.SessionID{"session-card title\n\ncard notes"}) {
		t.Fatalf("rollback sessions = %v", got)
	}
	card := store.cards["card"]
	if card.Status != domain.CardStatusReady || card.SessionID != "" {
		t.Fatalf("card after failed link = %#v, want ready and unlinked", card)
	}
}

func TestDispatchOnceDurablyClaimsCardWhenSessionLinkAndRollbackFail(t *testing.T) {
	now := time.Date(2026, time.July, 17, 9, 0, 0, 0, time.UTC)
	linkErr := errors.New("database unavailable")
	rollbackErr := errors.New("runtime teardown unavailable")
	store := newDispatchStore(1, []domain.WorkCard{readyCard("card", domain.CardPriorityNormal, now)})
	store.failLinkErr = linkErr
	spawner := &dispatchSpawner{rollbackErr: rollbackErr}
	dispatcher := NewDispatcher(DispatchDeps{Store: store, Spawner: spawner, Clock: func() time.Time { return now }})

	claimed, err := dispatcher.DispatchOnce(context.Background(), "p1")
	if !errors.Is(err, linkErr) || !errors.Is(err, rollbackErr) {
		t.Fatalf("DispatchOnce error = %v, want joined link and rollback errors", err)
	}
	if !strings.Contains(err.Error(), "remains durably claimed") {
		t.Fatalf("DispatchOnce error = %v, want durable claim annotation", err)
	}
	if len(claimed) != 0 {
		t.Fatalf("claimed = %v, want none", claimed)
	}
	card := store.cards["card"]
	if card.Status != domain.CardStatusRunning || card.SessionID != "" {
		t.Fatalf("card after failed rollback = %#v, want running and unlinked durable claim", card)
	}

	newDispatcher := NewDispatcher(DispatchDeps{Store: store, Spawner: spawner, Clock: func() time.Time { return now }})
	claimed, err = newDispatcher.DispatchOnce(context.Background(), "p1")
	if err != nil {
		t.Fatalf("second DispatchOnce: %v", err)
	}
	if len(claimed) != 0 {
		t.Fatalf("second claimed = %v, want none", claimed)
	}
	if got := spawner.cardIDs(); !reflect.DeepEqual(got, []string{"card"}) {
		t.Fatalf("spawned cards = %v, want original spawn only", got)
	}
}

type dispatchStore struct {
	project      domain.ProjectRecord
	cards        map[string]domain.WorkCard
	events       map[string][]domain.WorkCardEvent
	sessions     []domain.SessionRecord
	failClaimErr error
	failLinkErr  error
	mu           sync.Mutex
}

func newDispatchStore(wipLimit int, cards []domain.WorkCard) *dispatchStore {
	s := &dispatchStore{
		project: domain.ProjectRecord{ID: "p1", Config: domain.ProjectConfig{Workboard: domain.WorkboardConfig{WIPLimit: wipLimit}}},
		cards:   make(map[string]domain.WorkCard, len(cards)),
		events:  make(map[string][]domain.WorkCardEvent),
	}
	for _, card := range cards {
		s.cards[card.ID] = card
	}
	return s
}

func (s *dispatchStore) GetProject(_ context.Context, id string) (domain.ProjectRecord, bool, error) {
	return s.project, id == s.project.ID, nil
}

func (s *dispatchStore) ListWorkCards(_ context.Context, projectID, boardID string) ([]domain.WorkCard, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if projectID != s.project.ID || boardID != defaultBoardID {
		return nil, nil
	}
	cards := make([]domain.WorkCard, 0, len(s.cards))
	for _, card := range s.cards {
		cards = append(cards, card)
	}
	return cards, nil
}

func (s *dispatchStore) ListSessions(_ context.Context, projectID domain.ProjectID) ([]domain.SessionRecord, error) {
	if projectID != domain.ProjectID(s.project.ID) {
		return nil, nil
	}
	return append([]domain.SessionRecord(nil), s.sessions...), nil
}

func (s *dispatchStore) UpdateWorkCard(_ context.Context, card domain.WorkCard) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if card.SessionID != "" && s.failLinkErr != nil {
		return s.failLinkErr
	}
	s.cards[card.ID] = card
	return nil
}

func (s *dispatchStore) AppendWorkCardEvent(_ context.Context, event domain.WorkCardEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events[event.CardID] = append(s.events[event.CardID], event)
	return nil
}

func (s *dispatchStore) ClaimReadyWorkCard(_ context.Context, cardID, projectID string, wipLimit int, at time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failClaimErr != nil {
		return false, s.failClaimErr
	}
	card, ok := s.cards[cardID]
	if !ok || card.ProjectID != projectID || card.Status != domain.CardStatusReady || card.PausedRetarget {
		return false, nil
	}
	running := 0
	for _, card := range s.cards {
		if card.ProjectID == projectID && card.Status == domain.CardStatusRunning {
			running++
		}
	}
	if running >= wipLimit {
		return false, nil
	}
	card.Status = domain.CardStatusRunning
	card.SessionID = ""
	card.UpdatedAt = at
	s.cards[cardID] = card
	return true, nil
}

type listBarrierStore struct {
	*sqlite.Store
	arrived chan struct{}
	release chan struct{}
}

func (s *listBarrierStore) ListWorkCards(ctx context.Context, projectID, boardID string) ([]domain.WorkCard, error) {
	cards, err := s.Store.ListWorkCards(ctx, projectID, boardID)
	if err != nil {
		return nil, err
	}
	s.arrived <- struct{}{}
	<-s.release
	return cards, nil
}

type dispatchSpawner struct {
	err                     error
	failCardIDs             map[string]error
	orchestratorErr         error
	failOrchestratorCardIDs map[string]error
	rollbackErr             error
	sessionStore            *sqlite.Store
	configs                 []ports.SpawnConfig
	orchestratorPrompts     []string
	orchestratorSession     domain.Session
	rollbackIDs             []domain.SessionID
	mu                      sync.Mutex
}

func (s *dispatchSpawner) Spawn(_ context.Context, cfg ports.SpawnConfig) (domain.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.configs = append(s.configs, cfg)
	title, _, _ := strings.Cut(cfg.Prompt, "\n\n")
	if err, ok := s.failCardIDs[strings.TrimSuffix(title, " title")]; ok {
		return domain.Session{}, err
	}
	if s.err != nil {
		return domain.Session{}, s.err
	}
	if s.sessionStore != nil {
		now := time.Now().UTC().Truncate(time.Second)
		rec, err := s.sessionStore.CreateSession(context.Background(), domain.SessionRecord{
			ProjectID: cfg.ProjectID,
			Kind:      cfg.Kind,
			Harness:   cfg.Harness,
			Activity:  domain.Activity{State: domain.ActivityActive, LastActivityAt: now},
			CreatedAt: now,
			UpdatedAt: now,
		})
		if err != nil {
			return domain.Session{}, err
		}
		return domain.Session{SessionRecord: rec}, nil
	}
	return domain.Session{SessionRecord: domain.SessionRecord{ID: domain.SessionID("session-" + cfg.Prompt)}}, nil
}

func (s *dispatchSpawner) RollbackSpawn(_ context.Context, id domain.SessionID) (sessionsvc.RollbackOutcome, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rollbackIDs = append(s.rollbackIDs, id)
	return sessionsvc.RollbackOutcome{Killed: s.rollbackErr == nil}, s.rollbackErr
}

func (s *dispatchSpawner) SpawnOrchestrator(_ context.Context, _ domain.ProjectID, _ bool, prompt string) (domain.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.orchestratorPrompts = append(s.orchestratorPrompts, prompt)
	cardID := extractCardIDFromBriefing(prompt)
	if err, ok := s.failOrchestratorCardIDs[cardID]; ok {
		return domain.Session{}, err
	}
	if s.orchestratorErr != nil {
		return domain.Session{}, s.orchestratorErr
	}
	return s.orchestratorSession, nil
}

func extractCardIDFromBriefing(prompt string) string {
	idx := strings.Index(prompt, `"cardId":"`)
	if idx == -1 {
		return ""
	}
	rest := prompt[idx+len(`"cardId":"`):]
	if end := strings.Index(rest, `"`); end != -1 {
		return rest[:end]
	}
	return rest
}

func (s *dispatchSpawner) cardIDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ids []string
	for _, cfg := range s.configs {
		title, _, _ := strings.Cut(cfg.Prompt, "\n\n")
		ids = append(ids, strings.TrimSuffix(title, " title"))
	}
	return ids
}

func readyCard(id string, priority domain.CardPriority, readyAt time.Time) domain.WorkCard {
	return domain.WorkCard{
		ID: id, ProjectID: "p1", BoardID: defaultBoardID, Title: id + " title", Notes: id + " notes",
		Priority: priority, Status: domain.CardStatusReady, ReadyAt: ptrTime(readyAt), Agent: string(domain.HarnessCodex),
	}
}

func todoCard(id string, priority domain.CardPriority, createdAt time.Time) domain.WorkCard {
	card := readyCard(id, priority, createdAt)
	card.Status = domain.CardStatusTodo
	card.ReadyAt = nil
	card.CreatedAt = createdAt
	return card
}

func ptrTime(t time.Time) *time.Time { return &t }
