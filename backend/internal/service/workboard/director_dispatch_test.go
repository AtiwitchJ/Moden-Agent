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
