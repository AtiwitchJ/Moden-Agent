package orchestrator

import (
	"context"
	"fmt"
	"time"

	"github.com/modernagent/modern-agent/backend/internal/commander"
	"github.com/modernagent/modern-agent/backend/internal/domain"
)

const (
	// Orphan timeouts per phase.
	runningOrphanTimeout = 30 * time.Minute
	reviewTestingTimeout = 10 * time.Minute
)

// checkOrphans scans all active-phase cards and spawns replacement sessions
// for cards whose active_session row is older than the phase-specific timeout.
func (o *ConfiguredOrchestrator) checkOrphans(ctx context.Context, cards []domain.WorkCard) error {
	for _, card := range cards {
		if !isActivePhase(card.Status) {
			continue
		}

		active, session, err := o.checkActiveSession(ctx, card)
		if err != nil {
			return err
		}
		if !active {
			continue
		}

		timeout := timeoutForPhase(card.Status)
		if o.clock().Sub(session.CreatedAt) > timeout {
			cycles, err := o.store.ListRedoCycles(ctx, card.ID)
			if err != nil {
				return fmt.Errorf("list redo cycles for orphan %s: %w", card.ID, err)
			}
			var cycle *domain.RedoCycle
			if len(cycles) > 0 {
				cycle = &cycles[len(cycles)-1]
			}
			if err := o.spawnOrphanReplacement(ctx, card, cycle); err != nil {
				return err
			}
		}
	}
	return nil
}

// timeoutForPhase returns the orphan timeout for a given card status.
func timeoutForPhase(status domain.CardStatus) time.Duration {
	switch status {
	case domain.CardStatusRunning:
		return runningOrphanTimeout
	case domain.CardStatusReview, domain.CardStatusTesting:
		return reviewTestingTimeout
	default:
		return runningOrphanTimeout
	}
}

// spawnOrphanReplacement spawns a replacement session for an orphaned card.
func (o *ConfiguredOrchestrator) spawnOrphanReplacement(ctx context.Context, card domain.WorkCard, cycle *domain.RedoCycle) error {
	var phase commander.Phase
	var agent string
	var briefing string

	switch card.Status {
	case domain.CardStatusRunning:
		phase = commander.PhaseCoding
		agent = o.pickNextAgent(card, commander.PhaseCoding, "")
		briefing, _ = generateBriefing(card, commander.PhaseCoding, cycle)
	case domain.CardStatusReview:
		phase = commander.PhaseReview
		agent = o.pickNextAgent(card, commander.PhaseReview, "")
		briefing, _ = generateBriefing(card, commander.PhaseReview, nil)
	case domain.CardStatusTesting:
		phase = commander.PhaseTesting
		agent = o.pickNextAgent(card, commander.PhaseTesting, "")
		briefing, _ = generateBriefing(card, commander.PhaseTesting, nil)
	default:
		return nil
	}

	if err := o.store.DeleteActiveSession(ctx, card.ID); err != nil {
		return fmt.Errorf("delete stale session for orphan %s: %w", card.ID, err)
	}

	if err := o.appendEvent(ctx, card, "orphan_detected", map[string]any{
		"phase":  phase,
		"agent":  agent,
		"reason": "session timeout",
	}); err != nil {
		return fmt.Errorf("append orphan_detected event for %s: %w", card.ID, err)
	}

	return o.spawnSession(ctx, card, phase, agent, briefing, cycle)
}
