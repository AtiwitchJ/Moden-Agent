package workboard

import (
	"context"
	"testing"
	"time"

	"github.com/modernagent/modern-agent/backend/internal/domain"
)

// TestLifecycleDispatcherIsUnwired_RunningCardShouldAdvanceToReview_WhenTickRun
// proves that a card in `running` SHOULD automatically transition to `review`
// after a tick.  On main the dispatcher only promotes Todo→Ready→Running; there
// is no producer for the running→review transition.  This test is RED on main
// because DispatchOnce currently does nothing for cards already in running.
func TestLifecycleDispatcherIsUnwired_RunningCardShouldAdvanceToReview_WhenTickRun(t *testing.T) {
	now := time.Date(2026, time.August, 3, 0, 0, 0, 0, time.UTC)

	// A card already in running with a linked session (simulating a Hermes
	// session that has been running and completed its work).
	runningCard := domain.WorkCard{
		ID:        "running-1",
		ProjectID: "p1",
		BoardID:   defaultBoardID,
		Status:    domain.CardStatusRunning,
		SessionID: "hermes-session-1",
		Priority:  domain.CardPriorityHigh,
		Title:     "work complete",
		CreatedAt: now.Add(-30 * time.Minute),
		UpdatedAt: now.Add(-1 * time.Minute),
	}

	store := newDispatchStore(4, []domain.WorkCard{runningCard})
	dispatcher := NewDispatcher(DispatchDeps{
		Store:   store,
		Spawner: &dispatchSpawner{},
		Clock:   func() time.Time { return now },
	})

	_, err := dispatcher.DispatchOnce(context.Background(), "p1")
	if err != nil {
		t.Fatalf("DispatchOnce: %v", err)
	}

	// The orchestrator tick SHOULD promote running→review automatically.
	// RED on main: the card stays in running because nothing drives this transition.
	if got := store.cards["running-1"].Status; got != domain.CardStatusReview {
		t.Fatalf("card status after tick = %q, want review (orchestrator tick should advance running cards)", got)
	}
}

// TestLifecycleDispatcherIsUnwired_ReviewCardShouldAdvanceToTesting_WhenTickRun
// asserts that a card in `review` SHOULD automatically transition to `testing`
// after a tick (reviewer approved).  On main nothing produces this transition.
func TestLifecycleDispatcherIsUnwired_ReviewCardShouldAdvanceToTesting_WhenTickRun(t *testing.T) {
	now := time.Date(2026, time.August, 3, 0, 0, 0, 0, time.UTC)

	reviewCard := domain.WorkCard{
		ID:        "card-testing",
		ProjectID: "p1",
		BoardID:   defaultBoardID,
		Status:    domain.CardStatusReview,
		SessionID: "reviewer-session-1",
		Priority:  domain.CardPriorityNormal,
		Title:     "card in review",
		CreatedAt: now.Add(-20 * time.Minute),
		UpdatedAt: now.Add(-1 * time.Minute),
	}

	store := newDispatchStore(4, []domain.WorkCard{reviewCard})
	dispatcher := NewDispatcher(DispatchDeps{
		Store:   store,
		Spawner: &dispatchSpawner{},
		Clock:   func() time.Time { return now },
	})

	_, err := dispatcher.DispatchOnce(context.Background(), "p1")
	if err != nil {
		t.Fatalf("DispatchOnce: %v", err)
	}

	// RED on main: no automatic testing producer exists.
	if got := store.cards["card-testing"].Status; got != domain.CardStatusTesting {
		t.Fatalf("card status = %q, want testing (orchestrator should advance review→testing)", got)
	}
}

// TestLifecycleDispatcherIsUnwired_TestingCardShouldAdvanceToDone_WhenTickRun
// asserts that a card in `testing` SHOULD automatically transition to `done`
// after all tests pass.  On main nothing produces this transition.
func TestLifecycleDispatcherIsUnwired_TestingCardShouldAdvanceToDone_WhenTickRun(t *testing.T) {
	now := time.Date(2026, time.August, 3, 0, 0, 0, 0, time.UTC)

	testingCard := domain.WorkCard{
		ID:        "card-done",
		ProjectID: "p1",
		BoardID:   defaultBoardID,
		Status:    domain.CardStatusTesting,
		SessionID: "tester-session-1",
		Priority:  domain.CardPriorityNormal,
		Title:     "card in testing",
		CreatedAt: now.Add(-15 * time.Minute),
		UpdatedAt: now.Add(-1 * time.Minute),
	}

	store := newDispatchStore(4, []domain.WorkCard{testingCard})
	dispatcher := NewDispatcher(DispatchDeps{
		Store:   store,
		Spawner: &dispatchSpawner{},
		Clock:   func() time.Time { return now },
	})

	_, err := dispatcher.DispatchOnce(context.Background(), "p1")
	if err != nil {
		t.Fatalf("DispatchOnce: %v", err)
	}

	// RED on main: no automatic done producer exists.
	if got := store.cards["card-done"].Status; got != domain.CardStatusDone {
		t.Fatalf("card status = %q, want done (orchestrator should advance testing→done)", got)
	}
}
