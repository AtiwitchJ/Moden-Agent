package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/modernagent/modern-agent/backend/internal/commander"
	"github.com/modernagent/modern-agent/backend/internal/commander/spawner"
	"github.com/modernagent/modern-agent/backend/internal/domain"
)

const defaultBoardID = "default"

// ConfiguredOrchestrator wraps an Orchestrator with its dependencies.
type ConfiguredOrchestrator struct {
	spawner  commander.Spawner
	store    OrchestratorStore
	clock    func() time.Time
	newID    func() string
	wipLimit int
}

// OrchestratorStore is the durable surface required by the orchestrator tick
// loop. *sqlite.Store satisfies it structurally.
type OrchestratorStore interface {
	ListWorkCards(ctx context.Context, projectID, boardID string) ([]domain.WorkCard, error)
	GetWorkCard(ctx context.Context, id string) (domain.WorkCard, bool, error)
	GetActiveSession(ctx context.Context, cardID string) (ActiveSessionRecord, bool, error)
	InsertActiveSession(ctx context.Context, s spawner.InsertActiveSession) error
	DeleteActiveSession(ctx context.Context, cardID string) error
	UpdateWorkCard(ctx context.Context, card domain.WorkCard) error
	AppendWorkCardEvent(ctx context.Context, event domain.WorkCardEvent) error
	ListRedoCycles(ctx context.Context, cardID string) ([]domain.RedoCycle, error)
	ListSessions(ctx context.Context, projectID domain.ProjectID) ([]domain.SessionRecord, error)
}

// ActiveSessionRecord is the in-memory projection of an active_session row.
type ActiveSessionRecord struct {
	CardID    string
	SessionID string
	Phase     string
	Agent     string
	CreatedAt time.Time
}

// Config holds the dependencies and limits for a ConfiguredOrchestrator.
type Config struct {
	Spawner  commander.Spawner
	Store    OrchestratorStore
	Clock    func() time.Time
	NewID    func() string
	WIPLimit int
}

// New returns a configured orchestrator that implements commander.Orchestrator
// (Tick/OnAgentFailed/OnAgentCompleted/OnSignal) by delegating to the tick loop,
// fallback chain, and recovery logic.
func New(cfg Config) *ConfiguredOrchestrator {
	clock := cfg.Clock
	if clock == nil {
		clock = time.Now
	}
	newID := cfg.NewID
	if newID == nil {
		newID = func() string { return uuid.NewString() }
	}
	return &ConfiguredOrchestrator{
		spawner:  cfg.Spawner,
		store:    cfg.Store,
		clock:    clock,
		newID:    newID,
		wipLimit: cfg.WIPLimit,
	}
}

// Tick is called periodically (every 30s) and on every card state change event.
// It lists all cards in active phases (running, review, testing, redo) and
// ensures each has a live session.
func (o *ConfiguredOrchestrator) Tick(ctx context.Context, projectID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	cards, err := o.store.ListWorkCards(ctx, projectID, defaultBoardID)
	if err != nil {
		return fmt.Errorf("list work cards: %w", err)
	}
	return o.tickActiveCards(ctx, cards)
}

// OnAgentFailed is called when an agent session terminates with a non-zero exit
// or emits a failure signal.
func (o *ConfiguredOrchestrator) OnAgentFailed(ctx context.Context, cardID string, attempt commander.AgentAttempt) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	card, ok, err := o.getCard(ctx, cardID)
	if err != nil {
		return fmt.Errorf("get card %s: %w", cardID, err)
	}
	if !ok {
		return fmt.Errorf("card %s not found", cardID)
	}

	if err := o.store.DeleteActiveSession(ctx, cardID); err != nil {
		return fmt.Errorf("delete active session for %s: %w", cardID, err)
	}

	phase := commander.Phase(attempt.Phase)
	now := o.clock()

	cycles, err := o.store.ListRedoCycles(ctx, cardID)
	if err != nil {
		return fmt.Errorf("list redo cycles for %s: %w", cardID, err)
	}
	var cycle *domain.RedoCycle
	if len(cycles) > 0 {
		cycle = &cycles[len(cycles)-1]
	}

	nextAgent := o.pickNextAgent(card, phase, attempt.Agent)

	if nextAgent == "" {
		updatedCard, err := o.createRedoCycleAndTransition(ctx, card, phase, attempt, now)
		if err != nil {
			return err
		}
		card = updatedCard
		if err := o.store.UpdateWorkCard(ctx, card); err != nil {
			return fmt.Errorf("persist card %s after redo transition: %w", cardID, err)
		}
		return nil
	}

	if o.isLastAgent(card, phase, nextAgent) {
		if err := o.appendEvent(ctx, card, "agent_exhausted", map[string]any{
			"phase": phase, "agent": attempt.Agent, "next": nextAgent, "last": true,
		}); err != nil {
			return fmt.Errorf("append agent_exhausted event for %s: %w", cardID, err)
		}
		// Last agent failed with a fallback available — transition to redo.
		updatedCard, err := o.createRedoCycleAndTransition(ctx, card, phase, attempt, now)
		if err != nil {
			return err
		}
		card = updatedCard
		if err := o.store.UpdateWorkCard(ctx, card); err != nil {
			return fmt.Errorf("persist card %s after redo transition: %w", cardID, err)
		}
	} else {
		briefing, err := generateBriefing(card, phase, cycle)
		if err != nil {
			return fmt.Errorf("generate briefing for %s: %w", cardID, err)
		}

		spec := commander.SpawnSpec{
			CardID:       cardID,
			ProjectID:    card.ProjectID,
			Phase:        phase,
			Agent:        nextAgent,
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
			return fmt.Errorf("spawn fallback agent %s for %s: %w", nextAgent, cardID, err)
		}
	}

	return nil
}

// OnAgentCompleted is called when an agent reports success.
func (o *ConfiguredOrchestrator) OnAgentCompleted(ctx context.Context, cardID string, result commander.AgentResult) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	card, ok, err := o.getCard(ctx, cardID)
	if err != nil {
		return fmt.Errorf("get card %s: %w", cardID, err)
	}
	if !ok {
		return fmt.Errorf("card %s not found", cardID)
	}

	if err := o.store.DeleteActiveSession(ctx, cardID); err != nil {
		return fmt.Errorf("delete active session for %s: %w", cardID, err)
	}

	phase := commander.Phase(result.Phase)
	now := o.clock()

	switch phase {
	case commander.PhaseCoding:
		if result.Verdict != "approved" && result.Verdict != "pass" {
			return o.OnAgentFailed(ctx, cardID, commander.AgentAttempt{
				Phase:         phase,
				Agent:         card.CodingAgent,
				FailureReason: "inconclusive",
				AttemptNumber: 1,
			})
		}
		if err := domain.ValidateWorkflowTransition(card.Status, domain.CardStatusReview, "hermes-director"); err != nil {
			return fmt.Errorf("validate running→review for %s: %w", cardID, err)
		}
		card.Status = domain.CardStatusReview
		card.UpdatedAt = now
		if err := o.store.UpdateWorkCard(ctx, card); err != nil {
			return fmt.Errorf("update card %s to review: %w", cardID, err)
		}
		if err := o.appendEvent(ctx, card, "phase_completed", map[string]any{
			"from": "coding", "to": "review", "verdict": result.Verdict,
		}); err != nil {
			return fmt.Errorf("append phase_completed for %s: %w", cardID, err)
		}

	case commander.PhaseReview:
		if result.Verdict == "approved" {
			if err := domain.ValidateWorkflowTransition(card.Status, domain.CardStatusTesting, "hermes-director"); err != nil {
				return fmt.Errorf("validate review→testing for %s: %w", cardID, err)
			}
			card.Status = domain.CardStatusTesting
			card.UpdatedAt = now
			if err := o.store.UpdateWorkCard(ctx, card); err != nil {
				return fmt.Errorf("update card %s to testing: %w", cardID, err)
			}
			if err := o.appendEvent(ctx, card, "phase_completed", map[string]any{
				"from": "review", "to": "testing", "verdict": result.Verdict,
			}); err != nil {
				return fmt.Errorf("append phase_completed for %s: %w", cardID, err)
			}
		} else {
			return o.OnAgentFailed(ctx, cardID, commander.AgentAttempt{
				Phase:         phase,
				Agent:         card.ReviewerAgent,
				FailureReason: result.Verdict,
				AttemptNumber: 1,
			})
		}

	case commander.PhaseTesting:
		if result.Verdict == "pass" {
			if err := domain.ValidateWorkflowTransition(card.Status, domain.CardStatusDone, "hermes-director"); err != nil {
				return fmt.Errorf("validate testing→done for %s: %w", cardID, err)
			}
			card.Status = domain.CardStatusDone
			card.UpdatedAt = now
			if err := o.store.UpdateWorkCard(ctx, card); err != nil {
				return fmt.Errorf("update card %s to done: %w", cardID, err)
			}
			if err := o.appendEvent(ctx, card, "phase_completed", map[string]any{
				"from": "testing", "to": "done", "verdict": result.Verdict,
			}); err != nil {
				return fmt.Errorf("append phase_completed for %s: %w", cardID, err)
			}
		} else {
			return o.OnAgentFailed(ctx, cardID, commander.AgentAttempt{
				Phase:         phase,
				Agent:         card.TestingAgent,
				FailureReason: result.Verdict,
				AttemptNumber: 1,
			})
		}
	}

	return nil
}

// OnSignal handles external signals: PRReady, CIFailed, PRClosed.
func (o *ConfiguredOrchestrator) OnSignal(ctx context.Context, cardID string, signal commander.Signal) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	card, ok, err := o.getCard(ctx, cardID)
	if err != nil {
		return fmt.Errorf("get card %s: %w", cardID, err)
	}
	if !ok {
		return fmt.Errorf("card %s not found", cardID)
	}

	now := o.clock()
	switch signal.Kind {
	case "PRReady":
		if card.Status == domain.CardStatusReview {
			if err := domain.ValidateWorkflowTransition(card.Status, domain.CardStatusTesting, "hermes-director"); err != nil {
				return fmt.Errorf("validate review→testing on PRReady for %s: %w", cardID, err)
			}
			card.Status = domain.CardStatusTesting
			card.UpdatedAt = now
			if err := o.store.UpdateWorkCard(ctx, card); err != nil {
				return fmt.Errorf("update card %s to testing on PRReady: %w", cardID, err)
			}
		}

	case "CIFailed":
		if card.Status == domain.CardStatusTesting || card.Status == domain.CardStatusReview {
			return o.OnAgentFailed(ctx, cardID, commander.AgentAttempt{
				Phase:         commander.PhaseTesting,
				Agent:         card.TestingAgent,
				FailureReason: "ci_failed",
				AttemptNumber: 1,
			})
		}

	case "PRClosed":
		if card.Status == domain.CardStatusReview || card.Status == domain.CardStatusTesting {
			if err := domain.ValidateWorkflowTransition(card.Status, domain.CardStatusRedo, "hermes-director"); err != nil {
				return fmt.Errorf("validate redo transition on PRClosed for %s: %w", cardID, err)
			}
			card.Status = domain.CardStatusRedo
			card.UpdatedAt = now
			if err := o.store.UpdateWorkCard(ctx, card); err != nil {
				return fmt.Errorf("update card %s to redo on PRClosed: %w", cardID, err)
			}
		}
	}

	return nil
}

// createRedoCycleAndTransition creates a redo cycle for a card when all agents
// in a phase are exhausted. It returns the updated card (caller must persist).
func (o *ConfiguredOrchestrator) createRedoCycleAndTransition(ctx context.Context, card domain.WorkCard, phase commander.Phase, attempt commander.AgentAttempt, now time.Time) (domain.WorkCard, error) {
	cycles, err := o.store.ListRedoCycles(ctx, card.ID)
	if err != nil {
		return card, fmt.Errorf("list redo cycles for %s: %w", card.ID, err)
	}

	var cycle *domain.RedoCycle
	if len(cycles) > 0 {
		last := cycles[len(cycles)-1]
		last.CycleNumber++
		last.Summary = fmt.Sprintf("All agents failed in %s phase; entering redo cycle %d", phase, last.CycleNumber)
		cycle = &last
	} else {
		cycle = &domain.RedoCycle{
			ID:          o.newID(),
			CardID:      card.ID,
			CycleNumber: 1,
			Source:      fmt.Sprintf("%s agent exhausted", phase),
			Summary:     fmt.Sprintf("All agents failed in %s phase; entering redo", phase),
			CreatedAt:   now,
		}
	}

	card.Status = domain.CardStatusRedo
	card.RedoCount++
	card.UpdatedAt = now
	if err := o.appendEvent(ctx, card, "redo_started", map[string]any{
		"phase":         phase,
		"cycleNumber":   cycle.CycleNumber,
		"failureReason": attempt.FailureReason,
	}); err != nil {
		return card, fmt.Errorf("append redo_started for %s: %w", card.ID, err)
	}
	return card, nil
}

// getCard fetches a single card by ID from the store.
func (o *ConfiguredOrchestrator) getCard(ctx context.Context, cardID string) (domain.WorkCard, bool, error) {
	return o.store.GetWorkCard(ctx, cardID)
}

// appendEvent records a work card event.
func (o *ConfiguredOrchestrator) appendEvent(ctx context.Context, card domain.WorkCard, kind string, payload map[string]any) error {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	event := domain.WorkCardEvent{
		ID:        o.newID(),
		CardID:    card.ID,
		ProjectID: card.ProjectID,
		Kind:      kind,
		Payload:   string(payloadBytes),
		CreatedAt: o.clock(),
	}
	return o.store.AppendWorkCardEvent(ctx, event)
}
