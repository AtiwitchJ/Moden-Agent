package daemon

import (
	"context"
	"time"

	"github.com/modernagent/modern-agent/backend/internal/commander/orchestrator"
	"github.com/modernagent/modern-agent/backend/internal/commander/spawner"
	"github.com/modernagent/modern-agent/backend/internal/domain"
	"github.com/modernagent/modern-agent/backend/internal/storage/sqlite"
)

// orchestratorStoreAdapter adapts *sqlite.Store's plain row types to the
// named types commander/orchestrator.OrchestratorStore and
// commander/spawner.ActiveSessionStore require, so the storage package never
// has to import the commander package tree (Global Constraints).
type orchestratorStoreAdapter struct {
	store *sqlite.Store
}

func (a orchestratorStoreAdapter) ListWorkCards(ctx context.Context, projectID, boardID string) ([]domain.WorkCard, error) {
	return a.store.ListWorkCards(ctx, projectID, boardID)
}

func (a orchestratorStoreAdapter) GetWorkCard(ctx context.Context, id string) (domain.WorkCard, bool, error) {
	return a.store.GetWorkCard(ctx, id)
}

func (a orchestratorStoreAdapter) GetActiveSession(ctx context.Context, cardID string) (orchestrator.ActiveSessionRecord, bool, error) {
	row, ok, err := a.store.GetActiveSession(ctx, cardID)
	if err != nil || !ok {
		return orchestrator.ActiveSessionRecord{}, ok, err
	}
	return orchestrator.ActiveSessionRecord{
		CardID:    row.CardID,
		SessionID: row.SessionID,
		Phase:     row.Phase,
		Agent:     row.Agent,
		CreatedAt: row.CreatedAt,
	}, true, nil
}

func (a orchestratorStoreAdapter) InsertActiveSession(ctx context.Context, s spawner.InsertActiveSession) error {
	return a.store.InsertActiveSession(ctx, s.CardID, s.SessionID, string(s.Phase), s.Agent, time.Now())
}

func (a orchestratorStoreAdapter) DeleteActiveSession(ctx context.Context, cardID string) error {
	return a.store.DeleteActiveSession(ctx, cardID)
}

func (a orchestratorStoreAdapter) UpdateWorkCard(ctx context.Context, card domain.WorkCard) error {
	return a.store.UpdateWorkCard(ctx, card)
}

func (a orchestratorStoreAdapter) AppendWorkCardEvent(ctx context.Context, event domain.WorkCardEvent) error {
	return a.store.AppendWorkCardEvent(ctx, event)
}

func (a orchestratorStoreAdapter) ListRedoCycles(ctx context.Context, cardID string) ([]domain.RedoCycle, error) {
	return a.store.ListRedoCycles(ctx, cardID)
}

func (a orchestratorStoreAdapter) InsertRedoCycle(ctx context.Context, cycle domain.RedoCycle) error {
	return a.store.InsertRedoCycle(ctx, cycle)
}

func (a orchestratorStoreAdapter) ListSessions(ctx context.Context, projectID domain.ProjectID) ([]domain.SessionRecord, error) {
	return a.store.ListSessions(ctx, projectID)
}

func (a orchestratorStoreAdapter) GetProject(ctx context.Context, id string) (domain.ProjectRecord, bool, error) {
	return a.store.GetProject(ctx, id)
}
