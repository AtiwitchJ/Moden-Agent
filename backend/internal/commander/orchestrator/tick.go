package orchestrator

import (
	"context"
	"fmt"
	"time"

	"github.com/modernagent/modern-agent/backend/internal/commander"
	"github.com/modernagent/modern-agent/backend/internal/domain"
)

// tickActiveCards ensures every active-phase card has a live session.
func (o *ConfiguredOrchestrator) tickActiveCards(ctx context.Context, cards []domain.WorkCard) error {
	for _, card := range cards {
		if !isActivePhase(card.Status) {
			continue
		}

		active, session, err := o.checkActiveSession(ctx, card)
		if err != nil {
			return err
		}

		activeCount, err := o.countActiveCards(ctx, card.ProjectID)
		if err != nil {
			return fmt.Errorf("count active cards for %s: %w", card.ProjectID, err)
		}
		if activeCount >= o.wipLimit {
			continue
		}

		switch card.Status {
		case domain.CardStatusRunning:
			if !active {
				if err := o.spawnCodingSession(ctx, card, nil); err != nil {
					return err
				}
			} else if !o.isSessionLive(session, 30*time.Minute) {
				o.killSupersededSession(ctx, session)
				if err := o.spawnCodingSession(ctx, card, nil); err != nil {
					return err
				}
			}

		case domain.CardStatusReview:
			if !active {
				if err := o.spawnReviewSession(ctx, card); err != nil {
					return err
				}
			} else if !o.isSessionLive(session, 10*time.Minute) {
				o.killSupersededSession(ctx, session)
				if err := o.spawnReviewSession(ctx, card); err != nil {
					return err
				}
			}

		case domain.CardStatusTesting:
			if !active {
				if err := o.spawnTestingSession(ctx, card); err != nil {
					return err
				}
			} else if !o.isSessionLive(session, 10*time.Minute) {
				o.killSupersededSession(ctx, session)
				if err := o.spawnTestingSession(ctx, card); err != nil {
					return err
				}
			}

		case domain.CardStatusRedo:
			if !active {
				cycles, err := o.store.ListRedoCycles(ctx, card.ID)
				if err != nil {
					return fmt.Errorf("list redo cycles for %s: %w", card.ID, err)
				}
				var cycle *domain.RedoCycle
				if len(cycles) > 0 {
					cycle = &cycles[len(cycles)-1]
				}
				if err := o.spawnCodingSession(ctx, card, cycle); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// isActivePhase returns true for phases that count toward active WIP.
func isActivePhase(status domain.CardStatus) bool {
	return status == domain.CardStatusRunning ||
		status == domain.CardStatusReview ||
		status == domain.CardStatusTesting ||
		status == domain.CardStatusRedo
}

// checkActiveSession returns (hasActive, sessionRecord, error).
func (o *ConfiguredOrchestrator) checkActiveSession(ctx context.Context, card domain.WorkCard) (bool, ActiveSessionRecord, error) {
	session, ok, err := o.store.GetActiveSession(ctx, card.ID)
	if err != nil {
		return false, ActiveSessionRecord{}, err
	}
	if !ok {
		return false, ActiveSessionRecord{}, nil
	}
	return true, session, nil
}

// isSessionLive checks whether the active session is still within its
// phase-appropriate timeout. active_session.card_id uniquely identifies the
// row (it is the table's primary key), so GetActiveSession(ctx, card.ID)
// already scopes to the right session — there is nothing to cross-check
// against domain.WorkCard.SessionID, which tracks a different spawn system's
// (dispatch.go's initial auto-dispatch) session, not the orchestrator's own
// per-phase spawns.
func (o *ConfiguredOrchestrator) isSessionLive(session ActiveSessionRecord, timeout time.Duration) bool {
	return o.clock().Sub(session.CreatedAt) <= timeout
}

// killSupersededSession best-effort stops the session a replacement spawn is
// about to replace. Nil killer or an already-gone session are both normal,
// harmless cases — this never returns an error to its caller.
func (o *ConfiguredOrchestrator) killSupersededSession(ctx context.Context, session ActiveSessionRecord) {
	if o.killer == nil || session.SessionID == "" {
		return
	}
	_, _ = o.killer.Kill(ctx, domain.SessionID(session.SessionID))
}

// countActiveCards returns the count of cards in active phases for a project.
func (o *ConfiguredOrchestrator) countActiveCards(ctx context.Context, projectID string) (int, error) {
	cards, err := o.store.ListWorkCards(ctx, projectID, defaultBoardID)
	if err != nil {
		return 0, err
	}
	active := 0
	for _, c := range cards {
		if isActivePhase(c.Status) {
			active++
		}
	}
	return active, nil
}

// spawnCodingSession spawns a coding agent for a card.
func (o *ConfiguredOrchestrator) spawnCodingSession(ctx context.Context, card domain.WorkCard, cycle *domain.RedoCycle) error {
	agent := o.pickNextAgent(card, commander.PhaseCoding, "")
	briefing, err := generateBriefing(card, commander.PhaseCoding, cycle)
	if err != nil {
		return fmt.Errorf("generate briefing for %s: %w", card.ID, err)
	}
	return o.spawnSession(ctx, card, commander.PhaseCoding, agent, briefing, cycle)
}

// spawnReviewSession spawns the next reviewer agent for a card.
func (o *ConfiguredOrchestrator) spawnReviewSession(ctx context.Context, card domain.WorkCard) error {
	agent := o.pickNextAgent(card, commander.PhaseReview, "")
	briefing, err := generateBriefing(card, commander.PhaseReview, nil)
	if err != nil {
		return fmt.Errorf("generate briefing for %s: %w", card.ID, err)
	}
	return o.spawnSession(ctx, card, commander.PhaseReview, agent, briefing, nil)
}

// spawnTestingSession spawns the next tester agent for a card.
func (o *ConfiguredOrchestrator) spawnTestingSession(ctx context.Context, card domain.WorkCard) error {
	agent := o.pickNextAgent(card, commander.PhaseTesting, "")
	briefing, err := generateBriefing(card, commander.PhaseTesting, nil)
	if err != nil {
		return fmt.Errorf("generate briefing for %s: %w", card.ID, err)
	}
	return o.spawnSession(ctx, card, commander.PhaseTesting, agent, briefing, nil)
}

// spawnSession is the common spawn helper used by tick and orphan recovery.
func (o *ConfiguredOrchestrator) spawnSession(ctx context.Context, card domain.WorkCard, phase commander.Phase, agent string, briefing string, cycle *domain.RedoCycle) error {
	spec := commander.SpawnSpec{
		CardID:       card.ID,
		ProjectID:    card.ProjectID,
		Phase:        phase,
		Agent:        agent,
		Briefing:     briefing,
		ParentCard:   &card,
		CycleHistory: cycle,
	}

	// spawner.Spawn already records the (card, session, phase, agent) fact in
	// active_session on success — active_session.card_id is a PRIMARY KEY, so
	// inserting it again here always fails and (before this fix) made every
	// tick believe the spawn never happened, triggering an unbounded respawn
	// loop. Do not re-insert.
	if _, err := o.spawner.Spawn(ctx, spec); err != nil {
		return fmt.Errorf("spawn %s agent %s for %s: %w", phase, agent, card.ID, err)
	}

	return nil
}
