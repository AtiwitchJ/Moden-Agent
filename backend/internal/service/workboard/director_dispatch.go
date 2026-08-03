package workboard

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/modernagent/modern-agent/backend/internal/domain"
	"github.com/modernagent/modern-agent/backend/internal/ports"
)

// directorEnabled reports whether the project opted into AO's first-party
// Director harness. A different configured harness must not silently take the
// card-control role.
func directorEnabled(project domain.ProjectRecord) bool {
	return project.Config.Director.Harness == domain.HarnessDirector
}

// isProjectDirector reports whether a session is this project's live Director.
// Deliberately independent of isCardCommander: that predicate answers "may I
// coordinate with this session"; this one answers "is this project's Director
// already running", and the two must not drift into each other.
func isProjectDirector(session domain.SessionRecord, harness domain.AgentHarness) bool {
	return !session.IsTerminated && session.Kind == domain.KindOrchestrator && session.Harness == harness
}

// dispatchToDirector starts one Director against the next eligible card. The
// Director owns later phase transitions; dispatch performs the initial durable
// claim and links the card to the Director terminal.
func (d *Dispatcher) dispatchToDirector(ctx context.Context, project domain.ProjectRecord, cards []domain.WorkCard, now time.Time) ([]string, error) {
	harness := project.Config.Director.Harness

	sessions, err := d.store.ListSessions(ctx, domain.ProjectID(project.ID))
	if err != nil {
		return nil, fmt.Errorf("list sessions for project %s: %w", project.ID, err)
	}
	for _, session := range sessions {
		if isProjectDirector(session, harness) {
			return nil, nil // already running
		}
	}
	card, wasTodo, ok := nextDirectorCard(cards, now)
	if !ok {
		return nil, nil
	}
	if card.Status != domain.CardStatusReady {
		card.Status = domain.CardStatusReady
		card.ReadyAt = timePtr(now)
		card.UpdatedAt = now
		if err := d.store.UpdateWorkCard(ctx, card); err != nil {
			return nil, fmt.Errorf("ready Director card %s: %w", card.ID, err)
		}
	}
	wipLimit := project.Config.Workboard.WIPLimit
	if wipLimit <= 0 {
		wipLimit = domain.DefaultWorkboardConfig().WIPLimit
	}
	won, err := d.store.ClaimReadyWorkCard(ctx, card.ID, project.ID, wipLimit, now)
	if err != nil {
		return nil, fmt.Errorf("claim Director card %s: %w", card.ID, err)
	}
	if !won {
		if wasTodo {
			card.Status = domain.CardStatusTodo
			card.ReadyAt = nil
			card.UpdatedAt = now
			if err := d.store.UpdateWorkCard(ctx, card); err != nil {
				return nil, fmt.Errorf("release Director todo promotion for card %s: %w", card.ID, err)
			}
		}
		return nil, nil
	}

	// Spawn directly rather than through SpawnOrchestrator: that helper takes no
	// harness and resolves it from Config.Orchestrator.Harness, which is the
	// wrong field for the Director.
	session, err := d.spawner.Spawn(ctx, ports.SpawnConfig{
		ProjectID:      domain.ProjectID(project.ID),
		Kind:           domain.KindOrchestrator,
		Harness:        harness,
		Prompt:         fmt.Sprintf("Drive work card %s for project %s.", card.ID, project.ID),
		TargetPath:     card.TargetPath,
		DirectorCardID: card.ID,
	})
	if err != nil {
		if recoverErr := d.recoverCardFromSpawnFailure(ctx, card, wasTodo, false, dispatchFailedReasonSpawnFailed, err, now); recoverErr != nil {
			return nil, recoverErr
		}
		return nil, nil
	}
	card.Status = domain.CardStatusRunning
	card.SessionID = string(session.ID)
	card.UpdatedAt = now
	if err := d.store.UpdateWorkCard(ctx, card); err != nil {
		linkErr := fmt.Errorf("link Director session for card %s: %w", card.ID, err)
		persistenceCtx := context.WithoutCancel(ctx)
		if _, rollbackErr := d.rollbacker.RollbackSpawn(persistenceCtx, session.ID); rollbackErr != nil {
			return nil, fmt.Errorf("%w; rollback Director session %s: %v", linkErr, session.ID, rollbackErr)
		}
		if recoverErr := d.recoverCardFromSpawnFailure(persistenceCtx, card, wasTodo, false, dispatchFailedReasonSpawnFailed, linkErr, now); recoverErr != nil {
			return nil, recoverErr
		}
		return nil, nil
	}
	return []string{card.ID}, nil
}

// nextDirectorCard picks one queued card for the project's single Director.
// The Director's own loop owns the later phase transitions; dispatch only
// performs the durable Todo/Ready -> Running claim that starts that loop.
func nextDirectorCard(cards []domain.WorkCard, now time.Time) (domain.WorkCard, bool, bool) {
	candidates := make([]domain.WorkCard, 0, len(cards))
	wasTodo := make(map[string]bool, len(cards))
	for _, card := range cards {
		switch card.Status {
		case domain.CardStatusTodo:
			wasTodo[card.ID] = true
		case domain.CardStatusScheduled:
			if card.ScheduledAt == nil || card.ScheduledAt.After(now) {
				continue
			}
		case domain.CardStatusReady:
		default:
			continue
		}
		if !card.PausedRetarget {
			candidates = append(candidates, card)
		}
	}
	if len(candidates) == 0 {
		return domain.WorkCard{}, false, false
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Priority.Rank() != candidates[j].Priority.Rank() {
			return candidates[i].Priority.Rank() > candidates[j].Priority.Rank()
		}
		return candidates[i].ID < candidates[j].ID
	})
	card := candidates[0]
	return card, wasTodo[card.ID], true
}
