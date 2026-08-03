package workboard

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/modernagent/modern-agent/backend/internal/domain"
)

func TestWIPLimitCountsAcrossAllActivePhases(t *testing.T) {
	now := time.Date(2026, time.August, 3, 0, 0, 0, 0, time.UTC)

	// 3 cards in running, 1 in review — WIP limit is 4, so no more can dispatch
	store := newDispatchStore(4, []domain.WorkCard{
		{ID: "r1", ProjectID: "p1", BoardID: defaultBoardID, Status: domain.CardStatusRunning},
		{ID: "r2", ProjectID: "p1", BoardID: defaultBoardID, Status: domain.CardStatusRunning},
		{ID: "r3", ProjectID: "p1", BoardID: defaultBoardID, Status: domain.CardStatusRunning},
		{ID: "rv1", ProjectID: "p1", BoardID: defaultBoardID, Status: domain.CardStatusReview},
		{ID: "todo1", ProjectID: "p1", BoardID: defaultBoardID, Status: domain.CardStatusTodo},
	})

	dispatcher := NewDispatcher(DispatchDeps{
		Store:   store,
		Spawner: &dispatchSpawner{},
		Clock:   func() time.Time { return now },
	})

	claimed, err := dispatcher.DispatchOnce(context.Background(), "p1")
	if err != nil {
		t.Fatalf("DispatchOnce: %v", err)
	}

	// WIP is full (4 active), todo1 must not be claimed
	if len(claimed) != 0 {
		t.Fatalf("claimed = %v, want none (WIP full with 4 active cards)", claimed)
	}
}

func TestCompletingACardUnblocksNext(t *testing.T) {
	now := time.Date(2026, time.August, 3, 0, 0, 0, 0, time.UTC)

	// 4 active cards (at WIP limit) — todo1 waits
	store := newDispatchStore(4, []domain.WorkCard{
		{ID: "r1", ProjectID: "p1", BoardID: defaultBoardID, Status: domain.CardStatusRunning},
		{ID: "r2", ProjectID: "p1", BoardID: defaultBoardID, Status: domain.CardStatusRunning},
		{ID: "r3", ProjectID: "p1", BoardID: defaultBoardID, Status: domain.CardStatusRunning},
		{ID: "r4", ProjectID: "p1", BoardID: defaultBoardID, Status: domain.CardStatusRunning},
		{ID: "todo1", ProjectID: "p1", BoardID: defaultBoardID, Status: domain.CardStatusTodo},
	})

	// Simulate r1 completing (transitioning to done, no longer active)
	r1Card := store.cards["r1"]
	r1Card.Status = domain.CardStatusDone
	store.cards["r1"] = r1Card

	dispatcher := NewDispatcher(DispatchDeps{
		Store:   store,
		Spawner: &dispatchSpawner{},
		Clock:   func() time.Time { return now },
	})

	claimed, err := dispatcher.DispatchOnce(context.Background(), "p1")
	if err != nil {
		t.Fatalf("DispatchOnce: %v", err)
	}

	// Now WIP has room, todo1 should be claimed
	if !slices.Contains(claimed, "todo1") {
		t.Fatalf("claimed = %v, want todo1 (WIP had room after r1 completed)", claimed)
	}
}

func TestWIPCountsTestingPhase(t *testing.T) {
	now := time.Date(2026, time.August, 3, 0, 0, 0, 0, time.UTC)

	// 2 running + 2 testing = 4 active, at WIP limit
	store := newDispatchStore(4, []domain.WorkCard{
		{ID: "r1", ProjectID: "p1", BoardID: defaultBoardID, Status: domain.CardStatusRunning},
		{ID: "r2", ProjectID: "p1", BoardID: defaultBoardID, Status: domain.CardStatusRunning},
		{ID: "t1", ProjectID: "p1", BoardID: defaultBoardID, Status: domain.CardStatusTesting},
		{ID: "t2", ProjectID: "p1", BoardID: defaultBoardID, Status: domain.CardStatusTesting},
		{ID: "todo1", ProjectID: "p1", BoardID: defaultBoardID, Status: domain.CardStatusTodo},
	})

	dispatcher := NewDispatcher(DispatchDeps{
		Store:   store,
		Spawner: &dispatchSpawner{},
		Clock:   func() time.Time { return now },
	})

	claimed, err := dispatcher.DispatchOnce(context.Background(), "p1")
	if err != nil {
		t.Fatalf("DispatchOnce: %v", err)
	}

	// WIP is full with 4 active cards (2 running + 2 testing)
	if len(claimed) != 0 {
		t.Fatalf("claimed = %v, want none (WIP full with 2 running + 2 testing)", claimed)
	}
}

func TestWIPCountsRedoPhase(t *testing.T) {
	now := time.Date(2026, time.August, 3, 0, 0, 0, 0, time.UTC)

	// 3 running + 1 redo = 4 active, at WIP limit
	store := newDispatchStore(4, []domain.WorkCard{
		{ID: "r1", ProjectID: "p1", BoardID: defaultBoardID, Status: domain.CardStatusRunning},
		{ID: "r2", ProjectID: "p1", BoardID: defaultBoardID, Status: domain.CardStatusRunning},
		{ID: "r3", ProjectID: "p1", BoardID: defaultBoardID, Status: domain.CardStatusRunning},
		{ID: "redo1", ProjectID: "p1", BoardID: defaultBoardID, Status: domain.CardStatusRedo},
		{ID: "todo1", ProjectID: "p1", BoardID: defaultBoardID, Status: domain.CardStatusTodo},
	})

	dispatcher := NewDispatcher(DispatchDeps{
		Store:   store,
		Spawner: &dispatchSpawner{},
		Clock:   func() time.Time { return now },
	})

	claimed, err := dispatcher.DispatchOnce(context.Background(), "p1")
	if err != nil {
		t.Fatalf("DispatchOnce: %v", err)
	}

	// WIP is full with 4 active (3 running + 1 redo)
	if len(claimed) != 0 {
		t.Fatalf("claimed = %v, want none (WIP full with 3 running + 1 redo)", claimed)
	}
}

func TestWIPWithMixedActivePhases(t *testing.T) {
	now := time.Date(2026, time.August, 3, 0, 0, 0, 0, time.UTC)

	// 1 running + 1 review + 1 testing + 1 redo = 4 active, at WIP limit
	store := newDispatchStore(4, []domain.WorkCard{
		{ID: "r1", ProjectID: "p1", BoardID: defaultBoardID, Status: domain.CardStatusRunning},
		{ID: "rv1", ProjectID: "p1", BoardID: defaultBoardID, Status: domain.CardStatusReview},
		{ID: "t1", ProjectID: "p1", BoardID: defaultBoardID, Status: domain.CardStatusTesting},
		{ID: "redo1", ProjectID: "p1", BoardID: defaultBoardID, Status: domain.CardStatusRedo},
		{ID: "todo1", ProjectID: "p1", BoardID: defaultBoardID, Status: domain.CardStatusTodo},
	})

	dispatcher := NewDispatcher(DispatchDeps{
		Store:   store,
		Spawner: &dispatchSpawner{},
		Clock:   func() time.Time { return now },
	})

	claimed, err := dispatcher.DispatchOnce(context.Background(), "p1")
	if err != nil {
		t.Fatalf("DispatchOnce: %v", err)
	}

	// WIP is full with 4 active across all phases
	if len(claimed) != 0 {
		t.Fatalf("claimed = %v, want none (WIP full with mixed active phases)", claimed)
	}
}
