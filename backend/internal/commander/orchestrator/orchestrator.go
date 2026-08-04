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
	killer   SessionKiller
}

// SessionKiller stops a previously spawned session. Used when the
// orchestrator supersedes a session on a phase-timeout-triggered
// replacement, so the old real process doesn't leak. A Kill failure (the
// old session may already be gone — a normal, harmless case) is handled by
// the caller and never blocks the replacement spawn.
type SessionKiller interface {
	Kill(ctx context.Context, id domain.SessionID) (bool, error)
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
	InsertRedoCycle(ctx context.Context, cycle domain.RedoCycle) error
	InsertRedoFinding(ctx context.Context, finding domain.RedoFinding) error
	ListWorkCardEvents(ctx context.Context, cardID string) ([]domain.WorkCardEvent, error)
	ListSessions(ctx context.Context, projectID domain.ProjectID) ([]domain.SessionRecord, error)
	GetProject(ctx context.Context, id string) (domain.ProjectRecord, bool, error)
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
	Killer   SessionKiller
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
		killer:   cfg.Killer,
	}
}

// Tick is called periodically (every 30s) and on every card state change event.
// It lists all cards in active phases (running, review, testing, redo) and
// ensures each has a live session.
//
// A project whose cards are commanded by the Director is skipped entirely: the
// Director spawns and sequences its own phase workers, so ticking here would
// put a second commander on every card.
func (o *ConfiguredOrchestrator) Tick(ctx context.Context, projectID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	commanded, err := o.directorCommanded(ctx, projectID)
	if err != nil {
		return err
	}
	if commanded {
		return nil
	}
	cards, err := o.store.ListWorkCards(ctx, projectID, defaultBoardID)
	if err != nil {
		return fmt.Errorf("list work cards: %w", err)
	}
	return o.tickActiveCards(ctx, cards)
}

// directorCommanded reports whether the project opted into AO's Director
// harness. Deliberately a local predicate rather than a shared one: the two
// commander mechanisms share no state, and workboard's directorEnabled makes
// the same point from the other side.
func (o *ConfiguredOrchestrator) directorCommanded(ctx context.Context, projectID string) (bool, error) {
	project, ok, err := o.store.GetProject(ctx, projectID)
	if err != nil {
		return false, fmt.Errorf("get project %s: %w", projectID, err)
	}
	if !ok {
		return false, nil
	}
	return project.Config.Director.Harness == domain.HarnessDirector, nil
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

	// See OnAgentCompleted's matching comment: the failed session — same as a
	// completed one — is by construction the very session calling this method
	// right now (`ao workboard card set-verdict` / `fail-attempt` reporting
	// its own failure), so the kill is deferred past this function's own work
	// (the redo transition, or spawning the fallback agent below) and
	// detached from ctx, never synchronous here.
	previous, hadPrevious, sessionErr := o.store.GetActiveSession(ctx, cardID)
	if sessionErr != nil {
		return fmt.Errorf("get active session for %s: %w", cardID, sessionErr)
	}
	if err := o.store.DeleteActiveSession(ctx, cardID); err != nil {
		return fmt.Errorf("delete active session for %s: %w", cardID, err)
	}
	if hadPrevious {
		defer func() {
			go o.killSupersededSession(context.WithoutCancel(ctx), previous)
		}()
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
		updatedCard, err := o.createRedoCycleAndTransition(ctx, card, phase, attempt, now, sessionStartedAt(previous, hadPrevious))
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
		updatedCard, err := o.createRedoCycleAndTransition(ctx, card, phase, attempt, now, sessionStartedAt(previous, hadPrevious))
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

	// The completed phase's session is done its job but stays live in its
	// pane until something tears it down. Capture it before DeleteActiveSession
	// removes the only record of which session that was.
	previous, hadPrevious, sessionErr := o.store.GetActiveSession(ctx, cardID)
	if sessionErr != nil {
		return fmt.Errorf("get active session for %s: %w", cardID, sessionErr)
	}
	if err := o.store.DeleteActiveSession(ctx, cardID); err != nil {
		return fmt.Errorf("delete active session for %s: %w", cardID, err)
	}
	// Deferred and detached from ctx — not a synchronous call here. The
	// session that "just completed a phase" is, by construction, the very
	// session calling this method right now (a phase completes by that
	// session reporting it via `ao workboard card handoff` / `set-verdict` /
	// `set-test-result`). An earlier version killed it synchronously at this
	// point, before the phase transition below was written: that tore down
	// the CLI process waiting on this request's own HTTP response, which
	// then failed with "context canceled" on the UpdateWorkCard call — caught
	// live, the card never advanced and kept re-spawning the same phase
	// forever. Deferring past the transition (so it is already durable) and
	// detaching from ctx (so the client's own connection closing afterward
	// can't cancel it) makes the kill strictly follow, never race, the work
	// this call exists to do.
	if hadPrevious {
		defer func() {
			go o.killSupersededSession(context.WithoutCancel(ctx), previous)
		}()
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

// ReportVerdict routes a phase-scoped outcome report — from RecordAgentEvent's
// agent_verdict, test_result, agent_failed, or a coding-phase agent_handoff —
// to OnAgentCompleted or OnAgentFailed. Those callbacks already carried the
// full phase-advancement logic but had no caller anywhere in the daemon: an
// agent's `ao workboard card set-verdict` / `set-test-result` / `fail-attempt`
// recorded an audit event and nothing else, so a card never advanced past
// review once a live session existed for it. ReportVerdict is that missing
// caller, added at the workboard service's RecordAgentEvent call site.
//
// The reporting agent is not known to the caller — the event payload carries
// only a phase and an outcome — so a failure report resolves it here from
// this package's own active_session bookkeeping via currentAgent.
func (o *ConfiguredOrchestrator) ReportVerdict(ctx context.Context, cardID string, phase string, verdict string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p := commander.Phase(phase)
	if verdict == "approved" || verdict == "pass" {
		return o.OnAgentCompleted(ctx, cardID, commander.AgentResult{Phase: p, Verdict: verdict})
	}
	card, ok, err := o.getCard(ctx, cardID)
	if err != nil {
		return fmt.Errorf("get card %s: %w", cardID, err)
	}
	if !ok {
		return fmt.Errorf("card %s not found", cardID)
	}
	return o.OnAgentFailed(ctx, cardID, commander.AgentAttempt{
		Phase:         p,
		Agent:         o.currentAgent(ctx, card, p),
		FailureReason: verdict,
		AttemptNumber: 1,
	})
}

// currentAgent resolves which agent is (or was) running phase for card, so a
// failure report can hand OnAgentFailed the agent it should fall back from.
// It prefers the live active_session record; when none exists (a coding
// session workboard's dispatch.go spawned directly never registers one) it
// falls back to the phase's first-choice agent so pickNextAgent still has a
// deterministic starting point rather than picking the first agent again.
func (o *ConfiguredOrchestrator) currentAgent(ctx context.Context, card domain.WorkCard, phase commander.Phase) string {
	if session, ok, err := o.store.GetActiveSession(ctx, card.ID); err == nil && ok && session.Agent != "" {
		return session.Agent
	}
	return o.pickNextAgent(card, phase, "")
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
//
// Each exhaustion is its own row with a fresh ID, cycle number = last + 1: an
// earlier version reused the last cycle's ID and just bumped its in-memory
// CycleNumber, which was harmless only because InsertRedoCycle was never
// actually called. Once it was (to fix retries getting no redo context at
// all), reusing an ID on a second redo would have collided with
// work_card_redo_cycles' primary key. A fresh row per cycle also keeps each
// attempt's findings — collected below — scoped to the attempt that produced
// them, instead of accumulating everything under one ever-updated row.
func (o *ConfiguredOrchestrator) createRedoCycleAndTransition(ctx context.Context, card domain.WorkCard, phase commander.Phase, attempt commander.AgentAttempt, now time.Time, sessionStartedAt time.Time) (domain.WorkCard, error) {
	cycles, err := o.store.ListRedoCycles(ctx, card.ID)
	if err != nil {
		return card, fmt.Errorf("list redo cycles for %s: %w", card.ID, err)
	}
	cycleNumber := 1
	if len(cycles) > 0 {
		cycleNumber = cycles[len(cycles)-1].CycleNumber + 1
	}
	cycle := domain.RedoCycle{
		ID:          o.newID(),
		CardID:      card.ID,
		CycleNumber: cycleNumber,
		Source:      fmt.Sprintf("%s agent exhausted", phase),
		Summary:     fmt.Sprintf("%s phase failed (%s); entering redo cycle %d", phase, attempt.FailureReason, cycleNumber),
		CreatedAt:   now,
	}
	// The retry's briefing (tick.go's redo respawn) is built by reading this
	// cycle back via ListRedoCycles — without persisting it here, that read
	// always came back empty and a retry got no information about why it was
	// sent back, not even the generic Summary above.
	if err := o.store.InsertRedoCycle(ctx, cycle); err != nil {
		return card, fmt.Errorf("insert redo cycle for %s: %w", card.ID, err)
	}

	// Attach whatever `ao workboard card set-finding` recorded during the
	// attempt that just failed — sessionStartedAt.IsZero() means there was no
	// tracked session to scope from (checkActiveSession found nothing), so
	// there is nothing attributable to this specific attempt to collect.
	if !sessionStartedAt.IsZero() {
		findings, err := o.collectPendingFindings(ctx, card.ID, cycle.ID, sessionStartedAt)
		if err != nil {
			return card, fmt.Errorf("collect findings for %s: %w", card.ID, err)
		}
		for _, finding := range findings {
			if err := o.store.InsertRedoFinding(ctx, finding); err != nil {
				return card, fmt.Errorf("insert redo finding for %s: %w", card.ID, err)
			}
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

// collectPendingFindings gathers agent_finding events the card received
// during the attempt that just failed (created after sessionStartedAt) and
// turns each into a domain.RedoFinding ready to attach to cycleID. A finding
// recorded before that attempt started belongs to an earlier, already-closed
// cycle and must not be reattached to this new one. A malformed payload is
// skipped rather than failing the whole redo transition — set-finding
// validates its own payload at write time, so a bad one here would be a
// pre-existing row, not something this attempt can still reject.
func (o *ConfiguredOrchestrator) collectPendingFindings(ctx context.Context, cardID, cycleID string, sessionStartedAt time.Time) ([]domain.RedoFinding, error) {
	events, err := o.store.ListWorkCardEvents(ctx, cardID)
	if err != nil {
		return nil, fmt.Errorf("list events for %s: %w", cardID, err)
	}
	var findings []domain.RedoFinding
	sequence := 0
	for _, event := range events {
		if event.Kind != "agent_finding" || !event.CreatedAt.After(sessionStartedAt) {
			continue
		}
		var payload struct {
			Severity string           `json:"severity"`
			Title    string           `json:"title"`
			Details  string           `json:"details"`
			Command  string           `json:"command"`
			FileRefs []domain.FileRef `json:"fileRefs"`
		}
		if err := json.Unmarshal([]byte(event.Payload), &payload); err != nil {
			continue
		}
		sequence++
		findings = append(findings, domain.RedoFinding{
			ID:        o.newID(),
			CycleID:   cycleID,
			Sequence:  sequence,
			Severity:  domain.FindingSeverity(payload.Severity),
			Title:     payload.Title,
			Details:   payload.Details,
			Command:   payload.Command,
			FileRefs:  payload.FileRefs,
			Status:    domain.FindingStatusPending,
			CreatedAt: event.CreatedAt,
			UpdatedAt: event.CreatedAt,
		})
	}
	return findings, nil
}

// sessionStartedAt returns the active session's start time, or the zero time
// when there was none to scope a redo's findings from.
func sessionStartedAt(session ActiveSessionRecord, had bool) time.Time {
	if !had {
		return time.Time{}
	}
	return session.CreatedAt
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
