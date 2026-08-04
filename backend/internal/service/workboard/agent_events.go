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

// AgentReporter routes a phase-scoped outcome to the orchestrator so a card
// actually advances (or falls back/redoes) once an agent reports on its
// phase. commander/orchestrator's ConfiguredOrchestrator satisfies it. Phase
// is one of "coding", "review", "testing"; verdict is "approved" or "pass" for
// success, any other value for a failure reason. Optional: when nil (e.g. a
// Director-driven project, which owns its own phase sequencing), reporting is
// skipped and only the audit trail is written.
type AgentReporter interface {
	ReportVerdict(ctx context.Context, cardID string, phase string, verdict string) error
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
	"agent_handoff":    true,
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
	var handoff Handoff
	if kind == "agent_handoff" {
		if err := json.Unmarshal([]byte(in.Payload), &handoff); err != nil ||
			(handoff.Phase != "coding" && handoff.Phase != "review" && handoff.Phase != "testing") ||
			strings.TrimSpace(handoff.Summary) == "" {
			return domain.WorkCard{}, apierr.Invalid("WORK_CARD_HANDOFF_INVALID", "Handoff needs a phase (coding, review, or testing) and summary", nil)
		}
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

	s.reportPhaseOutcome(ctx, card, kind, in.Payload, handoff)

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

// reportPhaseOutcome forwards a phase-completing or phase-failing agent
// report to the orchestrator so the card actually advances, falls back to the
// next agent, or enters redo. Without this, OnAgentCompleted/OnAgentFailed
// (commander/orchestrator) had the correct phase-advancement logic but no
// caller anywhere in the daemon, so a card sat wherever it was once its live
// session existed.
//
// Best-effort: a reporter error is swallowed, not returned. The audit event
// above is already durably recorded — that is this method's real contract —
// and the orchestrator's own Tick loop retries spawning on its next pass
// regardless, so failing the agent's own report over an unrelated spawn
// hiccup would only make its CLI call unreliable for no benefit.
func (s *Service) reportPhaseOutcome(ctx context.Context, card domain.WorkCard, kind, payload string, handoff Handoff) {
	if s.reporter == nil {
		return
	}
	var phase, verdict string
	switch kind {
	case "agent_handoff":
		// Only a coding handoff is itself a completion signal — see
		// generateBriefing (commander/orchestrator/briefing.go): review and
		// testing phases record a handoff and then separately report a
		// verdict or test result, so their handoff alone must not advance
		// the card.
		if handoff.Phase != "coding" {
			return
		}
		phase, verdict = "coding", "approved"

	case "agent_verdict":
		var body struct {
			Verdict string `json:"verdict"`
		}
		if err := json.Unmarshal([]byte(payload), &body); err != nil || body.Verdict == "" {
			return
		}
		p, ok := phaseForStatus(card.Status)
		if !ok {
			return
		}
		phase, verdict = p, body.Verdict

	case "test_result":
		var body struct {
			Exit int `json:"exit"`
		}
		if err := json.Unmarshal([]byte(payload), &body); err != nil {
			return
		}
		p, ok := phaseForStatus(card.Status)
		if !ok {
			return
		}
		phase = p
		if body.Exit == 0 {
			verdict = "pass"
		} else {
			verdict = "fail"
		}

	case "agent_failed":
		var body struct {
			Reason string `json:"reason"`
		}
		if err := json.Unmarshal([]byte(payload), &body); err != nil || body.Reason == "" {
			return
		}
		p, ok := phaseForStatus(card.Status)
		if !ok {
			return
		}
		phase, verdict = p, body.Reason

	default:
		return
	}
	if err := s.reporter.ReportVerdict(ctx, card.ID, phase, verdict); err != nil {
		// Best-effort by design (see the doc comment above), but silent
		// best-effort is a black hole: a card that should have advanced and
		// didn't leaves no trace anywhere else. Logging here is the only
		// record that this specific report was dropped and why.
		s.logger.Warn("reportPhaseOutcome: ReportVerdict failed", "cardID", card.ID, "kind", kind, "phase", phase, "verdict", verdict, "err", err)
	}
}

// phaseForStatus maps a card's current status onto the phase name an agent
// report is scoped to. Only the three active work phases apply; any other
// status (redo, blocked, done, …) means the card is not mid-phase, and a
// report arriving for it is a race — the orchestrator's own Tick already
// reconciles active-phase cards independently, so reportPhaseOutcome skips
// reporting rather than guessing a phase that no longer applies.
func phaseForStatus(status domain.CardStatus) (string, bool) {
	switch status {
	case domain.CardStatusRunning:
		return "coding", true
	case domain.CardStatusReview:
		return "review", true
	case domain.CardStatusTesting:
		return "testing", true
	default:
		return "", false
	}
}
