package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/modernagent/modern-agent/backend/internal/domain"
)

func TestPrepareHermesAnswerAttemptAtomicallyConsumesOneShot(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, time.July, 17, 9, 10, 0, 0, time.UTC)
	project := domain.ProjectRecord{
		ID: "mer", Path: "/tmp/mer", RegisteredAt: now,
		Config: domain.ProjectConfig{Workboard: domain.WorkboardConfig{Autonomous: domain.WorkboardAutonomousConfig{Enabled: true, Mode: "skip_timeout", Sticky: false}}},
	}
	if err := s.UpsertProject(ctx, project); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	card := domain.WorkCard{ID: "card-1", ProjectID: "mer", BoardID: "default", Title: "Ship API", Priority: domain.CardPriorityNormal, Labels: []string{}, Status: domain.CardStatusRunning, TargetPath: "/tmp/mer", Agent: "codex", GoalVersion: 1, CreatedAt: now, UpdatedAt: now}
	if err := s.CreateWorkCard(ctx, card); err != nil {
		t.Fatalf("create card: %v", err)
	}
	event := domain.WorkCardEvent{ID: "attempt-1", CardID: card.ID, ProjectID: card.ProjectID, Kind: "hermes_answer_requested", Payload: `{}`, CreatedAt: now}
	prepared, err := s.PrepareHermesAnswerAttempt(ctx, project.ID, project.Config.Workboard, event, true)
	if err != nil || !prepared {
		t.Fatalf("PrepareHermesAnswerAttempt: prepared=%t err=%v", prepared, err)
	}
	gotProject, ok, err := s.GetProject(ctx, "mer")
	if err != nil || !ok || gotProject.Config.Workboard.Autonomous.Enabled {
		t.Fatalf("project after prepare = %+v ok=%t err=%v", gotProject.Config.Workboard.Autonomous, ok, err)
	}
	events, err := s.ListWorkCardEvents(ctx, card.ID)
	if err != nil || len(events) != 1 || events[0].ID != event.ID {
		t.Fatalf("events after prepare = %+v err=%v", events, err)
	}
}

func TestPatchWorkboardAutonomousDoesNotRestoreConsumedOneShot(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, time.July, 17, 9, 10, 0, 0, time.UTC)
	project := domain.ProjectRecord{
		ID: "mer", Path: "/tmp/mer", RegisteredAt: now,
		Config: domain.ProjectConfig{Workboard: domain.WorkboardConfig{Autonomous: domain.WorkboardAutonomousConfig{Enabled: true, Mode: "skip_timeout", Sticky: false}}},
	}
	if err := s.UpsertProject(ctx, project); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	card := domain.WorkCard{ID: "card-1", ProjectID: "mer", BoardID: "default", Title: "Ship API", Priority: domain.CardPriorityNormal, Labels: []string{}, Status: domain.CardStatusRunning, TargetPath: "/tmp/mer", Agent: "codex", GoalVersion: 1, CreatedAt: now, UpdatedAt: now}
	if err := s.CreateWorkCard(ctx, card); err != nil {
		t.Fatalf("create card: %v", err)
	}
	event := domain.WorkCardEvent{ID: "attempt-1", CardID: card.ID, ProjectID: card.ProjectID, Kind: "hermes_answer_requested", Payload: `{}`, CreatedAt: now}
	prepared, err := s.PrepareHermesAnswerAttempt(ctx, project.ID, project.Config.Workboard, event, true)
	if err != nil || !prepared {
		t.Fatalf("PrepareHermesAnswerAttempt: prepared=%t err=%v", prepared, err)
	}
	sticky := true
	updated, ok, err := s.PatchWorkboardAutonomous(ctx, project.ID, domain.WorkboardAutonomousPatch{Sticky: &sticky})
	if err != nil || !ok {
		t.Fatalf("PatchWorkboardAutonomous: updated=%+v ok=%t err=%v", updated, ok, err)
	}
	if updated.Config.Workboard.Autonomous.Enabled || !updated.Config.Workboard.Autonomous.Sticky {
		t.Fatalf("patched autonomous config = %+v, want consumed enabled=false and sticky=true", updated.Config.Workboard.Autonomous)
	}
}

func TestPrepareHermesAnswerAttemptRollsBackOneShotWithoutEvent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, time.July, 17, 9, 10, 0, 0, time.UTC)
	project := domain.ProjectRecord{
		ID: "mer", Path: "/tmp/mer", RegisteredAt: now,
		Config: domain.ProjectConfig{Workboard: domain.WorkboardConfig{Autonomous: domain.WorkboardAutonomousConfig{Enabled: true, Mode: "skip_timeout", Sticky: false}}},
	}
	if err := s.UpsertProject(ctx, project); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	card := domain.WorkCard{ID: "card-1", ProjectID: "mer", BoardID: "default", Title: "Ship API", Priority: domain.CardPriorityNormal, Labels: []string{}, Status: domain.CardStatusRunning, TargetPath: "/tmp/mer", Agent: "codex", GoalVersion: 1, CreatedAt: now, UpdatedAt: now}
	if err := s.CreateWorkCard(ctx, card); err != nil {
		t.Fatalf("create card: %v", err)
	}
	event := domain.WorkCardEvent{ID: "attempt-1", CardID: card.ID, ProjectID: card.ProjectID, Kind: "hermes_answer_requested", Payload: `{}`, CreatedAt: now}
	if err := s.AppendWorkCardEvent(ctx, event); err != nil {
		t.Fatalf("seed event: %v", err)
	}
	if _, err := s.PrepareHermesAnswerAttempt(ctx, project.ID, project.Config.Workboard, event, true); err == nil {
		t.Fatal("PrepareHermesAnswerAttempt error = nil, want duplicate event failure")
	}
	gotProject, ok, err := s.GetProject(ctx, "mer")
	if err != nil || !ok || !gotProject.Config.Workboard.Autonomous.Enabled {
		t.Fatalf("project after rollback = %+v ok=%t err=%v", gotProject.Config.Workboard.Autonomous, ok, err)
	}
	events, err := s.ListWorkCardEvents(ctx, card.ID)
	if err != nil || len(events) != 1 || events[0].ID != event.ID {
		t.Fatalf("events after rollback = %+v err=%v", events, err)
	}
}

func TestPrepareHermesAnswerAttemptPreservesCurrentProjectConfig(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, time.July, 17, 9, 10, 0, 0, time.UTC)
	project := domain.ProjectRecord{
		ID: "mer", Path: "/tmp/mer", RegisteredAt: now,
		Config: domain.ProjectConfig{Workboard: domain.WorkboardConfig{Autonomous: domain.WorkboardAutonomousConfig{Enabled: true, Mode: "skip_timeout", Sticky: false}}},
	}
	if err := s.UpsertProject(ctx, project); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	card := domain.WorkCard{ID: "card-1", ProjectID: "mer", BoardID: "default", Title: "Ship API", Priority: domain.CardPriorityNormal, Labels: []string{}, Status: domain.CardStatusRunning, TargetPath: "/tmp/mer", Agent: "codex", GoalVersion: 1, CreatedAt: now, UpdatedAt: now}
	if err := s.CreateWorkCard(ctx, card); err != nil {
		t.Fatalf("create card: %v", err)
	}

	// Simulate SetConfig completing after reconciliation read project but before
	// it writes the one-shot consumption. That write must survive.
	updated := project
	updated.Config.DefaultBranch = "release"
	if err := s.UpsertProject(ctx, updated); err != nil {
		t.Fatalf("concurrent SetConfig: %v", err)
	}
	event := domain.WorkCardEvent{ID: "attempt-1", CardID: card.ID, ProjectID: card.ProjectID, Kind: "hermes_answer_requested", Payload: `{}`, CreatedAt: now}
	prepared, err := s.PrepareHermesAnswerAttempt(ctx, project.ID, project.Config.Workboard, event, true)
	if err != nil || !prepared {
		t.Fatalf("PrepareHermesAnswerAttempt: prepared=%t err=%v", prepared, err)
	}
	got, ok, err := s.GetProject(ctx, project.ID)
	if err != nil || !ok {
		t.Fatalf("get project: ok=%t err=%v", ok, err)
	}
	if got.Config.DefaultBranch != "release" || got.Config.Workboard.Autonomous.Enabled {
		t.Fatalf("project config after prepare = %+v, want preserved config with one-shot consumed", got.Config)
	}
}

func TestPrepareHermesAnswerAttemptRejectsStaleOneShotAuthorization(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, time.July, 17, 9, 10, 0, 0, time.UTC)
	project := domain.ProjectRecord{
		ID: "mer", Path: "/tmp/mer", RegisteredAt: now,
		Config: domain.ProjectConfig{Workboard: domain.WorkboardConfig{Autonomous: domain.WorkboardAutonomousConfig{Enabled: true, Mode: "skip_timeout", Sticky: false}}},
	}
	if err := s.UpsertProject(ctx, project); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	card := domain.WorkCard{ID: "card-1", ProjectID: "mer", BoardID: "default", Title: "Ship API", Priority: domain.CardPriorityNormal, Labels: []string{}, Status: domain.CardStatusRunning, TargetPath: "/tmp/mer", Agent: "codex", GoalVersion: 1, CreatedAt: now, UpdatedAt: now}
	if err := s.CreateWorkCard(ctx, card); err != nil {
		t.Fatalf("create card: %v", err)
	}

	updated := project
	updated.Config.Workboard.Autonomous.Enabled = false
	updated.Config.DefaultBranch = "release"
	if err := s.UpsertProject(ctx, updated); err != nil {
		t.Fatalf("disable autonomous: %v", err)
	}
	event := domain.WorkCardEvent{ID: "attempt-1", CardID: card.ID, ProjectID: card.ProjectID, Kind: "hermes_answer_requested", Payload: `{}`, CreatedAt: now}
	prepared, err := s.PrepareHermesAnswerAttempt(ctx, project.ID, project.Config.Workboard, event, true)
	if err != nil || prepared {
		t.Fatalf("PrepareHermesAnswerAttempt: prepared=%t err=%v, want stale authorization rejection", prepared, err)
	}
	events, err := s.ListWorkCardEvents(ctx, card.ID)
	if err != nil || len(events) != 0 {
		t.Fatalf("events after stale authorization = %+v err=%v, want none", events, err)
	}
	got, ok, err := s.GetProject(ctx, project.ID)
	if err != nil || !ok || got.Config.DefaultBranch != "release" || got.Config.Workboard.Autonomous.Enabled {
		t.Fatalf("project after stale authorization = %+v ok=%t err=%v", got.Config, ok, err)
	}
}

func TestPrepareHermesAnswerAttemptRejectsStaleNormalTimeoutAuthorization(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, time.July, 17, 9, 10, 0, 0, time.UTC)
	project := domain.ProjectRecord{
		ID: "mer", Path: "/tmp/mer", RegisteredAt: now,
		Config: domain.ProjectConfig{Workboard: domain.WorkboardConfig{AnswerTimeoutMinutes: 10}},
	}
	if err := s.UpsertProject(ctx, project); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	card := domain.WorkCard{ID: "card-1", ProjectID: "mer", BoardID: "default", Title: "Ship API", Priority: domain.CardPriorityNormal, Labels: []string{}, Status: domain.CardStatusRunning, TargetPath: "/tmp/mer", Agent: "codex", GoalVersion: 1, CreatedAt: now, UpdatedAt: now}
	if err := s.CreateWorkCard(ctx, card); err != nil {
		t.Fatalf("create card: %v", err)
	}

	updated := project
	updated.Config.Workboard.AnswerDenylist = []string{"test suite"}
	if err := s.UpsertProject(ctx, updated); err != nil {
		t.Fatalf("add denylist entry: %v", err)
	}
	event := domain.WorkCardEvent{ID: "attempt-1", CardID: card.ID, ProjectID: card.ProjectID, Kind: "hermes_answer_requested", Payload: `{}`, CreatedAt: now}
	prepared, err := s.PrepareHermesAnswerAttempt(ctx, project.ID, project.Config.Workboard, event, false)
	if err != nil || prepared {
		t.Fatalf("PrepareHermesAnswerAttempt: prepared=%t err=%v, want stale authorization rejection", prepared, err)
	}
	events, err := s.ListWorkCardEvents(ctx, card.ID)
	if err != nil || len(events) != 0 {
		t.Fatalf("events after stale authorization = %+v err=%v, want none", events, err)
	}
}
