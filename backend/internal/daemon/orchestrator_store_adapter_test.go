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

	// active_session.card_id FKs to work_cards.id, so seed a project + card
	// before inserting the active-session row.
	if err := store.UpsertProject(ctx, domain.ProjectRecord{ID: "p1", Path: "/repo", RegisteredAt: time.Now()}); err != nil {
		t.Fatalf("UpsertProject: %v", err)
	}
	if err := store.CreateWorkCard(ctx, domain.WorkCard{
		ID: "card-1", ProjectID: "p1", BoardID: "default", Title: "t",
		Priority: domain.CardPriorityNormal, Status: domain.CardStatusRunning,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("CreateWorkCard: %v", err)
	}

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
