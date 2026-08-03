package workboard

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modernagent/modern-agent/backend/internal/domain"
	"github.com/modernagent/modern-agent/backend/internal/httpd/apierr"
)

// workCardEventAppender is the optional durable capability behind agent
// reporting, matching the cardDeleter/workCardEventLister pattern in
// service.go. *sqlite.Store satisfies it.
type workCardEventAppender interface {
	AppendWorkCardEvent(ctx context.Context, event domain.WorkCardEvent) error
}

// activeSessionDeleter is the optional durable capability that clears the
// orchestrator's per-card session bookkeeping when a phase transition
// happens. commander/orchestrator's own OnAgentCompleted/OnAgentFailed
// already do this for the signal-driven path; RecordAgentEvent's
// agent_transition is the only other place a card's phase changes today
// (ValidateWorkflowTransition rejects every actor="user" move), so it must
// do the same or the next phase's spawn collides with the stale row and
// never recovers. *sqlite.Store satisfies it.
type activeSessionDeleter interface {
	DeleteActiveSession(ctx context.Context, cardID string) error
}

// agentEventKinds are the reports `ao workboard card ...` can send. The set is
// closed so an agent cannot invent audit kinds the board does not understand.
var agentEventKinds = map[string]bool{
	"agent_transition": true,
	"agent_verdict":    true,
	"agent_finding":    true,
	"test_result":      true,
	"agent_failed":     true,
}

// AgentEventInput is one report an agent makes against a card.
type AgentEventInput struct {
	Kind    string
	Payload string
}

// RecordAgentEvent appends an agent report to the card's audit trail and, for
// agent_transition, applies the requested status through the same validated
// write path every other move uses. The trail is written first so a rejected
// transition still leaves evidence the agent tried.
func (s *Service) RecordAgentEvent(ctx context.Context, cardID string, in AgentEventInput) (domain.WorkCard, error) {
	card, err := s.Get(ctx, cardID)
	if err != nil {
		return domain.WorkCard{}, err
	}
	kind := strings.TrimSpace(in.Kind)
	if !agentEventKinds[kind] {
		return domain.WorkCard{}, apierr.Invalid("WORK_CARD_EVENT_KIND_INVALID", "Unknown agent event kind", nil)
	}
	if in.Payload != "" && !json.Valid([]byte(in.Payload)) {
		return domain.WorkCard{}, apierr.Invalid("WORK_CARD_EVENT_PAYLOAD_INVALID", "Payload must be JSON", nil)
	}
	appender, ok := s.store.(workCardEventAppender)
	if !ok {
		return domain.WorkCard{}, apierr.Internal("WORK_CARD_EVENTS_UNAVAILABLE", "Work card events are unavailable")
	}
	if err := appender.AppendWorkCardEvent(ctx, domain.WorkCardEvent{
		ID:        s.newID(),
		CardID:    card.ID,
		ProjectID: card.ProjectID,
		Kind:      kind,
		Payload:   in.Payload,
		CreatedAt: s.clock().UTC(),
	}); err != nil {
		return domain.WorkCard{}, fmt.Errorf("append %s event for card %s: %w", kind, card.ID, err)
	}
	if kind != "agent_transition" {
		return card, nil
	}

	var body struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(in.Payload), &body); err != nil {
		return domain.WorkCard{}, apierr.Invalid("WORK_CARD_EVENT_PAYLOAD_INVALID", "agent_transition payload needs a status", nil)
	}
	next := domain.CardStatus(strings.TrimSpace(body.Status))
	if err := domain.ValidateWorkflowTransition(card.Status, next, "agent"); err != nil {
		return domain.WorkCard{}, apierr.Invalid("WORK_CARD_TRANSITION_INVALID", err.Error(), nil)
	}
	updated, err := s.Update(ctx, cardID, UpdateInput{Status: &next})
	if err != nil {
		return domain.WorkCard{}, err
	}
	if deleter, ok := s.store.(activeSessionDeleter); ok {
		if err := deleter.DeleteActiveSession(ctx, cardID); err != nil {
			return domain.WorkCard{}, fmt.Errorf("clear active session for card %s after transition to %s: %w", cardID, next, err)
		}
	}
	return updated, nil
}
