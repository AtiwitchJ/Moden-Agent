package workboard

import (
	"context"
	"fmt"
	"log/slog"
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
		d.logger.Info("director dispatch: branch=list_sessions_error", "project", project.ID, "err", err)
		return nil, fmt.Errorf("list sessions for project %s: %w", project.ID, err)
	}
	liveSessions := make(map[domain.SessionID]bool, len(sessions))
	for _, session := range sessions {
		if !session.IsTerminated {
			liveSessions[session.ID] = true
		}
	}
	for _, session := range sessions {
		if isProjectDirector(session, harness) {
			d.logger.Info("director dispatch: branch=director_already_running", "project", project.ID, "session", session.ID)
			return nil, nil // already running
		}
	}
	card, wasTodo, ok := nextDirectorCard(cards, liveSessions, now)
	if !ok {
		d.logger.Info("director dispatch: branch=no_eligible_card", "project", project.ID, "cardCount", len(cards))
		return nil, nil
	}
	if card.Status != domain.CardStatusReady {
		card.Status = domain.CardStatusReady
		card.ReadyAt = timePtr(now)
		card.SessionID = ""
		card.UpdatedAt = now
		if err := d.store.UpdateWorkCard(ctx, card); err != nil {
			d.logger.Info("director dispatch: branch=promote_ready_error", "project", project.ID, "card", card.ID, "err", err)
			return nil, fmt.Errorf("ready Director card %s: %w", card.ID, err)
		}
	}
	wipLimit := project.Config.Workboard.WIPLimit
	if wipLimit <= 0 {
		wipLimit = domain.DefaultWorkboardConfig().WIPLimit
	}
	won, err := d.store.ClaimReadyWorkCard(ctx, card.ID, project.ID, wipLimit, now)
	if err != nil {
		d.logger.Info("director dispatch: branch=claim_error", "project", project.ID, "card", card.ID, "wipLimit", wipLimit, "err", err)
		return nil, fmt.Errorf("claim Director card %s: %w", card.ID, err)
	}
	if !won {
		d.logger.Info("director dispatch: branch=claim_not_won", "project", project.ID, "card", card.ID, "wasTodo", wasTodo, "wipLimit", wipLimit)
		if wasTodo {
			card.Status = domain.CardStatusTodo
			card.ReadyAt = nil
			card.UpdatedAt = now
			if err := d.store.UpdateWorkCard(ctx, card); err != nil {
				d.logger.Info("director dispatch: branch=release_todo_error", "project", project.ID, "card", card.ID, "err", err)
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
		d.logger.Info("director dispatch: branch=spawn_error", "project", project.ID, "card", card.ID, "err", err)
		if recoverErr := d.recoverCardFromSpawnFailure(ctx, card, wasTodo, false, dispatchFailedReasonSpawnFailed, err, now); recoverErr != nil {
			d.logger.Info("director dispatch: branch=recover_error_after_spawn_error", "project", project.ID, "card", card.ID, "recoverErr", recoverErr)
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
			d.logger.Info("director dispatch: branch=link_error_rollback_error", "project", project.ID, "card", card.ID, "session", session.ID, "linkErr", linkErr, "rollbackErr", rollbackErr)
			return nil, fmt.Errorf("%w; rollback Director session %s: %v", linkErr, session.ID, rollbackErr)
		}
		if recoverErr := d.recoverCardFromSpawnFailure(persistenceCtx, card, wasTodo, false, dispatchFailedReasonSpawnFailed, linkErr, now); recoverErr != nil {
			d.logger.Info("director dispatch: branch=link_error_recover_error", "project", project.ID, "card", card.ID, "session", session.ID, "recoverErr", recoverErr)
			return nil, recoverErr
		}
		d.logger.Info("director dispatch: branch=link_error_recovered", "project", project.ID, "card", card.ID, "session", session.ID)
		return nil, nil
	}
	d.logger.Info("director dispatch: branch=spawned", "project", project.ID, "card", card.ID, "session", session.ID)
	return []string{card.ID}, nil
}

// nextDirectorCard picks one queued card for the project's single Director.
// The Director's own loop owns the later phase transitions; dispatch only
// performs the durable Todo/Ready -> Running claim that starts that loop.
//
// A card whose linked Director session is no longer live is reclaimed, no
// matter which phase the card is parked in. That covers two failure modes:
//   - A card durably status=running whose Director exited without unwinding
//     the claim (the prior fix; status was the only stale-Director signal).
//   - A card parked in a later phase (review, testing, redo, blocked) whose
//     Director's node process deadlocked — e.g. a stuck LLM provider promise
//     or a daemon-side fault that left the row marked is_terminated=1 while
//     the card was mid-flight. Without this path the card never advances and
//     the user has no automated recovery. The reclaim clears the stale
//     SessionID on the returned card so the caller can promote it to Ready
//     and let a fresh Director take ownership; the new Director re-derives
//     the live state from the workboard.
//
// A card whose linked session is still live is left alone in every phase —
// the live Director still owns it and a parallel spawn would race the
// transition the Director is mid-flight through.
func nextDirectorCard(cards []domain.WorkCard, liveSessions map[domain.SessionID]bool, now time.Time) (domain.WorkCard, bool, bool) {
	candidates := make([]domain.WorkCard, 0, len(cards))
	wasTodo := make(map[string]bool, len(cards))
	for _, card := range cards {
		diagLogger().Info("director dispatch: nextDirectorCard iter", "card", card.ID, "status", string(card.Status), "pausedRetarget", card.PausedRetarget, "sessionID", card.SessionID)
		switch card.Status {
		case domain.CardStatusTodo:
			wasTodo[card.ID] = true
		case domain.CardStatusScheduled:
			if card.ScheduledAt == nil || card.ScheduledAt.After(now) {
				continue
			}
		case domain.CardStatusReady:
		case domain.CardStatusRunning:
			if card.SessionID == "" || liveSessions[domain.SessionID(card.SessionID)] {
				continue
			}
			wasTodo[card.ID] = true
			card.SessionID = ""
		case domain.CardStatusReview, domain.CardStatusTesting, domain.CardStatusRedo, domain.CardStatusBlocked:
			// Later-phase reclaim: a stuck card whose Director session is no
			// longer live. Empty SessionID (defensive: should not happen in
			// these phases) is also reclaimable.
			if card.SessionID != "" && liveSessions[domain.SessionID(card.SessionID)] {
				continue
			}
			wasTodo[card.ID] = true
			card.SessionID = ""
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

// diagLogger is a process-wide default logger used by helpers that don't hold
// a Dispatcher reference. It defaults to slog.Default() and is overridden in
// tests to discard output.
var diagLogger = func() *slog.Logger { return slog.Default() }
