package workboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/modernagent/modern-agent/backend/internal/domain"
	"github.com/modernagent/modern-agent/backend/internal/ports"
	sessionsvc "github.com/modernagent/modern-agent/backend/internal/service/session"
)

// DispatchStore is the durable surface required to promote and claim cards.
// Workboard v1 has one board per project, so ListWorkCards uses defaultBoardID
// for both candidate selection and the project-wide running-card count.
type DispatchStore interface {
	GetProject(ctx context.Context, id string) (domain.ProjectRecord, bool, error)
	ListWorkCards(ctx context.Context, projectID, boardID string) ([]domain.WorkCard, error)
	ListSessions(ctx context.Context, projectID domain.ProjectID) ([]domain.SessionRecord, error)
	UpdateWorkCard(ctx context.Context, card domain.WorkCard) error
	ClaimReadyWorkCard(ctx context.Context, cardID, projectID string, wipLimit int, at time.Time) (bool, error)
}

// WorkerSpawner starts a worker through the existing session-service boundary.
type WorkerSpawner interface {
	Spawn(ctx context.Context, cfg ports.SpawnConfig) (domain.Session, error)
}

// OrchestratorSpawner is the established session-service operation used by
// Hermes-commanded cards. It reuses the project's one live orchestrator.
type OrchestratorSpawner interface {
	SpawnOrchestrator(ctx context.Context, projectID domain.ProjectID, clean bool, prompt string) (domain.Session, error)
}

// SpawnRollbacker compensates a successful spawn when the card cannot durably
// link to it. Keeping this separate from WorkerSpawner keeps the dispatcher
// dependent on only the recovery operation it needs.
type SpawnRollbacker interface {
	RollbackSpawn(ctx context.Context, id domain.SessionID) (sessionsvc.RollbackOutcome, error)
}

// DispatchDeps configures a Dispatcher.
type DispatchDeps struct {
	Store      DispatchStore
	Spawner    WorkerSpawner
	Rollbacker SpawnRollbacker
	Clock      func() time.Time
}

// Dispatcher promotes due cards and claims ready cards under a project's WIP
// limit. It stores only card facts; session status and runtime liveness remain
// owned by their existing services.
type Dispatcher struct {
	store      DispatchStore
	spawner    WorkerSpawner
	rollbacker SpawnRollbacker
	clock      func() time.Time
}

// NewDispatcher constructs a workboard dispatcher.
func NewDispatcher(d DispatchDeps) *Dispatcher {
	clock := d.Clock
	if clock == nil {
		clock = time.Now
	}
	rollbacker := d.Rollbacker
	if rollbacker == nil {
		rollbacker, _ = d.Spawner.(SpawnRollbacker)
	}
	return &Dispatcher{store: d.Store, spawner: d.Spawner, rollbacker: rollbacker, clock: clock}
}

// DispatchOnce promotes todo and due scheduled cards, then claims ready cards
// in priority/FIFO order. Before starting a worker, it atomically writes a
// durable running claim only if the project is still below its WIP limit, so
// independent dispatcher instances cannot over-claim the same project.
func (d *Dispatcher) DispatchOnce(ctx context.Context, projectID string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if d.store == nil || d.spawner == nil {
		return nil, nil
	}
	project, ok, err := d.store.GetProject(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("get project %s: %w", projectID, err)
	}
	if !ok {
		return nil, fmt.Errorf("project %s not found", projectID)
	}
	commanding := project.Config.Orchestrator.Harness == domain.HarnessHermes
	if !commanding && d.rollbacker == nil {
		return nil, fmt.Errorf("workboard dispatcher requires spawn rollback support")
	}
	var orchestrator OrchestratorSpawner
	if commanding {
		var supported bool
		orchestrator, supported = d.spawner.(OrchestratorSpawner)
		if !supported {
			return nil, fmt.Errorf("workboard dispatcher requires orchestrator spawn support for Hermes")
		}
	}
	cards, err := d.store.ListWorkCards(ctx, projectID, defaultBoardID)
	if err != nil {
		return nil, fmt.Errorf("list work cards for project %s: %w", projectID, err)
	}

	now := d.clock().UTC()
	for i := range cards {
		card := &cards[i]
		switch card.Status {
		case domain.CardStatusTodo:
			// Todo is the normal auto-start queue: promote it in this same
			// dispatch pass so it can claim a worker immediately.
		case domain.CardStatusScheduled:
			if card.ScheduledAt == nil || card.ScheduledAt.After(now) {
				continue
			}
		default:
			continue
		}
		card.Status = domain.CardStatusReady
		card.ReadyAt = timePtr(now)
		card.UpdatedAt = now
		if err := d.store.UpdateWorkCard(ctx, *card); err != nil {
			return nil, fmt.Errorf("promote scheduled card %s: %w", card.ID, err)
		}
	}

	wipLimit := project.Config.Workboard.WIPLimit
	if wipLimit <= 0 {
		wipLimit = domain.DefaultWorkboardConfig().WIPLimit
	}
	candidates := make([]domain.WorkCard, 0, len(cards))
	for _, card := range cards {
		if card.Status == domain.CardStatusReady && !card.PausedRetarget {
			candidates = append(candidates, card)
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		left, right := candidates[i], candidates[j]
		if left.Priority.Rank() != right.Priority.Rank() {
			return left.Priority.Rank() > right.Priority.Rank()
		}
		leftReady, rightReady := cardReadyAt(left), cardReadyAt(right)
		if !leftReady.Equal(rightReady) {
			return leftReady.Before(rightReady)
		}
		return left.ID < right.ID
	})

	var claimed []string
	for _, card := range candidates {
		won, err := d.store.ClaimReadyWorkCard(ctx, card.ID, projectID, wipLimit, now)
		if err != nil {
			return claimed, fmt.Errorf("persist dispatch claim for card %s: %w", card.ID, err)
		}
		if !won {
			continue
		}
		card.Status = domain.CardStatusRunning
		card.SessionID = ""
		card.UpdatedAt = now

		var session domain.Session
		if commanding {
			active, activeErr := d.store.ListSessions(ctx, domain.ProjectID(projectID))
			if activeErr != nil {
				err = fmt.Errorf("list orchestrators for card %s: %w", card.ID, activeErr)
			} else if hasActiveNonHermesOrchestrator(active) {
				err = fmt.Errorf("project %s has a non-Hermes active orchestrator", projectID)
			} else {
				briefing, briefErr := hermesCardBriefing(card)
				if briefErr != nil {
					return claimed, briefErr
				}
				session, err = orchestrator.SpawnOrchestrator(ctx, domain.ProjectID(projectID), false, briefing)
			}
			if err == nil && (session.Kind != domain.KindOrchestrator || session.Harness != domain.HarnessHermes) {
				err = fmt.Errorf("project %s has a non-Hermes active orchestrator", projectID)
			}
		} else {
			session, err = d.spawner.Spawn(ctx, ports.SpawnConfig{
				ProjectID:  domain.ProjectID(projectID),
				Kind:       domain.KindWorker,
				Harness:    domain.AgentHarness(card.Agent),
				Prompt:     card.Title + "\n\n" + card.Notes,
				TargetPath: card.TargetPath,
			})
		}
		if err != nil {
			spawnErr := fmt.Errorf("start %s for card %s: %w", dispatchRole(commanding), card.ID, err)
			card.Status = domain.CardStatusReady
			card.SessionID = ""
			card.UpdatedAt = now
			if releaseErr := d.store.UpdateWorkCard(context.WithoutCancel(ctx), card); releaseErr != nil {
				return claimed, errors.Join(
					spawnErr,
					fmt.Errorf("release dispatch claim for card %s: %w", card.ID, releaseErr),
					fmt.Errorf("card %s remains durably claimed as running without a session ID", card.ID),
				)
			}
			return claimed, spawnErr
		}

		card.SessionID = string(session.ID)
		card.UpdatedAt = now
		if err := d.store.UpdateWorkCard(ctx, card); err != nil {
			linkErr := fmt.Errorf("link worker session for card %s: %w", card.ID, err)
			if commanding {
				card.Status = domain.CardStatusReady
				card.SessionID = ""
				card.UpdatedAt = now
				if releaseErr := d.store.UpdateWorkCard(context.WithoutCancel(ctx), card); releaseErr != nil {
					return claimed, errors.Join(linkErr, fmt.Errorf("release Hermes dispatch claim for card %s: %w", card.ID, releaseErr))
				}
				return claimed, linkErr
			}
			persistenceCtx := context.WithoutCancel(ctx)
			if _, rollbackErr := d.rollbacker.RollbackSpawn(persistenceCtx, session.ID); rollbackErr != nil {
				return claimed, errors.Join(
					linkErr,
					fmt.Errorf("rollback session %s: %w", session.ID, rollbackErr),
					fmt.Errorf("card %s remains durably claimed as running without a session ID", card.ID),
				)
			}
			card.Status = domain.CardStatusReady
			card.SessionID = ""
			card.UpdatedAt = now
			if releaseErr := d.store.UpdateWorkCard(persistenceCtx, card); releaseErr != nil {
				return claimed, errors.Join(
					linkErr,
					fmt.Errorf("release dispatch claim for card %s after rollback: %w", card.ID, releaseErr),
					fmt.Errorf("card %s remains durably claimed as running without a session ID", card.ID),
				)
			}
			return claimed, linkErr
		}
		claimed = append(claimed, card.ID)
	}
	return claimed, nil
}

func hasActiveNonHermesOrchestrator(sessions []domain.SessionRecord) bool {
	for _, session := range sessions {
		if !session.IsTerminated && session.Kind == domain.KindOrchestrator && session.Harness != domain.HarnessHermes {
			return true
		}
	}
	return false
}

func dispatchRole(commanding bool) string {
	if commanding {
		return "Hermes commander"
	}
	return "worker"
}

// hermesCardBriefing is intentionally a complete, machine-readable snapshot.
// Hermes must refresh the card through the CLI before it changes its plan.
func hermesCardBriefing(card domain.WorkCard) (string, error) {
	payload, err := json.Marshal(struct {
		CardID        string              `json:"cardId"`
		ProjectID     string              `json:"projectId"`
		Title         string              `json:"title"`
		Notes         string              `json:"notes"`
		Priority      domain.CardPriority `json:"priority"`
		Labels        []string            `json:"labels"`
		TargetPath    string              `json:"targetPath"`
		CodingAgent   string              `json:"codingAgent"`
		ReviewerMode  string              `json:"reviewerMode"`
		ReviewerAgent string              `json:"reviewerAgent"`
		TestingAgent  string              `json:"testingAgent"`
		GoalVersion   int                 `json:"goalVersion"`
	}{
		CardID: card.ID, ProjectID: card.ProjectID, Title: card.Title, Notes: card.Notes,
		Priority: card.Priority, Labels: card.Labels, TargetPath: card.TargetPath,
		CodingAgent: firstNonEmpty(card.CodingAgent, card.Agent), ReviewerMode: card.ReviewerMode,
		ReviewerAgent: card.ReviewerAgent, TestingAgent: card.TestingAgent, GoalVersion: card.GoalVersion,
	})
	if err != nil {
		return "", fmt.Errorf("marshal Hermes briefing for card %s: %w", card.ID, err)
	}
	return "AO work-card command briefing. You own this card. Refresh it with `ao workboard get " + card.ID + " --json` before acting, then coordinate workers according to your standing instructions.\n\n" + string(payload), nil
}

func cardReadyAt(card domain.WorkCard) time.Time {
	if card.ReadyAt != nil {
		return *card.ReadyAt
	}
	return card.CreatedAt
}

func timePtr(t time.Time) *time.Time { return &t }
