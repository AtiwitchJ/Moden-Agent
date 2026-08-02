// Package workboard implements durable work-card CRUD for the project board.
package workboard

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/modernagent/modern-agent/backend/internal/domain"
	"github.com/modernagent/modern-agent/backend/internal/httpd/apierr"
)

const defaultBoardID = "default"

// Store is the narrow durable surface required by Service.
type Store interface {
	CreateWorkCard(ctx context.Context, card domain.WorkCard) error
	GetWorkCard(ctx context.Context, id string) (domain.WorkCard, bool, error)
	ListWorkCards(ctx context.Context, projectID, boardID string) ([]domain.WorkCard, error)
	ListAllWorkCards(ctx context.Context) ([]domain.WorkCard, error)
	UpdateWorkCard(ctx context.Context, card domain.WorkCard) error
	GetProject(ctx context.Context, id string) (domain.ProjectRecord, bool, error)
	ListWorkspaceRepos(ctx context.Context, projectID string) ([]domain.WorkspaceRepoRecord, error)
	InsertRedoCycle(ctx context.Context, cycle domain.RedoCycle) error
	GetLatestRedoCycle(ctx context.Context, cardID string) (domain.RedoCycle, bool, error)
	ListRedoCycles(ctx context.Context, cardID string) ([]domain.RedoCycle, error)
	CompleteRedoCycle(ctx context.Context, cycleID string, completedAt time.Time) error
	InsertRedoFinding(ctx context.Context, finding domain.RedoFinding) error
	ListRedoFindings(ctx context.Context, cycleID string) ([]domain.RedoFinding, error)
	UpdateRedoFindingStatus(ctx context.Context, findingID string, status domain.FindingStatus, addAttempts int, at time.Time) error
	InsertRedoAttempt(ctx context.Context, attempt domain.RedoAttempt) error
	ListRedoAttempts(ctx context.Context, findingID string) ([]domain.RedoAttempt, error)
}

type cardDeleter interface {
	DeleteWorkCard(ctx context.Context, id string) error
}

type workCardEventLister interface {
	ListWorkCardEvents(ctx context.Context, cardID string) ([]domain.WorkCardEvent, error)
}

// DispatchFailure is the safe, user-facing explanation of the latest failed
// automatic dispatch attempt for a card.
type DispatchFailure struct {
	CardID      string
	Reason      string
	AttemptedAt time.Time
}

// CreateInput is the required content and placement of a new work card.
type CreateInput struct {
	ProjectID     string
	BoardID       string
	Title         string
	Notes         string
	Priority      domain.CardPriority
	Labels        []string
	Status        domain.CardStatus
	TargetPath    string
	Agent         string
	CodingAgent   string
	ReviewerMode  string
	ReviewerAgent string
	TestingAgent  string
	SessionID     string
	ScheduledAt   *time.Time
}

// UpdateInput is a partial update. Nil fields are left unchanged.
type UpdateInput struct {
	Title       *string
	Notes       *string
	Priority    *domain.CardPriority
	Labels      *[]string
	Status      *domain.CardStatus
	ScheduledAt OptionalTime
	TargetPath  *string
	Agent       *string
	SessionID   *string
	Position    *int64
}

// OptionalTime preserves the PATCH distinction between an omitted time and an
// explicit null that clears an existing timestamp.
type OptionalTime struct {
	Set   bool
	Value *time.Time
}

// Service owns work-card validation and orchestration-free CRUD.
type Service struct {
	store          Store
	sender         SessionMessenger
	spawner        WorkerSpawner
	killer         SessionKiller
	dispatchKicker DispatchKicker
	clock          func() time.Time
	newID          func() string
}

// DispatchKicker wakes the daemon's per-project dispatch trigger. It is
// implemented by the daemon's DispatchTrigger and kept as an interface here so
// the service can ask for dispatch without depending on the daemon package.
type DispatchKicker interface {
	Kick(projectID string)
}

// Deps configures optional collaborators for Service.
type Deps struct {
	Store          Store
	Sender         SessionMessenger
	Spawner        WorkerSpawner
	Killer         SessionKiller
	DispatchKicker DispatchKicker
	Clock          func() time.Time
	NewID          func() string
}

// New creates a workboard service backed by store.
func New(store Store) *Service {
	return NewWithDeps(Deps{Store: store})
}

// NewWithDeps creates a workboard service with testable time and id sources.
func NewWithDeps(d Deps) *Service {
	s := &Service{
		store: d.Store, sender: d.Sender, spawner: d.Spawner, killer: d.Killer,
		dispatchKicker: d.DispatchKicker, clock: d.Clock, newID: d.NewID,
	}
	if s.clock == nil {
		s.clock = time.Now
	}
	if s.newID == nil {
		s.newID = func() string { return "card_" + uuid.NewString() }
	}
	return s
}

// LatestDispatchFailure returns the newest dispatch_failed audit event. It
// deliberately exposes only the stable reason code, never a daemon or agent
// error string that could contain implementation details.
func (s *Service) LatestDispatchFailure(ctx context.Context, cardID string) (DispatchFailure, error) {
	card, err := s.Get(ctx, cardID)
	if err != nil {
		return DispatchFailure{}, err
	}
	lister, ok := s.store.(workCardEventLister)
	if !ok {
		return DispatchFailure{}, apierr.Internal("WORK_CARD_EVENTS_UNAVAILABLE", "Work card events are unavailable")
	}
	events, err := lister.ListWorkCardEvents(ctx, card.ID)
	if err != nil {
		return DispatchFailure{}, apierr.Internal("WORK_CARD_EVENTS_LOAD_FAILED", "Failed to load work card events")
	}
	for i := len(events) - 1; i >= 0; i-- {
		event := events[i]
		if event.Kind != workCardEventDispatchFailed {
			continue
		}
		var payload struct {
			Reason      string `json:"reason"`
			AttemptedAt string `json:"attemptedAt"`
		}
		if err := json.Unmarshal([]byte(event.Payload), &payload); err != nil || strings.TrimSpace(payload.Reason) == "" {
			continue
		}
		attemptedAt := event.CreatedAt
		if parsed, err := time.Parse(time.RFC3339, payload.AttemptedAt); err == nil {
			attemptedAt = parsed
		}
		return DispatchFailure{CardID: card.ID, Reason: payload.Reason, AttemptedAt: attemptedAt}, nil
	}
	return DispatchFailure{}, apierr.NotFound("WORK_CARD_DISPATCH_FAILURE_NOT_FOUND", "No dispatch failure found for this work card")
}

// Create validates and persists a new work card.
func (s *Service) Create(ctx context.Context, in CreateInput) (domain.WorkCard, error) {
	projectID := strings.TrimSpace(in.ProjectID)
	if projectID == "" {
		return domain.WorkCard{}, apierr.Invalid("WORK_CARD_PROJECT_REQUIRED", "Project is required", nil)
	}
	project, ok, err := s.store.GetProject(ctx, projectID)
	if err != nil {
		return domain.WorkCard{}, apierr.Internal("PROJECT_LOAD_FAILED", "Failed to load project")
	}
	if !ok || !project.ArchivedAt.IsZero() {
		return domain.WorkCard{}, apierr.NotFound("PROJECT_NOT_FOUND", "Unknown project")
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return domain.WorkCard{}, apierr.Invalid("WORK_CARD_TITLE_REQUIRED", "Title is required", nil)
	}
	priority := in.Priority
	if priority == "" {
		priority = domain.CardPriorityNormal
	} else if _, err := domain.ParseCardPriority(string(priority)); err != nil {
		return domain.WorkCard{}, apierr.Invalid("WORK_CARD_PRIORITY_INVALID", err.Error(), nil)
	}
	labels := in.Labels
	if labels == nil {
		labels = []string{}
	}
	status := in.Status
	if status == "" {
		status = domain.CardStatusTodo
	}
	if err := domain.ValidateCardStatus(string(status)); err != nil {
		return domain.WorkCard{}, apierr.Invalid("WORK_CARD_STATUS_INVALID", err.Error(), nil)
	}
	boardID := strings.TrimSpace(in.BoardID)
	if boardID == "" {
		boardID = defaultBoardID
	}
	targetPath := strings.TrimSpace(in.TargetPath)
	if targetPath == "" {
		targetPath = project.Path
	} else {
		validatedPath, err := s.validateTargetPath(ctx, projectID, targetPath)
		if err != nil {
			return domain.WorkCard{}, err
		}
		targetPath = validatedPath
	}

	codingAgent := strings.TrimSpace(in.CodingAgent)
	if codingAgent == "" {
		codingAgent = strings.TrimSpace(in.Agent)
	}
	agent := codingAgent

	reviewerMode := strings.TrimSpace(in.ReviewerMode)
	if reviewerMode == "" {
		reviewerMode = "same"
	}
	reviewerAgent := strings.TrimSpace(in.ReviewerAgent)
	testingAgent := strings.TrimSpace(in.TestingAgent)

	sessionID := strings.TrimSpace(in.SessionID)
	if err := s.ensureSessionIDAvailable(ctx, projectID, "", sessionID); err != nil {
		return domain.WorkCard{}, err
	}

	now := s.clock().UTC()
	card := domain.WorkCard{
		ID:            s.newID(),
		ProjectID:     projectID,
		ProjectName:   project.Name(),
		BoardID:       boardID,
		Title:         title,
		Notes:         strings.TrimSpace(in.Notes),
		Priority:      priority,
		Labels:        append([]string{}, labels...),
		Status:        status,
		ScheduledAt:   cloneTime(in.ScheduledAt),
		TargetPath:    targetPath,
		Agent:         agent,
		CodingAgent:   codingAgent,
		ReviewerMode:  reviewerMode,
		ReviewerAgent: reviewerAgent,
		TestingAgent:  testingAgent,
		SessionID:     sessionID,
		GoalVersion:   1,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if status == domain.CardStatusReady {
		card.ReadyAt = &now
	}
	if err := s.store.CreateWorkCard(ctx, card); err != nil {
		return domain.WorkCard{}, apierr.Internal("WORK_CARD_CREATE_FAILED", "Failed to create work card")
	}
	s.kickDispatchIfTodo(projectID, card.Status)
	return card, nil
}

// List returns a project's cards on one board.
func (s *Service) List(ctx context.Context, projectID, boardID string) ([]domain.WorkCard, error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, apierr.Invalid("WORK_CARD_PROJECT_REQUIRED", "Project is required", nil)
	}
	if strings.TrimSpace(boardID) == "" {
		boardID = defaultBoardID
	}
	cards, err := s.store.ListWorkCards(ctx, projectID, boardID)
	if err != nil {
		return nil, apierr.Internal("WORK_CARDS_LIST_FAILED", "Failed to load work cards")
	}
	if project, ok, err := s.store.GetProject(ctx, projectID); err == nil && ok {
		for i := range cards {
			cards[i].ProjectName = project.Name()
		}
	}
	return cards, nil
}

// ListAll returns cards across all projects.
func (s *Service) ListAll(ctx context.Context) ([]domain.WorkCard, error) {
	cards, err := s.store.ListAllWorkCards(ctx)
	if err != nil {
		return nil, apierr.Internal("WORK_CARDS_LIST_FAILED", "Failed to load work cards")
	}
	for i := range cards {
		if project, ok, err := s.store.GetProject(ctx, cards[i].ProjectID); err == nil && ok {
			cards[i].ProjectName = project.Name()
		}
	}
	return cards, nil
}

// ListRedo returns all Redo cycles and findings for a card.
func (s *Service) ListRedo(ctx context.Context, cardID string) ([]domain.RedoCycle, error) {
	cycles, err := s.store.ListRedoCycles(ctx, cardID)
	if err != nil {
		return nil, apierr.Internal("REDO_CYCLES_LIST_FAILED", "Failed to load redo cycles")
	}
	for i := range cycles {
		findings, err := s.store.ListRedoFindings(ctx, cycles[i].ID)
		if err == nil {
			cycles[i].Findings = findings
		}
	}
	return cycles, nil
}

// Get returns a work card by id.
func (s *Service) Get(ctx context.Context, id string) (domain.WorkCard, error) {
	card, ok, err := s.store.GetWorkCard(ctx, id)
	if err != nil {
		return domain.WorkCard{}, apierr.Internal("WORK_CARD_LOAD_FAILED", "Failed to load work card")
	}
	if !ok {
		return domain.WorkCard{}, apierr.NotFound("WORK_CARD_NOT_FOUND", "Unknown work card")
	}
	return card, nil
}

// Delete permanently removes a durable work card and its dependent history.
// Sessions are intentionally not removed: they have an independent lifecycle.
func (s *Service) Delete(ctx context.Context, id string) error {
	if _, err := s.Get(ctx, id); err != nil {
		return err
	}
	deleter, ok := s.store.(cardDeleter)
	if !ok {
		return apierr.Internal("WORK_CARD_DELETE_UNAVAILABLE", "Work card deletion is unavailable")
	}
	if err := deleter.DeleteWorkCard(ctx, id); err != nil {
		return apierr.Internal("WORK_CARD_DELETE_FAILED", "Failed to delete work card")
	}
	return nil
}

// Move updates a card's board position and status. Ready cards record the
// moment they entered the ready queue.
func (s *Service) Move(ctx context.Context, id string, status domain.CardStatus, position int64) (domain.WorkCard, error) {
	if err := domain.ValidateCardStatus(string(status)); err != nil {
		return domain.WorkCard{}, apierr.Invalid("WORK_CARD_STATUS_INVALID", err.Error(), nil)
	}
	card, err := s.Get(ctx, id)
	if err != nil {
		return domain.WorkCard{}, err
	}
	wasReady := card.Status == domain.CardStatusReady
	card.Status = status
	card.Position = position
	card.UpdatedAt = s.clock().UTC()
	if !wasReady && status == domain.CardStatusReady {
		readyAt := card.UpdatedAt
		card.ReadyAt = &readyAt
	}
	if err := s.store.UpdateWorkCard(ctx, card); err != nil {
		return domain.WorkCard{}, apierr.Internal("WORK_CARD_UPDATE_FAILED", "Failed to update work card")
	}
	s.kickDispatchIfTodo(card.ProjectID, card.Status)
	return card, nil
}

// Update applies only the supplied mutable fields to a work card.
func (s *Service) Update(ctx context.Context, id string, in UpdateInput) (domain.WorkCard, error) {
	card, err := s.Get(ctx, id)
	if err != nil {
		return domain.WorkCard{}, err
	}
	if in.Title != nil {
		card.Title = strings.TrimSpace(*in.Title)
		if card.Title == "" {
			return domain.WorkCard{}, apierr.Invalid("WORK_CARD_TITLE_REQUIRED", "Title is required", nil)
		}
	}
	if in.Notes != nil {
		card.Notes = strings.TrimSpace(*in.Notes)
		if card.Notes == "" {
			return domain.WorkCard{}, apierr.Invalid("WORK_CARD_NOTES_REQUIRED", "Notes are required", nil)
		}
	}
	if in.Priority != nil {
		if _, err := domain.ParseCardPriority(string(*in.Priority)); err != nil {
			return domain.WorkCard{}, apierr.Invalid("WORK_CARD_PRIORITY_INVALID", err.Error(), nil)
		}
		card.Priority = *in.Priority
	}
	if in.Labels != nil {
		if len(*in.Labels) == 0 {
			return domain.WorkCard{}, apierr.Invalid("WORK_CARD_LABELS_REQUIRED", "At least one label is required", nil)
		}
		card.Labels = append([]string(nil), (*in.Labels)...)
	}
	if in.Status != nil {
		if err := domain.ValidateCardStatus(string(*in.Status)); err != nil {
			return domain.WorkCard{}, apierr.Invalid("WORK_CARD_STATUS_INVALID", err.Error(), nil)
		}
		wasReady := card.Status == domain.CardStatusReady
		card.Status = *in.Status
		if !wasReady && card.Status == domain.CardStatusReady {
			readyAt := s.clock().UTC()
			card.ReadyAt = &readyAt
		}
	}
	if in.ScheduledAt.Set {
		card.ScheduledAt = cloneTime(in.ScheduledAt.Value)
	}
	if in.TargetPath != nil {
		targetPath, err := s.validateTargetPath(ctx, card.ProjectID, *in.TargetPath)
		if err != nil {
			return domain.WorkCard{}, err
		}
		card.TargetPath = targetPath
	}
	if in.Agent != nil {
		card.Agent = strings.TrimSpace(*in.Agent)
		if card.Agent == "" {
			return domain.WorkCard{}, apierr.Invalid("WORK_CARD_AGENT_REQUIRED", "Agent is required", nil)
		}
	}
	if in.SessionID != nil {
		sessionID := strings.TrimSpace(*in.SessionID)
		if err := s.ensureSessionIDAvailable(ctx, card.ProjectID, card.ID, sessionID); err != nil {
			return domain.WorkCard{}, err
		}
		card.SessionID = sessionID
	}
	if in.Position != nil {
		card.Position = *in.Position
	}
	card.UpdatedAt = s.clock().UTC()
	if err := s.store.UpdateWorkCard(ctx, card); err != nil {
		return domain.WorkCard{}, apierr.Internal("WORK_CARD_UPDATE_FAILED", "Failed to update work card")
	}
	s.kickDispatchIfTodo(card.ProjectID, card.Status)
	return card, nil
}

// kickDispatchIfTodo wakes the per-project dispatcher when a durable change may
// have added eligible work to the Todo/Ready queue.
func (s *Service) kickDispatchIfTodo(projectID string, status domain.CardStatus) {
	if status != domain.CardStatusTodo && status != domain.CardStatusReady {
		return
	}
	if s.dispatchKicker == nil {
		return
	}
	s.dispatchKicker.Kick(projectID)
}

func (s *Service) validateTargetPath(ctx context.Context, projectID, targetPath string) (string, error) {
	path := filepath.Clean(strings.TrimSpace(targetPath))
	if targetPath == "" || !filepath.IsAbs(path) {
		return "", apierr.Invalid("WORK_CARD_TARGET_PATH_INVALID", "Target path must be an absolute path", nil)
	}
	roots, err := s.repoRoots(ctx, projectID)
	if err != nil {
		return "", err
	}
	if err := domain.ValidateTargetPathUnderRepos(path, roots); err != nil {
		return "", apierr.Invalid("WORK_CARD_TARGET_PATH_INVALID", err.Error(), nil)
	}
	return path, nil
}

func (s *Service) ensureSessionIDAvailable(ctx context.Context, projectID, cardID, sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}
	cards, err := s.store.ListWorkCards(ctx, projectID, defaultBoardID)
	if err != nil {
		return apierr.Internal("WORK_CARDS_LIST_FAILED", "Failed to load work cards")
	}
	for _, other := range cards {
		if other.ID == cardID {
			continue
		}
		if other.SessionID == sessionID {
			return apierr.Conflict("WORK_CARD_SESSION_IN_USE", "Session is already linked to another work card", map[string]any{
				"sessionId": sessionID,
				"cardId":    other.ID,
			})
		}
	}
	return nil
}

func (s *Service) repoRoots(ctx context.Context, projectID string) ([]string, error) {
	project, ok, err := s.store.GetProject(ctx, projectID)
	if err != nil {
		return nil, apierr.Internal("PROJECT_LOAD_FAILED", "Failed to load project")
	}
	if !ok || !project.ArchivedAt.IsZero() {
		return nil, apierr.NotFound("PROJECT_NOT_FOUND", "Unknown project")
	}
	roots := []string{project.Path}
	if project.Kind.WithDefault() != domain.ProjectKindWorkspace {
		return roots, nil
	}
	repos, err := s.store.ListWorkspaceRepos(ctx, projectID)
	if err != nil {
		return nil, apierr.Internal("PROJECT_LOAD_FAILED", "Failed to load workspace repositories")
	}
	for _, repo := range repos {
		roots = append(roots, filepath.Join(project.Path, repo.RelativePath))
	}
	return roots, nil
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := value.UTC()
	return &cloned
}
