package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/modernagent/modern-agent/backend/internal/domain"
)

// TestListRedoCyclesPopulatesFindings proves a cycle's findings actually come
// back with it. Before this fix, ListRedoCycles built each domain.RedoCycle
// without ever reading work_card_redo_findings, so .Findings was always empty
// regardless of what InsertRedoFinding recorded — the exact data
// commander/orchestrator's redo-respawn briefing depends on to tell a retry
// what needs fixing.
func TestListRedoCyclesPopulatesFindings(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, time.August, 4, 9, 0, 0, 0, time.UTC)
	seedProject(t, s, "p1")
	card := domain.WorkCard{
		ID: "card-1", ProjectID: "p1", BoardID: "default", Title: "Ship API",
		Priority: domain.CardPriorityNormal, Labels: []string{}, Status: domain.CardStatusRedo,
		TargetPath: "/tmp/p1", Agent: "codex", GoalVersion: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.CreateWorkCard(ctx, card); err != nil {
		t.Fatalf("create card: %v", err)
	}
	cycle := domain.RedoCycle{
		ID: "cycle-1", CardID: "card-1", CycleNumber: 1,
		Source: "review agent exhausted", Summary: "review phase failed (changes_requested); entering redo",
		CreatedAt: now,
	}
	if err := s.InsertRedoCycle(ctx, cycle); err != nil {
		t.Fatalf("InsertRedoCycle: %v", err)
	}
	finding := domain.RedoFinding{
		ID: "finding-1", CycleID: "cycle-1", Sequence: 1, Severity: domain.FindingSeverityHigh,
		Title: "Empty cart crashes checkout", Details: "POST /checkout with 0 items returns 500",
		FileRefs: []domain.FileRef{{File: "src/checkout.ts", StartLine: 42}},
		Status:   domain.FindingStatusPending, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.InsertRedoFinding(ctx, finding); err != nil {
		t.Fatalf("InsertRedoFinding: %v", err)
	}

	cycles, err := s.ListRedoCycles(ctx, "card-1")
	if err != nil {
		t.Fatalf("ListRedoCycles: %v", err)
	}
	if len(cycles) != 1 {
		t.Fatalf("cycles = %d, want 1", len(cycles))
	}
	if len(cycles[0].Findings) != 1 {
		t.Fatalf("findings = %d, want 1", len(cycles[0].Findings))
	}
	got := cycles[0].Findings[0]
	if got.Title != "Empty cart crashes checkout" || got.Severity != domain.FindingSeverityHigh {
		t.Fatalf("finding = %+v, want the seeded title/severity", got)
	}
	if len(got.FileRefs) != 1 || got.FileRefs[0].File != "src/checkout.ts" {
		t.Fatalf("finding file refs = %+v, want src/checkout.ts", got.FileRefs)
	}
}
