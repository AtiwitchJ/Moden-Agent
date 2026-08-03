package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/modernagent/modern-agent/backend/internal/domain"
	"github.com/modernagent/modern-agent/backend/internal/storage/sqlite"
)

// seedWorkCard inserts a minimal work card so active_session rows (which FK
// to work_cards.id) have somewhere to point.
func seedWorkCard(t *testing.T, s *sqlite.Store, projectID, cardID string) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	card := domain.WorkCard{
		ID: cardID, ProjectID: projectID, BoardID: "default",
		Title: "seed card", Priority: domain.CardPriorityNormal,
		Status: domain.CardStatusTriage, TargetPath: "/tmp/" + cardID, Agent: "codex",
		GoalVersion: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.CreateWorkCard(context.Background(), card); err != nil {
		t.Fatalf("seed work card %s: %v", cardID, err)
	}
}

func TestActiveSessionRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedProject(t, s, "mer")
	seedWorkCard(t, s, "mer", "card-1")

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

func TestInsertActiveSessionUpsertsOnConflict(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedProject(t, s, "mer")
	seedWorkCard(t, s, "mer", "card-1")

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
}

func TestGetActiveSessionMissingReturnsNotOK(t *testing.T) {
	s := newTestStore(t)

	_, ok, err := s.GetActiveSession(context.Background(), "no-such-card")
	if err != nil {
		t.Fatalf("GetActiveSession: %v", err)
	}
	if ok {
		t.Fatal("ok = true for a card with no active session, want false")
	}
}
