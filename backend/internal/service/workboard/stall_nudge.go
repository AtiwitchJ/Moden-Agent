package workboard

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/modernagent/modern-agent/backend/internal/domain"
)

const (
	workCardEventStallNudged = "stall_nudged"

	// ponytail: fixed thresholds; promote to WorkboardConfig knobs if real
	// projects need different pacing.
	stallNudgeIdleThreshold = 10 * time.Minute
	stallNudgeCooldown      = 15 * time.Minute
)

// StallNudgeStore is the narrow durable surface the nudger reads and audits
// through. *sqlite.Store satisfies it structurally.
type StallNudgeStore interface {
	ListWorkCards(ctx context.Context, projectID, boardID string) ([]domain.WorkCard, error)
	ListSessions(ctx context.Context, projectID domain.ProjectID) ([]domain.SessionRecord, error)
	ListWorkCardEvents(ctx context.Context, cardID string) ([]domain.WorkCardEvent, error)
	AppendWorkCardEvent(ctx context.Context, event domain.WorkCardEvent) error
}

// StallNudgeDeps configures a StallNudger.
type StallNudgeDeps struct {
	Store  StallNudgeStore
	Sender HermesSender
	Clock  func() time.Time
	NewID  func() string
}

// StallNudger watches cards in active phases (running/review/testing) whose
// linked commander session has gone idle past a threshold, and sends the
// session a reminder to re-read the card and advance or block it. It never
// kills or respawns anything; ActivityWaitingInput is sticky and never
// nudged, ActivityActive means the session is working and is left alone.
// This is the minimal safety net until the full orchestrator tick loop
// (hermes-director-orchestrator plan, Task 6) ships.
type StallNudger struct {
	store  StallNudgeStore
	sender HermesSender
	clock  func() time.Time
	newID  func() string
}

// NewStallNudger constructs the stall-nudge reconciler.
func NewStallNudger(d StallNudgeDeps) *StallNudger {
	clock := d.Clock
	if clock == nil {
		clock = time.Now
	}
	newID := d.NewID
	if newID == nil {
		newID = func() string { return "wce_" + uuid.NewString() }
	}
	return &StallNudger{store: d.Store, sender: d.Sender, clock: clock, newID: newID}
}

func stallNudgeActivePhase(status domain.CardStatus) bool {
	return status == domain.CardStatusRunning || status == domain.CardStatusReview || status == domain.CardStatusTesting
}

// ReconcileProject scans one project's active-phase cards and nudges idle
// commanders. It returns the card IDs that were nudged this pass.
func (n *StallNudger) ReconcileProject(ctx context.Context, projectID string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if n.store == nil || n.sender == nil {
		return nil, nil
	}
	cards, err := n.store.ListWorkCards(ctx, projectID, defaultBoardID)
	if err != nil {
		return nil, fmt.Errorf("list work cards for project %s: %w", projectID, err)
	}
	sessions, err := n.store.ListSessions(ctx, domain.ProjectID(projectID))
	if err != nil {
		return nil, fmt.Errorf("list sessions for project %s: %w", projectID, err)
	}
	byID := make(map[domain.SessionID]domain.SessionRecord, len(sessions))
	for _, s := range sessions {
		byID[s.ID] = s
	}

	now := n.clock().UTC()
	var nudged []string
	for _, card := range cards {
		if !stallNudgeActivePhase(card.Status) || card.SessionID == "" || card.PausedRetarget {
			continue
		}
		session, ok := byID[domain.SessionID(card.SessionID)]
		if !ok || session.IsTerminated {
			// Orphan (no live session) is a respawn problem, not a nudge
			// problem — out of scope here, the orchestrator plan's recovery
			// task owns it.
			continue
		}
		if session.Activity.State != domain.ActivityIdle {
			// WaitingInput is sticky (human needed); Active means working.
			continue
		}
		if session.Activity.LastActivityAt.IsZero() || now.Sub(session.Activity.LastActivityAt) < stallNudgeIdleThreshold {
			continue
		}
		events, err := n.store.ListWorkCardEvents(ctx, card.ID)
		if err != nil {
			return nudged, fmt.Errorf("list events for card %s: %w", card.ID, err)
		}
		if withinStallNudgeCooldown(events, now) {
			continue
		}
		message := stallNudgeMessage(card)
		if err := n.sender.Send(ctx, session.ID, message, ""); err != nil {
			return nudged, fmt.Errorf("nudge stalled card %s: %w", card.ID, err)
		}
		if err := n.store.AppendWorkCardEvent(ctx, domain.WorkCardEvent{
			ID: n.newID(), CardID: card.ID, ProjectID: card.ProjectID,
			Kind: workCardEventStallNudged, CreatedAt: now,
		}); err != nil {
			return nudged, fmt.Errorf("record stall nudge for card %s: %w", card.ID, err)
		}
		nudged = append(nudged, card.ID)
	}
	return nudged, nil
}

func withinStallNudgeCooldown(events []domain.WorkCardEvent, now time.Time) bool {
	for _, ev := range events {
		if ev.Kind == workCardEventStallNudged && now.Sub(ev.CreatedAt) < stallNudgeCooldown {
			return true
		}
	}
	return false
}

func stallNudgeMessage(card domain.WorkCard) string {
	return fmt.Sprintf(
		"Stall check: work card %s (%q) is still in %s and this session has been idle. "+
			"Run `ao workboard get %s --json` now, then either advance the card with "+
			"`ao workboard status %s <next-status>` after finishing the phase, or move it "+
			"to blocked with the exact reason if you cannot proceed.",
		card.ID, card.Title, card.Status, card.ID, card.ID)
}
