package workboard

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/modernagent/modern-agent/backend/internal/domain"
)

func TestDirectorEnabled(t *testing.T) {
	off := domain.ProjectRecord{Config: domain.ProjectConfig{}}
	if directorEnabled(off) {
		t.Fatal("directorEnabled = true for a project with no director configured")
	}
	on := domain.ProjectRecord{Config: domain.ProjectConfig{
		Director: domain.RoleOverride{Harness: domain.HarnessDirector},
	}}
	if !directorEnabled(on) {
		t.Fatal("directorEnabled = false for a project with a director configured")
	}
	wrongHarness := domain.ProjectRecord{Config: domain.ProjectConfig{
		Director: domain.RoleOverride{Harness: domain.HarnessCodex},
	}}
	if directorEnabled(wrongHarness) {
		t.Fatal("directorEnabled = true for a non-Director harness")
	}
}

func TestDispatchOnceRollsBackDirectorWhenCardLinkFails(t *testing.T) {
	now := time.Date(2026, time.August, 3, 9, 0, 0, 0, time.UTC)
	store := newDispatchStore(4, []domain.WorkCard{todoCard("c1", domain.CardPriorityNormal, now)})
	store.project.Config = domain.ProjectConfig{Director: domain.RoleOverride{Harness: domain.HarnessDirector}}
	store.failLinkErr = errors.New("database unavailable")
	spawner := &dispatchSpawner{}
	dispatcher := NewDispatcher(DispatchDeps{Store: store, Spawner: spawner, Clock: func() time.Time { return now }})

	claimed, err := dispatcher.DispatchOnce(context.Background(), "p1")
	if err != nil {
		t.Fatalf("DispatchOnce: %v", err)
	}
	if len(claimed) != 0 {
		t.Fatalf("claimed = %v, want no linked card", claimed)
	}
	if len(spawner.rollbackIDs) != 1 {
		t.Fatalf("rollback calls = %v, want the spawned Director rolled back", spawner.rollbackIDs)
	}
	if card := store.cards["c1"]; card.Status != domain.CardStatusTodo || card.SessionID != "" {
		t.Fatalf("card = %+v, want its todo state restored", card)
	}
}

func TestIsProjectDirector(t *testing.T) {
	live := domain.SessionRecord{Kind: domain.KindOrchestrator, Harness: domain.HarnessDirector}
	if !isProjectDirector(live, domain.HarnessDirector) {
		t.Fatal("a live orchestrator session on the director harness should count")
	}
	terminated := live
	terminated.IsTerminated = true
	if isProjectDirector(terminated, domain.HarnessDirector) {
		t.Fatal("a terminated session should not count")
	}
	worker := domain.SessionRecord{Kind: domain.KindWorker, Harness: domain.HarnessDirector}
	if isProjectDirector(worker, domain.HarnessDirector) {
		t.Fatal("a worker session should not count")
	}
	other := domain.SessionRecord{Kind: domain.KindOrchestrator, Harness: domain.HarnessClaudeCode}
	if isProjectDirector(other, domain.HarnessDirector) {
		t.Fatal("a different harness should not count")
	}
}

func TestDispatchOnceSpawnsDirectorForOptedInProject(t *testing.T) {
	now := time.Date(2026, time.August, 3, 9, 0, 0, 0, time.UTC)
	store := newDispatchStore(4, []domain.WorkCard{todoCard("c1", domain.CardPriorityNormal, now)})
	store.project.Config = domain.ProjectConfig{Director: domain.RoleOverride{Harness: domain.HarnessDirector}}
	spawner := &dispatchSpawner{}
	dispatcher := NewDispatcher(DispatchDeps{Store: store, Spawner: spawner, Clock: func() time.Time { return now }})

	if _, err := dispatcher.DispatchOnce(context.Background(), "p1"); err != nil {
		t.Fatalf("DispatchOnce: %v", err)
	}
	if len(spawner.configs) != 1 {
		t.Fatalf("spawn calls = %d, want 1", len(spawner.configs))
	}
	got := spawner.configs[0]
	if got.Harness != domain.HarnessDirector {
		t.Fatalf("Harness = %q, want %q", got.Harness, domain.HarnessDirector)
	}
	if got.Kind != domain.KindOrchestrator {
		t.Fatalf("Kind = %q, want %q", got.Kind, domain.KindOrchestrator)
	}
	if got.DirectorCardID != "c1" {
		t.Fatalf("DirectorCardID = %q, want c1", got.DirectorCardID)
	}
	if card := store.cards["c1"]; card.Status != domain.CardStatusRunning || card.SessionID == "" {
		t.Fatalf("card = %+v, want running and linked to the Director", card)
	}
}

func TestDispatchOnceReusesTheLiveDirectorSession(t *testing.T) {
	now := time.Date(2026, time.August, 3, 9, 0, 0, 0, time.UTC)
	store := newDispatchStore(4, []domain.WorkCard{todoCard("c1", domain.CardPriorityNormal, now)})
	store.project.Config = domain.ProjectConfig{Director: domain.RoleOverride{Harness: domain.HarnessDirector}}
	store.sessions = []domain.SessionRecord{{
		ID: "director-1", ProjectID: "p1", Kind: domain.KindOrchestrator, Harness: domain.HarnessDirector,
	}}
	spawner := &dispatchSpawner{}
	dispatcher := NewDispatcher(DispatchDeps{Store: store, Spawner: spawner, Clock: func() time.Time { return now }})

	if _, err := dispatcher.DispatchOnce(context.Background(), "p1"); err != nil {
		t.Fatalf("DispatchOnce: %v", err)
	}
	if len(spawner.configs) != 0 {
		t.Fatalf("spawn calls = %d, want 0 (the live director should be reused)", len(spawner.configs))
	}
}

func TestDispatchOnceLeavesNonDirectorProjectsUnchanged(t *testing.T) {
	now := time.Date(2026, time.August, 3, 9, 0, 0, 0, time.UTC)
	store := newDispatchStore(4, []domain.WorkCard{todoCard("c1", domain.CardPriorityNormal, now)})
	spawner := &dispatchSpawner{}
	dispatcher := NewDispatcher(DispatchDeps{Store: store, Spawner: spawner, Clock: func() time.Time { return now }})

	claimed, err := dispatcher.DispatchOnce(context.Background(), "p1")
	if err != nil {
		t.Fatalf("DispatchOnce: %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("claimed = %v, want the existing worker path to claim the card", claimed)
	}
	if len(spawner.configs) != 1 || spawner.configs[0].Kind != domain.KindWorker {
		t.Fatalf("configs = %+v, want one worker spawn from the unchanged path", spawner.configs)
	}
}

// TestDispatchOnceRecoversStuckCardAfterDirectorExit is the regression for the
// production symptom where a card stays durably status=running with a session_id
// pointing at a Director session that has been marked terminated. The
// dispatcher must not return wip_full when the only running card has no live
// orchestrator behind it: it must reclaim the card and spawn a fresh Director.
func TestDispatchOnceRecoversStuckCardAfterDirectorExit(t *testing.T) {
	now := time.Date(2026, time.August, 3, 9, 0, 0, 0, time.UTC)
	stuck := readyCard("stuck", domain.CardPriorityNormal, now.Add(-time.Minute))
	stuck.Status = domain.CardStatusRunning
	stuck.SessionID = "test-20"
	store := newDispatchStore(4, []domain.WorkCard{stuck})
	store.project.Config = domain.ProjectConfig{Director: domain.RoleOverride{Harness: domain.HarnessDirector}}
	store.sessions = []domain.SessionRecord{{
		ID: "test-20", ProjectID: "p1", Kind: domain.KindOrchestrator, Harness: domain.HarnessDirector,
		IsTerminated: true,
	}}
	spawner := &dispatchSpawner{}
	dispatcher := NewDispatcher(DispatchDeps{Store: store, Spawner: spawner, Clock: func() time.Time { return now }})

	claimed, err := dispatcher.DispatchOnce(context.Background(), "p1")
	if err != nil {
		t.Fatalf("DispatchOnce: %v", err)
	}
	if len(claimed) != 1 || claimed[0] != "stuck" {
		t.Fatalf("claimed = %v, want the stuck card reclaimed", claimed)
	}
	if len(spawner.configs) != 1 {
		t.Fatalf("spawn calls = %d, want 1 (a fresh Director must be spawned)", len(spawner.configs))
	}
	got := spawner.configs[0]
	if got.Harness != domain.HarnessDirector || got.Kind != domain.KindOrchestrator || got.DirectorCardID != "stuck" {
		t.Fatalf("spawn config = %+v, want a Director spawn for the stuck card", got)
	}
}

// TestNextDirectorCardReclaimsLaterPhaseStuckCards covers the production
// scenario where a Director's node process deadlocks (e.g. a stuck LLM provider
// promise) while a card is parked in a later phase like review, testing, or
// redo. A dead Director is one whose session is no longer live; the dispatcher
// must reclaim that card and let a fresh Director take over, otherwise the
// card never advances and the user has no automated recovery path.
func TestNextDirectorCardReclaimsLaterPhaseStuckCards(t *testing.T) {
	now := time.Date(2026, time.August, 3, 9, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		status domain.CardStatus
	}{
		{"review", domain.CardStatusReview},
		{"testing", domain.CardStatusTesting},
		{"redo", domain.CardStatusRedo},
		{"blocked", domain.CardStatusBlocked},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stuck := todoCard("stuck", domain.CardPriorityNormal, now.Add(-time.Minute))
			stuck.Status = tc.status
			stuck.SessionID = "test-21"
			liveSessions := map[domain.SessionID]bool{} // no live sessions — Director died
			got, _, ok := nextDirectorCard([]domain.WorkCard{stuck}, liveSessions, now)
			if !ok {
				t.Fatalf("nextDirectorCard ok = false, want a later-phase stuck card reclaimed")
			}
			if got.ID != "stuck" {
				t.Fatalf("got card %q, want stuck", got.ID)
			}
			if got.SessionID != "" {
				t.Fatalf("got SessionID %q, want cleared so a fresh Director can claim it", got.SessionID)
			}
		})
	}
}

// TestNextDirectorCardLeavesLaterPhaseAloneWhenDirectorLive ensures the
// recovery path is targeted: when a card is in a later phase and its linked
// Director is still alive, the dispatcher must NOT touch it — the live
// Director owns the phase transitions and a parallel spawn would race it.
func TestNextDirectorCardLeavesLaterPhaseAloneWhenDirectorLive(t *testing.T) {
	now := time.Date(2026, time.August, 3, 9, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		status domain.CardStatus
	}{
		{"review", domain.CardStatusReview},
		{"testing", domain.CardStatusTesting},
		{"redo", domain.CardStatusRedo},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stuck := todoCard("stuck", domain.CardPriorityNormal, now.Add(-time.Minute))
			stuck.Status = tc.status
			stuck.SessionID = "test-21"
			liveSessions := map[domain.SessionID]bool{"test-21": true}
			_, _, ok := nextDirectorCard([]domain.WorkCard{stuck}, liveSessions, now)
			if ok {
				t.Fatalf("nextDirectorCard returned a card while Director test-21 was still live; dispatcher should not race the live Director")
			}
		})
	}
}
