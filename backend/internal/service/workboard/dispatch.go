package workboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/modernagent/modern-agent/backend/internal/domain"
	"github.com/modernagent/modern-agent/backend/internal/ports"
	sessionsvc "github.com/modernagent/modern-agent/backend/internal/service/session"
)

const (
	workCardEventDispatchFailed = "dispatch_failed"

	dispatchFailedReasonHermesUnavailable     = "hermes_unavailable"
	dispatchFailedReasonNonHermesOrchestrator = "non_hermes_orchestrator"
	dispatchFailedReasonSpawnFailed           = "spawn_failed"
)

// DispatchResult is the display-safe outcome of a single dispatch attempt.
// Result is one of "success", "wip_full", or "error". Error is a stable,
// non-secret code such as "PROJECT_NOT_FOUND" or "DISPATCH_FAILED".
type DispatchResult struct {
	AttemptedAt time.Time `json:"attemptedAt"`
	Result      string    `json:"result"`
	Error       string    `json:"error,omitempty"`
}

// DirectorStatusProvider exposes the daemon process's current view of a
// project's auto-dispatch health without revealing raw errors or internals.
type DirectorStatusProvider interface {
	Ready() bool
	LastDispatchAttempt(projectID string) DispatchResult
}

// ErrHermesUnavailable is returned by an OrchestratorSpawner when the project's
// Hermes commander is not currently reachable. Dispatch records this reason so
// the UI can explain the failure without exposing inner errors.
var ErrHermesUnavailable = errors.New("hermes unavailable")

// DispatchStore is the durable surface required to promote and claim cards.
// Workboard v1 has one board per project, so ListWorkCards uses defaultBoardID
// for both candidate selection and the project-wide running-card count.
type DispatchStore interface {
	GetProject(ctx context.Context, id string) (domain.ProjectRecord, bool, error)
	ListWorkCards(ctx context.Context, projectID, boardID string) ([]domain.WorkCard, error)
	ListSessions(ctx context.Context, projectID domain.ProjectID) ([]domain.SessionRecord, error)
	UpdateWorkCard(ctx context.Context, card domain.WorkCard) error
	ClaimReadyWorkCard(ctx context.Context, cardID, projectID string, wipLimit int, at time.Time) (bool, error)
	AppendWorkCardEvent(ctx context.Context, event domain.WorkCardEvent) error
}

// WorkerSpawner starts a worker through the existing session-service boundary.
type WorkerSpawner interface {
	Spawn(ctx context.Context, cfg ports.SpawnConfig) (domain.Session, error)
}

// OrchestratorSpawner is the established session-service operation used by
// Hermes-commanded cards. It reuses the project's one live orchestrator.
type OrchestratorSpawner interface {
	SpawnOrchestrator(ctx context.Context, projectID domain.ProjectID, clean bool, prompt string) (domain.Session, error)
}

// SpawnRollbacker compensates a successful spawn when the card cannot durably
// link to it. Keeping this separate from WorkerSpawner keeps the dispatcher
// dependent on only the recovery operation it needs.
type SpawnRollbacker interface {
	RollbackSpawn(ctx context.Context, id domain.SessionID) (sessionsvc.RollbackOutcome, error)
}

// DispatchDeps configures a Dispatcher.
type DispatchDeps struct {
	Store      DispatchStore
	Spawner    WorkerSpawner
	Rollbacker SpawnRollbacker
	Clock      func() time.Time
	NewID      func() string
	// Logger receives diagnostic branch-trace lines from DispatchOnce and the
	// director path. Optional: when nil, slog.Default() is used.
	Logger *slog.Logger
}

// Dispatcher promotes due cards and claims ready cards under a project's WIP
// limit. It stores only card facts; session status and runtime liveness remain
// owned by their existing services.
type Dispatcher struct {
	store      DispatchStore
	spawner    WorkerSpawner
	rollbacker SpawnRollbacker
	clock      func() time.Time
	newID      func() string
	logger     *slog.Logger
}

// NewDispatcher constructs a workboard dispatcher.
func NewDispatcher(d DispatchDeps) *Dispatcher {
	clock := d.Clock
	if clock == nil {
		clock = time.Now
	}
	rollbacker := d.Rollbacker
	if rollbacker == nil {
		rollbacker, _ = d.Spawner.(SpawnRollbacker)
	}
	newID := d.NewID
	if newID == nil {
		newID = func() string { return uuid.NewString() }
	}
	logger := d.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Dispatcher{store: d.Store, spawner: d.Spawner, rollbacker: rollbacker, clock: clock, newID: newID, logger: logger}
}

// DispatchOnce promotes todo and due scheduled cards, then claims ready cards
// in priority/FIFO order. Before starting a worker, it atomically writes a
// durable running claim only if the project is still below its WIP limit, so
// independent dispatcher instances cannot over-claim the same project.
//
// A recoverable failure to start an individual card releases that card's claim,
// records a dispatch_failed event, and continues with the next candidate. Project
// reads, card-list reads, claim/update failures, and session-link failures remain
// fatal so the daemon logs real problems instead of silently dropping them.
func (d *Dispatcher) DispatchOnce(ctx context.Context, projectID string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		d.logger.Info("workboard dispatch: branch=ctx_canceled", "project", projectID, "err", err)
		return nil, err
	}
	if d.store == nil || d.spawner == nil {
		d.logger.Info("workboard dispatch: branch=deps_unwired", "project", projectID)
		return nil, nil
	}
	project, ok, err := d.store.GetProject(ctx, projectID)
	if err != nil {
		d.logger.Info("workboard dispatch: branch=get_project_error", "project", projectID, "err", err)
		return nil, fmt.Errorf("get project %s: %w", projectID, err)
	}
	if !ok {
		d.logger.Info("workboard dispatch: branch=project_not_found", "project", projectID)
		return nil, fmt.Errorf("project %s not found", projectID)
	}
	commanding := project.Config.Orchestrator.Harness == domain.HarnessHermes
	if !commanding && d.rollbacker == nil {
		d.logger.Info("workboard dispatch: branch=rollback_unwired", "project", projectID)
		return nil, fmt.Errorf("workboard dispatcher requires spawn rollback support")
	}
	var orchestrator OrchestratorSpawner
	if commanding {
		var supported bool
		orchestrator, supported = d.spawner.(OrchestratorSpawner)
		if !supported {
			d.logger.Info("workboard dispatch: branch=orchestrator_unwired", "project", projectID)
			return nil, fmt.Errorf("workboard dispatcher requires orchestrator spawn support for Hermes")
		}
	}
	cards, err := d.store.ListWorkCards(ctx, projectID, defaultBoardID)
	if err != nil {
		d.logger.Info("workboard dispatch: branch=list_cards_error", "project", projectID, "err", err)
		return nil, fmt.Errorf("list work cards for project %s: %w", projectID, err)
	}
	d.logger.Info("workboard dispatch: branch=dispatch", "project", projectID, "directorEnabled", directorEnabled(project), "commanding", commanding, "cardCount", len(cards))

	if directorEnabled(project) {
		return d.dispatchToDirector(ctx, project, cards, d.clock().UTC())
	}

	now := d.clock().UTC()
	wasTodo := make(map[string]bool, len(cards))
	for i := range cards {
		card := &cards[i]
		switch card.Status {
		case domain.CardStatusTodo:
			wasTodo[card.ID] = true
		case domain.CardStatusScheduled:
			if card.ScheduledAt == nil || card.ScheduledAt.After(now) {
				continue
			}
		default:
			continue
		}
		card.Status = domain.CardStatusReady
		card.ReadyAt = timePtr(now)
		card.UpdatedAt = now
		if err := d.store.UpdateWorkCard(ctx, *card); err != nil {
			return nil, fmt.Errorf("promote card %s: %w", card.ID, err)
		}
	}

	wipLimit := project.Config.Workboard.WIPLimit
	if wipLimit <= 0 {
		wipLimit = domain.DefaultWorkboardConfig().WIPLimit
	}
	candidates := make([]domain.WorkCard, 0, len(cards))
	for _, card := range cards {
		if card.Status == domain.CardStatusReady && !card.PausedRetarget {
			candidates = append(candidates, card)
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		left, right := candidates[i], candidates[j]
		if left.Priority.Rank() != right.Priority.Rank() {
			return left.Priority.Rank() > right.Priority.Rank()
		}
		leftReady, rightReady := cardReadyAt(left), cardReadyAt(right)
		if !leftReady.Equal(rightReady) {
			return leftReady.Before(rightReady)
		}
		return left.ID < right.ID
	})

	var claimed []string
	for _, card := range candidates {
		won, err := d.store.ClaimReadyWorkCard(ctx, card.ID, projectID, wipLimit, now)
		if err != nil {
			return claimed, fmt.Errorf("persist dispatch claim for card %s: %w", card.ID, err)
		}
		if !won {
			// WIP pressure is not an error. A card that was just promoted from Todo
			// goes back to Todo so the board shows it is queued, not stuck in Ready.
			if wasTodo[card.ID] {
				card.Status = domain.CardStatusTodo
				card.ReadyAt = nil
				card.SessionID = ""
				card.UpdatedAt = now
				if err := d.store.UpdateWorkCard(ctx, card); err != nil {
					return claimed, fmt.Errorf("release Todo promotion for card %s: %w", card.ID, err)
				}
			}
			break
		}
		card.Status = domain.CardStatusRunning
		card.SessionID = ""
		card.UpdatedAt = now

		var session domain.Session
		var spawnErr error
		failureReason := ""
		if commanding {
			active, activeErr := d.store.ListSessions(ctx, domain.ProjectID(projectID))
			if activeErr != nil {
				return claimed, fmt.Errorf("list orchestrators for card %s: %w", card.ID, activeErr)
			} else if hasActiveNonHermesOrchestrator(active) {
				spawnErr = fmt.Errorf("project %s has a non-Hermes active orchestrator", projectID)
				failureReason = dispatchFailedReasonNonHermesOrchestrator
			} else {
				briefing, briefErr := hermesCardBriefing(card)
				if briefErr != nil {
					return claimed, briefErr
				}
				session, spawnErr = orchestrator.SpawnOrchestrator(ctx, domain.ProjectID(projectID), false, briefing)
			}
			if spawnErr == nil && (session.Kind != domain.KindOrchestrator || session.Harness != domain.HarnessHermes) {
				spawnErr = fmt.Errorf("project %s has a non-Hermes active orchestrator", projectID)
				failureReason = dispatchFailedReasonNonHermesOrchestrator
			}
		} else {
			session, spawnErr = d.spawner.Spawn(ctx, ports.SpawnConfig{
				ProjectID:  domain.ProjectID(projectID),
				Kind:       domain.KindWorker,
				Harness:    domain.AgentHarness(card.Agent),
				Prompt:     codingWorkerPrompt(card),
				TargetPath: card.TargetPath,
			})
		}
		if spawnErr != nil {
			if failureReason == "" && commanding && errors.Is(spawnErr, ErrHermesUnavailable) {
				failureReason = dispatchFailedReasonHermesUnavailable
			}
			if failureReason == "" {
				failureReason = dispatchFailedReasonSpawnFailed
			}
			if recoverErr := d.recoverCardFromSpawnFailure(ctx, card, wasTodo[card.ID], commanding, failureReason, spawnErr, now); recoverErr != nil {
				return claimed, recoverErr
			}
			continue
		}

		card.SessionID = string(session.ID)
		card.UpdatedAt = now
		if err := d.store.UpdateWorkCard(ctx, card); err != nil {
			linkErr := fmt.Errorf("link worker session for card %s: %w", card.ID, err)
			if commanding {
				card.Status = domain.CardStatusReady
				card.SessionID = ""
				card.UpdatedAt = now
				if releaseErr := d.store.UpdateWorkCard(context.WithoutCancel(ctx), card); releaseErr != nil {
					return claimed, errors.Join(linkErr, fmt.Errorf("release Hermes dispatch claim for card %s: %w", card.ID, releaseErr))
				}
				return claimed, linkErr
			}
			persistenceCtx := context.WithoutCancel(ctx)
			if _, rollbackErr := d.rollbacker.RollbackSpawn(persistenceCtx, session.ID); rollbackErr != nil {
				return claimed, errors.Join(
					linkErr,
					fmt.Errorf("rollback session %s: %w", session.ID, rollbackErr),
					fmt.Errorf("card %s remains durably claimed as running without a session ID", card.ID),
				)
			}
			card.Status = domain.CardStatusReady
			card.SessionID = ""
			card.UpdatedAt = now
			if releaseErr := d.store.UpdateWorkCard(persistenceCtx, card); releaseErr != nil {
				return claimed, errors.Join(
					linkErr,
					fmt.Errorf("release dispatch claim for card %s after rollback: %w", card.ID, releaseErr),
					fmt.Errorf("card %s remains durably claimed as running without a session ID", card.ID),
				)
			}
			return claimed, linkErr
		}
		claimed = append(claimed, card.ID)
	}
	d.logger.Info("workboard dispatch: branch=done", "project", projectID, "claimed", len(claimed))
	return claimed, nil
}

// recoverCardFromSpawnFailure releases a card after an individual recoverable
// spawn failure, records a dispatch_failed event with a safe reason, and swallows
// the original spawn error. If the durable release or event append fails, it
// returns a fatal joined error so the caller does not silently drop the card.
func (d *Dispatcher) recoverCardFromSpawnFailure(ctx context.Context, card domain.WorkCard, originallyTodo, commanding bool, reason string, spawnErr error, now time.Time) error {
	switch {
	case commanding:
		// Hermes-commanded projects keep their single commander. A failed briefing
		// returns the card to the Todo queue so it can retry on the next pass.
		card.Status = domain.CardStatusTodo
		card.ReadyAt = nil
	case originallyTodo:
		// A card that was promoted from Todo goes back to Todo so the board shows
		// it is queued, not stuck in Ready.
		card.Status = domain.CardStatusTodo
		card.ReadyAt = nil
	default:
		card.Status = domain.CardStatusReady
	}
	card.SessionID = ""
	card.UpdatedAt = now

	payload, _ := json.Marshal(map[string]string{
		"reason":      reason,
		"attemptedAt": now.Format(time.RFC3339),
	})
	event := domain.WorkCardEvent{
		ID:        d.newID(),
		CardID:    card.ID,
		ProjectID: card.ProjectID,
		Kind:      workCardEventDispatchFailed,
		Payload:   string(payload),
		CreatedAt: now,
	}

	persistenceCtx := context.WithoutCancel(ctx)
	if releaseErr := d.store.UpdateWorkCard(persistenceCtx, card); releaseErr != nil {
		return errors.Join(
			fmt.Errorf("start %s for card %s: %w", dispatchRole(commanding), card.ID, spawnErr),
			fmt.Errorf("release dispatch claim for card %s: %w", card.ID, releaseErr),
			fmt.Errorf("card %s remains durably claimed as running without a session ID", card.ID),
		)
	}
	if eventErr := d.store.AppendWorkCardEvent(persistenceCtx, event); eventErr != nil {
		return errors.Join(
			fmt.Errorf("start %s for card %s: %w", dispatchRole(commanding), card.ID, spawnErr),
			fmt.Errorf("record dispatch failure event for card %s: %w", card.ID, eventErr),
		)
	}
	return nil
}

func hasActiveNonHermesOrchestrator(sessions []domain.SessionRecord) bool {
	for _, session := range sessions {
		if !session.IsTerminated && session.Kind == domain.KindOrchestrator && session.Harness != domain.HarnessHermes {
			return true
		}
	}
	return false
}

func dispatchRole(commanding bool) string {
	if commanding {
		return "Hermes commander"
	}
	return "worker"
}

// codingWorkerPrompt builds the initial prompt for a plain (non-Director,
// non-Hermes-commanded) project's first coding spawn. The instruction is
// required, not decorative: commander/orchestrator only advances a card past
// running when it sees a coding-phase `ao workboard card handoff` (wired via
// workboard's reportPhaseOutcome). Without it in this prompt, a worker spawned
// here has no way to know it should ever call that command, and the card sits
// in running until someone reports on its behalf by hand.
func codingWorkerPrompt(card domain.WorkCard) string {
	return card.Title + "\n\n" + card.Notes +
		"\n\nVerification here is `npm run lint` and `npm run build` (or this repo's equivalent) passing — that is enough. This card has dedicated review and testing phases downstream for anything deeper (manual browser checks, screenshots, multi-viewport runs); doing that work yourself burns your iteration budget before you ever report, and the card sits stuck with nothing recorded. As soon as lint and build pass, record a handoff with changed files, checks/results, commit or PR, and the review focus: ao workboard card handoff " + card.ID + " --phase coding --summary \"...\"."
}

// hermesCardBriefing is intentionally short: Hermes's system prompt
// (hermesWorkboardPrompt, session_manager/manager.go) already carries the
// full automation policy once per session, and `ao workboard get` returns
// the live card on demand. Repeating the full card JSON and the policy
// paragraph on every dispatch — including every later card sent to an
// already-running commander via SpawnOrchestrator's reuse path — was pure
// waste. This keeps only what Hermes needs before it can even run that
// command: which card, which goal version, and enough identity to act if
// the read fails.
func hermesCardBriefing(card domain.WorkCard) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "New work card: %s\n", card.ID)
	fmt.Fprintf(&b, "Goal version: %d\n", card.GoalVersion)
	fmt.Fprintf(&b, "Title: %s\n", card.Title)
	if card.TargetPath != "" {
		fmt.Fprintf(&b, "Target: %s\n", card.TargetPath)
	}
	fmt.Fprintf(&b, "Coding: %s\n", firstNonEmpty(card.CodingAgent, card.Agent))
	if card.ReviewerAgent != "" {
		fmt.Fprintf(&b, "Review: %s\n", card.ReviewerAgent)
	}
	if card.TestingAgent != "" {
		fmt.Fprintf(&b, "Testing: %s\n", card.TestingAgent)
	}
	fmt.Fprintf(&b, "\nRead the latest source of truth first:\nao workboard get %s --json\n\nThen plan, delegate, and drive the card through its configured workflow.", card.ID)
	return b.String(), nil
}

func cardReadyAt(card domain.WorkCard) time.Time {
	if card.ReadyAt != nil {
		return *card.ReadyAt
	}
	return card.CreatedAt
}

func timePtr(t time.Time) *time.Time { return &t }
