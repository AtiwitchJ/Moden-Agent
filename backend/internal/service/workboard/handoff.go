package workboard

import (
	"context"
	"encoding/json"

	"github.com/modernagent/modern-agent/backend/internal/domain"
	"github.com/modernagent/modern-agent/backend/internal/httpd/apierr"
)

// Handoff is the durable, concise context one phase leaves for the next.
// It deliberately stores references and results rather than terminal output.
type Handoff struct {
	Phase        string   `json:"phase"`
	Summary      string   `json:"summary"`
	ChangedFiles []string `json:"changedFiles,omitempty"`
	Checks       []string `json:"checks,omitempty"`
	Commit       string   `json:"commit,omitempty"`
	Next         string   `json:"next,omitempty"`
}

// Handoffs returns the valid handoff reports for a card in chronological order.
// A malformed legacy event is ignored so one bad audit row never prevents an
// agent from reading the card that it needs to recover.
func (s *Service) Handoffs(ctx context.Context, cardID string) ([]Handoff, error) {
	card, err := s.Get(ctx, cardID)
	if err != nil {
		return nil, err
	}
	lister, ok := s.store.(workCardEventLister)
	if !ok {
		return nil, apierr.Internal("WORK_CARD_EVENTS_UNAVAILABLE", "Work card events are unavailable")
	}
	events, err := lister.ListWorkCardEvents(ctx, card.ID)
	if err != nil {
		return nil, apierr.Internal("WORK_CARD_EVENTS_LOAD_FAILED", "Failed to load work card events")
	}
	handoffs := make([]Handoff, 0)
	for _, event := range events {
		if event.Kind != "agent_handoff" {
			continue
		}
		var handoff Handoff
		if json.Unmarshal([]byte(event.Payload), &handoff) != nil || handoff.Phase == "" || handoff.Summary == "" {
			continue
		}
		handoffs = append(handoffs, handoff)
	}
	return handoffs, nil
}

// StatusReason returns the reason recorded for the card's current status —
// the agent's own explanation for why it moved the card here, most useful on
// a blocked card a human has to act on. It is the most recent agent_transition
// event's reason, but only if that event's status still matches the card's
// current status; a status changed by any other route (or a legacy event
// recorded before this field existed) reports no reason rather than an
// unrelated, stale one.
func (s *Service) StatusReason(ctx context.Context, cardID string) (string, error) {
	card, err := s.Get(ctx, cardID)
	if err != nil {
		return "", err
	}
	lister, ok := s.store.(workCardEventLister)
	if !ok {
		return "", apierr.Internal("WORK_CARD_EVENTS_UNAVAILABLE", "Work card events are unavailable")
	}
	events, err := lister.ListWorkCardEvents(ctx, card.ID)
	if err != nil {
		return "", apierr.Internal("WORK_CARD_EVENTS_LOAD_FAILED", "Failed to load work card events")
	}
	var lastStatus, lastReason string
	for _, event := range events {
		if event.Kind != "agent_transition" {
			continue
		}
		var body struct {
			Status string `json:"status"`
			Reason string `json:"reason"`
		}
		if json.Unmarshal([]byte(event.Payload), &body) != nil {
			continue
		}
		lastStatus, lastReason = body.Status, body.Reason
	}
	if domain.CardStatus(lastStatus) != card.Status {
		return "", nil
	}
	return lastReason, nil
}
