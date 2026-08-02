package workboard

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modernagent/modern-agent/backend/internal/domain"
	"github.com/modernagent/modern-agent/backend/internal/httpd/apierr"
	"github.com/modernagent/modern-agent/backend/internal/ports"
)

const (
	workCardEventNudged     = "nudged"
	workCardEventRetargeted = "retargeted"
	workCardEventSplit      = "split"
)

// RunningCardStore is the durable surface for running-card control actions.
type RunningCardStore interface {
	GetWorkCard(ctx context.Context, id string) (domain.WorkCard, bool, error)
	UpdateWorkCard(ctx context.Context, card domain.WorkCard) error
	CreateWorkCard(ctx context.Context, card domain.WorkCard) error
	ListSessions(ctx context.Context, projectID domain.ProjectID) ([]domain.SessionRecord, error)
	AppendWorkCardEvent(ctx context.Context, event domain.WorkCardEvent) error
}

// SessionMessenger sends a message to a live session.
type SessionMessenger interface {
	Send(ctx context.Context, id domain.SessionID, message string, sender domain.SessionID) error
}

// NudgeInput is the user message delivered to a running card's worker session.
type NudgeInput struct {
	Message string
}

// RetargetInput updates the goal on a running card and hands off to Hermes or respawns.
type RetargetInput struct {
	Title *string
	Notes *string
}

// SplitInput creates a successor card from a running card and archives the old one.
type SplitInput struct {
	Title            string
	Notes            string
	OldCardFate      domain.CardStatus
	StartImmediately bool
}

// SplitResult is the old and new cards after a split.
type SplitResult struct {
	OldCard domain.WorkCard
	NewCard domain.WorkCard
}

// Nudge sends a message to the linked worker session and records a nudged event.
func (s *Service) Nudge(ctx context.Context, id string, in NudgeInput) (domain.WorkCard, error) {
	if s.sender == nil {
		return domain.WorkCard{}, apierr.Internal("WORK_CARD_NUDGE_UNAVAILABLE", "Work card nudge is unavailable")
	}
	card, err := s.requireRunningCard(ctx, id)
	if err != nil {
		return domain.WorkCard{}, err
	}
	message := strings.TrimSpace(in.Message)
	if message == "" {
		return domain.WorkCard{}, apierr.Invalid("WORK_CARD_NUDGE_MESSAGE_REQUIRED", "Message is required", nil)
	}
	if card.SessionID == "" {
		return domain.WorkCard{}, apierr.Invalid("WORK_CARD_SESSION_REQUIRED", "Running card has no linked session", nil)
	}
	if err := s.sender.Send(ctx, domain.SessionID(card.SessionID), message, ""); err != nil {
		return domain.WorkCard{}, apierr.Internal("WORK_CARD_NUDGE_FAILED", "Failed to nudge linked session")
	}
	now := s.clock().UTC()
	payload, _ := json.Marshal(map[string]string{
		"sessionId": card.SessionID,
		"message":   message,
	})
	store, err := s.runningStore()
	if err != nil {
		return domain.WorkCard{}, err
	}
	if err := store.AppendWorkCardEvent(ctx, domain.WorkCardEvent{
		ID: s.newID(), CardID: card.ID, ProjectID: card.ProjectID, Kind: workCardEventNudged,
		Payload: string(payload), CreatedAt: now,
	}); err != nil {
		return domain.WorkCard{}, apierr.Internal("WORK_CARD_EVENT_FAILED", "Failed to record nudge event")
	}
	card.UpdatedAt = now
	if err := store.UpdateWorkCard(ctx, card); err != nil {
		return domain.WorkCard{}, apierr.Internal("WORK_CARD_UPDATE_FAILED", "Failed to update work card")
	}
	return card, nil
}

// Retarget pauses WIP, updates the card goal, hands off through Hermes or respawns, then resumes.
func (s *Service) Retarget(ctx context.Context, id string, in RetargetInput) (domain.WorkCard, error) {
	if s.sender == nil {
		return domain.WorkCard{}, apierr.Internal("WORK_CARD_RETARGET_UNAVAILABLE", "Work card retarget is unavailable")
	}
	card, err := s.requireRunningCard(ctx, id)
	if err != nil {
		return domain.WorkCard{}, err
	}
	before := card
	if in.Title == nil && in.Notes == nil {
		return domain.WorkCard{}, apierr.Invalid("WORK_CARD_RETARGET_FIELDS_REQUIRED", "Title or notes is required", nil)
	}
	if in.Title != nil {
		title := strings.TrimSpace(*in.Title)
		if title == "" {
			return domain.WorkCard{}, apierr.Invalid("WORK_CARD_TITLE_REQUIRED", "Title is required", nil)
		}
		card.Title = title
	}
	if in.Notes != nil {
		notes := strings.TrimSpace(*in.Notes)
		if notes == "" {
			return domain.WorkCard{}, apierr.Invalid("WORK_CARD_NOTES_REQUIRED", "Notes are required", nil)
		}
		card.Notes = notes
	}

	now := s.clock().UTC()
	card.GoalVersion++
	card.UpdatedAt = now
	store, err := s.runningStore()
	if err != nil {
		return domain.WorkCard{}, err
	}
	card.PausedRetarget = true
	if err := store.UpdateWorkCard(ctx, card); err != nil {
		return domain.WorkCard{}, apierr.Internal("WORK_CARD_UPDATE_FAILED", "Failed to pause card for retarget")
	}
	revertRetarget := func() {
		reverted := before
		reverted.UpdatedAt = s.clock().UTC()
		_ = store.UpdateWorkCard(ctx, reverted)
	}

	sessions, err := store.ListSessions(ctx, domain.ProjectID(card.ProjectID))
	if err != nil {
		revertRetarget()
		return domain.WorkCard{}, apierr.Internal("WORK_CARD_RETARGET_FAILED", "Failed to load project sessions")
	}
	workers, hermes := answerSessions(sessions)
	worker, workerOK := workers[domain.SessionID(card.SessionID)]
	commander, commanded := hermesCommanderSession(sessions, domain.SessionID(card.SessionID))
	handoff := retargetHandoffPrompt(card, card.GoalVersion)
	var handoffSession domain.SessionID
	var spawnedSession domain.SessionID
	switch {
	case commanded:
		handoffSession = commander.ID
	case workerOK && !worker.IsTerminated:
		if hermes.ID != "" {
			handoffSession = hermes.ID
		} else {
			handoffSession = worker.ID
		}
	case card.SessionID != "":
		if s.spawner == nil {
			revertRetarget()
			return domain.WorkCard{}, apierr.Internal("WORK_CARD_RETARGET_UNAVAILABLE", "Work card retarget is unavailable")
		}
		spawned, err := s.spawner.Spawn(ctx, ports.SpawnConfig{
			ProjectID:   domain.ProjectID(card.ProjectID),
			Kind:        domain.KindWorker,
			Harness:     domain.AgentHarness(card.Agent),
			Prompt:      handoff,
			TargetPath:  card.TargetPath,
			DisplayName: card.Title,
		})
		if err != nil {
			revertRetarget()
			return domain.WorkCard{}, apierr.Internal("WORK_CARD_RETARGET_FAILED", "Failed to respawn worker for retarget")
		}
		spawnedSession = spawned.ID
		if s.killer != nil {
			if _, err := s.killer.Kill(ctx, domain.SessionID(before.SessionID)); err != nil {
				_, _ = s.killer.Kill(ctx, spawnedSession)
				revertRetarget()
				return domain.WorkCard{}, apierr.Internal("WORK_CARD_RETARGET_FAILED", "Failed to stop previous worker session")
			}
		}
		card.SessionID = string(spawnedSession)
	default:
		revertRetarget()
		return domain.WorkCard{}, apierr.Invalid("WORK_CARD_SESSION_REQUIRED", "Running card has no live linked session", nil)
	}
	if handoffSession != "" {
		if err := s.sender.Send(ctx, handoffSession, handoff, ""); err != nil {
			if spawnedSession != "" && s.killer != nil {
				_, _ = s.killer.Kill(ctx, spawnedSession)
			}
			revertRetarget()
			return domain.WorkCard{}, apierr.Internal("WORK_CARD_RETARGET_FAILED", "Failed to hand off retargeted goal")
		}
	}

	card.PausedRetarget = false
	card.UpdatedAt = s.clock().UTC()
	if err := store.UpdateWorkCard(ctx, card); err != nil {
		return domain.WorkCard{}, apierr.Internal("WORK_CARD_UPDATE_FAILED", "Failed to resume card after retarget")
	}
	payload, _ := json.Marshal(map[string]any{
		"goalVersion": card.GoalVersion,
		"sessionId":   card.SessionID,
		"title":       card.Title,
		"notes":       card.Notes,
	})
	if err := store.AppendWorkCardEvent(ctx, domain.WorkCardEvent{
		ID: s.newID(), CardID: card.ID, ProjectID: card.ProjectID, Kind: workCardEventRetargeted,
		Payload: string(payload), CreatedAt: card.UpdatedAt,
	}); err != nil {
		return domain.WorkCard{}, apierr.Internal("WORK_CARD_EVENT_FAILED", "Failed to record retarget event")
	}
	return card, nil
}

// Split creates a successor card, archives the running card, and optionally queues the successor.
func (s *Service) Split(ctx context.Context, id string, in SplitInput) (SplitResult, error) {
	card, err := s.requireRunningCard(ctx, id)
	if err != nil {
		return SplitResult{}, err
	}
	title := strings.TrimSpace(in.Title)
	notes := strings.TrimSpace(in.Notes)
	if title == "" {
		return SplitResult{}, apierr.Invalid("WORK_CARD_TITLE_REQUIRED", "Title is required", nil)
	}
	if notes == "" {
		return SplitResult{}, apierr.Invalid("WORK_CARD_NOTES_REQUIRED", "Notes are required", nil)
	}
	fate := in.OldCardFate
	if fate == "" {
		fate = domain.CardStatusTodo
	}
	switch fate {
	case domain.CardStatusTodo, domain.CardStatusBlocked, domain.CardStatusDone:
	default:
		return SplitResult{}, apierr.Invalid("WORK_CARD_SPLIT_FATE_INVALID", "Old card fate must be todo, blocked, or done", nil)
	}

	now := s.clock().UTC()
	newCard := domain.WorkCard{
		ID:          s.newID(),
		ProjectID:   card.ProjectID,
		BoardID:     card.BoardID,
		Title:       title,
		Notes:       notes,
		Priority:    card.Priority,
		Labels:      append([]string(nil), card.Labels...),
		Status:      domain.CardStatusTriage,
		TargetPath:  card.TargetPath,
		RepoName:    card.RepoName,
		Agent:       card.Agent,
		GoalVersion: 1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if in.StartImmediately {
		newCard.Status = domain.CardStatusReady
		newCard.ReadyAt = &now
	}
	store, err := s.runningStore()
	if err != nil {
		return SplitResult{}, err
	}
	sessions, err := store.ListSessions(ctx, domain.ProjectID(card.ProjectID))
	if err != nil {
		return SplitResult{}, apierr.Internal("WORK_CARD_SPLIT_FAILED", "Failed to load project sessions")
	}
	commander, commanded := hermesCommanderSession(sessions, domain.SessionID(card.SessionID))
	if !commanded && s.killer == nil {
		return SplitResult{}, apierr.Internal("WORK_CARD_SPLIT_UNAVAILABLE", "Work card split is unavailable")
	}
	if err := store.CreateWorkCard(ctx, newCard); err != nil {
		return SplitResult{}, apierr.Internal("WORK_CARD_CREATE_FAILED", "Failed to create successor card")
	}
	blockSuccessor := func() {
		blocked := newCard
		blocked.Status = domain.CardStatusBlocked
		blocked.ReadyAt = nil
		blocked.UpdatedAt = s.clock().UTC()
		_ = store.UpdateWorkCard(ctx, blocked)
	}

	if commanded {
		if s.sender == nil {
			blockSuccessor()
			return SplitResult{}, apierr.Internal("WORK_CARD_SPLIT_FAILED", "Failed to notify Hermes commander")
		}
		if err := s.sender.Send(ctx, commander.ID, splitHandoffPrompt(card, newCard, fate), ""); err != nil {
			blockSuccessor()
			return SplitResult{}, apierr.Internal("WORK_CARD_SPLIT_FAILED", "Failed to notify Hermes commander")
		}
	} else if card.SessionID != "" {
		if _, err := s.killer.Kill(ctx, domain.SessionID(card.SessionID)); err != nil {
			blockSuccessor()
			return SplitResult{}, apierr.Internal("WORK_CARD_SPLIT_FAILED", "Failed to stop previous worker session")
		}
	}
	card.Status = fate
	card.SessionID = ""
	card.WaitingForInput = false
	card.PausedRetarget = false
	card.SupersededByCardID = newCard.ID
	card.UpdatedAt = now
	if err := store.UpdateWorkCard(ctx, card); err != nil {
		blockSuccessor()
		return SplitResult{}, apierr.Internal("WORK_CARD_UPDATE_FAILED", "Failed to archive split card")
	}
	payload, _ := json.Marshal(map[string]string{
		"newCardId": newCard.ID,
		"oldFate":   string(fate),
	})
	if err := store.AppendWorkCardEvent(ctx, domain.WorkCardEvent{
		ID: s.newID(), CardID: card.ID, ProjectID: card.ProjectID, Kind: workCardEventSplit,
		Payload: string(payload), CreatedAt: now,
	}); err != nil {
		return SplitResult{}, apierr.Internal("WORK_CARD_EVENT_FAILED", "Failed to record split event")
	}
	s.kickDispatchIfTodo(card.ProjectID, card.Status)
	s.kickDispatchIfTodo(newCard.ProjectID, newCard.Status)
	return SplitResult{OldCard: card, NewCard: newCard}, nil
}

func isHermesCommander(session domain.SessionRecord) bool {
	return !session.IsTerminated && session.Kind == domain.KindOrchestrator && session.Harness == domain.HarnessHermes
}

func hermesCommanderSession(sessions []domain.SessionRecord, id domain.SessionID) (domain.SessionRecord, bool) {
	for _, session := range sessions {
		if session.ID == id && isHermesCommander(session) {
			return session, true
		}
	}
	return domain.SessionRecord{}, false
}

func splitHandoffPrompt(oldCard, newCard domain.WorkCard, fate domain.CardStatus) string {
	return "AO split work-card " + oldCard.ID + " into " + newCard.ID + ". Stop coordinating the old card; it moved to " + string(fate) + ". The new card will be dispatched separately."
}

func (s *Service) requireRunningCard(ctx context.Context, id string) (domain.WorkCard, error) {
	card, err := s.Get(ctx, id)
	if err != nil {
		return domain.WorkCard{}, err
	}
	if card.Status != domain.CardStatusRunning {
		return domain.WorkCard{}, apierr.Invalid("WORK_CARD_NOT_RUNNING", "Card must be running", nil)
	}
	return card, nil
}

func (s *Service) runningStore() (RunningCardStore, error) {
	store, ok := s.store.(RunningCardStore)
	if !ok {
		return nil, apierr.Internal("WORK_CARD_ACTIONS_UNAVAILABLE", "Work card actions are unavailable")
	}
	return store, nil
}

func retargetHandoffPrompt(card domain.WorkCard, goalVersion int) string {
	var b strings.Builder
	b.WriteString("AO retarget: the Workboard card goal changed. Continue on the same worktree with the updated goal.\n\n")
	b.WriteString(fmt.Sprintf("Goal version: %d\n", goalVersion))
	b.WriteString(card.Title)
	if strings.TrimSpace(card.Notes) != "" {
		b.WriteString("\n\n")
		b.WriteString(card.Notes)
	}
	return b.String()
}
