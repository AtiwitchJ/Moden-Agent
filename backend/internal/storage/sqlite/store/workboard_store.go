package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/modernagent/modern-agent/backend/internal/domain"
	"github.com/modernagent/modern-agent/backend/internal/storage/sqlite/gen"
)

// CreateWorkCard persists a new work card. Identity and audit fields are
// assigned by the service; this store only maps the domain record to sqlc.
func (s *Store) CreateWorkCard(ctx context.Context, card domain.WorkCard) error {
	params, err := workCardInsertParams(card)
	if err != nil {
		return err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := s.qw.InsertWorkCard(ctx, params); err != nil {
		return fmt.Errorf("insert work card %s: %w", card.ID, err)
	}
	return nil
}

// GetWorkCard returns a work card by id, or ok=false when it is absent.
func (s *Store) GetWorkCard(ctx context.Context, id string) (domain.WorkCard, bool, error) {
	row, err := s.qr.GetWorkCard(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.WorkCard{}, false, nil
	}
	if err != nil {
		return domain.WorkCard{}, false, fmt.Errorf("get work card %s: %w", id, err)
	}
	card, err := workCardFromRow(row)
	if err != nil {
		return domain.WorkCard{}, false, fmt.Errorf("decode work card %s: %w", id, err)
	}
	return card, true, nil
}

// ListWorkCards returns a board's cards in the query's stable board order.
func (s *Store) ListWorkCards(ctx context.Context, projectID, boardID string) ([]domain.WorkCard, error) {
	rows, err := s.qr.ListWorkCardsByProject(ctx, gen.ListWorkCardsByProjectParams{
		ProjectID: projectID,
		BoardID:   boardID,
	})
	if err != nil {
		return nil, fmt.Errorf("list work cards for project %s board %s: %w", projectID, boardID, err)
	}
	cards := make([]domain.WorkCard, 0, len(rows))
	for _, row := range rows {
		card, err := workCardFromRow(row)
		if err != nil {
			return nil, fmt.Errorf("decode work card %s: %w", row.ID, err)
		}
		cards = append(cards, card)
	}
	return cards, nil
}

// ListAllWorkCards returns cards across all projects in stable order.
func (s *Store) ListAllWorkCards(ctx context.Context) ([]domain.WorkCard, error) {
	rows, err := s.qr.ListAllWorkCards(ctx)
	if err != nil {
		return nil, fmt.Errorf("list all work cards: %w", err)
	}
	cards := make([]domain.WorkCard, 0, len(rows))
	for _, row := range rows {
		card, err := workCardFromRow(row)
		if err != nil {
			return nil, fmt.Errorf("decode work card %s: %w", row.ID, err)
		}
		cards = append(cards, card)
	}
	return cards, nil
}

// UpdateWorkCard writes the mutable state of an existing card. Its identity,
// project, board, and creation time remain untouched by the SQL query.
func (s *Store) UpdateWorkCard(ctx context.Context, card domain.WorkCard) error {
	params, err := workCardUpdateParams(card)
	if err != nil {
		return err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := s.qw.UpdateWorkCard(ctx, params); err != nil {
		return fmt.Errorf("update work card %s: %w", card.ID, err)
	}
	return nil
}

// DeleteWorkCard permanently removes a card. SQLite cascades its dependent
// work-card events and redo history through the schema's foreign keys.
func (s *Store) DeleteWorkCard(ctx context.Context, id string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := s.qw.DeleteWorkCard(ctx, id); err != nil {
		return fmt.Errorf("delete work card %s: %w", id, err)
	}
	return nil
}

// ClaimReadyWorkCard atomically transitions one ready card to running when it
// is still eligible and the project's durable running-card count is below the
// supplied WIP limit. The returned flag reports whether this dispatcher won
// the claim; callers must not start a worker when it is false.
func (s *Store) ClaimReadyWorkCard(ctx context.Context, cardID, projectID string, wipLimit int, at time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	n, err := s.qw.ClaimReadyWorkCard(ctx, gen.ClaimReadyWorkCardParams{
		UpdatedAt: at.UnixMilli(),
		CardID:    cardID,
		ProjectID: projectID,
		WipLimit:  int64(wipLimit),
	})
	if err != nil {
		return false, fmt.Errorf("claim ready work card %s: %w", cardID, err)
	}
	return n > 0, nil
}

// AppendWorkCardEvent records an immutable work-card audit event.
func (s *Store) AppendWorkCardEvent(ctx context.Context, event domain.WorkCardEvent) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := s.qw.InsertWorkCardEvent(ctx, gen.InsertWorkCardEventParams{
		ID:        event.ID,
		CardID:    event.CardID,
		ProjectID: event.ProjectID,
		Kind:      event.Kind,
		Payload:   event.Payload,
		CreatedAt: event.CreatedAt.UnixMilli(),
	}); err != nil {
		return fmt.Errorf("insert work card event %s: %w", event.ID, err)
	}
	return nil
}

// PrepareHermesAnswerAttempt records a Hermes send attempt and, when requested,
// consumes the non-sticky autonomous override in the same transaction. A
// one-shot override is therefore never durably spent without an attempt that
// owns the decision in its payload. expectedConfig makes the authorization
// decision conditional on the current persisted workboard config.
func (s *Store) PrepareHermesAnswerAttempt(ctx context.Context, projectID string, expectedConfig domain.WorkboardConfig, event domain.WorkCardEvent, consumeOneShot bool) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	prepared := false
	err := s.inTx(ctx, "prepare Hermes answer attempt", func(q *gen.Queries) error {
		project, err := q.GetProject(ctx, domain.ProjectID(projectID))
		if err != nil {
			return err
		}
		config := unmarshalProjectConfig(project.Config)
		if !reflect.DeepEqual(config.Workboard, expectedConfig) {
			return nil
		}
		if consumeOneShot {
			if !config.Workboard.Autonomous.Enabled || config.Workboard.Autonomous.Sticky {
				return nil
			}
			config.Workboard.Autonomous.Enabled = false
			encoded, err := marshalProjectConfig(config)
			if err != nil {
				return err
			}
			if err := q.UpdateProjectConfig(ctx, gen.UpdateProjectConfigParams{Config: encoded, ID: domain.ProjectID(projectID)}); err != nil {
				return err
			}
		}
		if err := q.InsertWorkCardEvent(ctx, gen.InsertWorkCardEventParams{
			ID:        event.ID,
			CardID:    event.CardID,
			ProjectID: event.ProjectID,
			Kind:      event.Kind,
			Payload:   event.Payload,
			CreatedAt: event.CreatedAt.UnixMilli(),
		}); err != nil {
			return err
		}
		prepared = true
		return nil
	})
	return prepared, err
}

// ListWorkCardEvents returns a card's immutable audit facts in creation order.
func (s *Store) ListWorkCardEvents(ctx context.Context, cardID string) ([]domain.WorkCardEvent, error) {
	rows, err := s.qr.ListWorkCardEventsByCard(ctx, cardID)
	if err != nil {
		return nil, fmt.Errorf("list work card events for %s: %w", cardID, err)
	}
	events := make([]domain.WorkCardEvent, 0, len(rows))
	for _, row := range rows {
		events = append(events, domain.WorkCardEvent{
			ID:        row.ID,
			CardID:    row.CardID,
			ProjectID: row.ProjectID,
			Kind:      row.Kind,
			Payload:   row.Payload,
			CreatedAt: time.UnixMilli(row.CreatedAt).UTC(),
		})
	}
	return events, nil
}

func workCardFromRow(row gen.WorkCard) (domain.WorkCard, error) {
	var labels []string
	if row.LabelsJson != "" {
		_ = json.Unmarshal([]byte(row.LabelsJson), &labels)
	}
	if labels == nil {
		labels = []string{}
	}
	return domain.WorkCard{
		ID:                 row.ID,
		ProjectID:          row.ProjectID,
		BoardID:            row.BoardID,
		Title:              row.Title,
		Notes:              row.Notes,
		Priority:           domain.CardPriority(row.Priority),
		Labels:             labels,
		Status:             domain.CardStatus(row.Status),
		ScheduledAt:        timeFromMillis(row.ScheduledAt),
		ReadyAt:            timeFromMillis(row.ReadyAt),
		Position:           row.Position,
		TargetPath:         row.TargetPath,
		RepoName:           row.RepoName,
		Agent:              row.Agent,
		CodingAgent:        row.CodingAgent,
		ReviewerMode:       row.ReviewerMode,
		ReviewerAgent:      row.ReviewerAgent,
		TestingAgent:       row.TestingAgent,
		RedoCount:          int(row.RedoCount),
		LatestRedoSummary:  row.LatestRedoSummary,
		SessionID:          row.SessionID,
		WaitingForInput:    row.WaitingForInput != 0,
		PausedRetarget:     row.PausedRetarget != 0,
		GoalVersion:        int(row.GoalVersion),
		SupersededByCardID: row.SupersededByCardID,
		CreatedAt:          time.UnixMilli(row.CreatedAt).UTC(),
		UpdatedAt:          time.UnixMilli(row.UpdatedAt).UTC(),
	}, nil
}

func workCardInsertParams(card domain.WorkCard) (gen.InsertWorkCardParams, error) {
	labels, err := json.Marshal(card.Labels)
	if err != nil {
		return gen.InsertWorkCardParams{}, fmt.Errorf("marshal work card labels: %w", err)
	}
	return gen.InsertWorkCardParams{
		ID:                 card.ID,
		ProjectID:          card.ProjectID,
		BoardID:            card.BoardID,
		Title:              card.Title,
		Notes:              card.Notes,
		Priority:           string(card.Priority),
		LabelsJson:         string(labels),
		Status:             string(card.Status),
		ScheduledAt:        millisFromTime(card.ScheduledAt),
		ReadyAt:            millisFromTime(card.ReadyAt),
		Position:           card.Position,
		TargetPath:         card.TargetPath,
		RepoName:           card.RepoName,
		Agent:              card.Agent,
		CodingAgent:        card.CodingAgent,
		ReviewerMode:       card.ReviewerMode,
		ReviewerAgent:      card.ReviewerAgent,
		TestingAgent:       card.TestingAgent,
		RedoCount:          int64(card.RedoCount),
		LatestRedoSummary:  card.LatestRedoSummary,
		SessionID:          card.SessionID,
		WaitingForInput:    boolToInt64(card.WaitingForInput),
		PausedRetarget:     boolToInt64(card.PausedRetarget),
		GoalVersion:        int64(card.GoalVersion),
		SupersededByCardID: card.SupersededByCardID,
		CreatedAt:          card.CreatedAt.UnixMilli(),
		UpdatedAt:          card.UpdatedAt.UnixMilli(),
	}, nil
}

func workCardUpdateParams(card domain.WorkCard) (gen.UpdateWorkCardParams, error) {
	labels, err := json.Marshal(card.Labels)
	if err != nil {
		return gen.UpdateWorkCardParams{}, fmt.Errorf("marshal work card labels: %w", err)
	}
	return gen.UpdateWorkCardParams{
		ID:                 card.ID,
		Title:              card.Title,
		Notes:              card.Notes,
		Priority:           string(card.Priority),
		LabelsJson:         string(labels),
		Status:             string(card.Status),
		ScheduledAt:        millisFromTime(card.ScheduledAt),
		ReadyAt:            millisFromTime(card.ReadyAt),
		Position:           card.Position,
		TargetPath:         card.TargetPath,
		RepoName:           card.RepoName,
		Agent:              card.Agent,
		CodingAgent:        card.CodingAgent,
		ReviewerMode:       card.ReviewerMode,
		ReviewerAgent:      card.ReviewerAgent,
		TestingAgent:       card.TestingAgent,
		RedoCount:          int64(card.RedoCount),
		LatestRedoSummary:  card.LatestRedoSummary,
		SessionID:          card.SessionID,
		WaitingForInput:    boolToInt64(card.WaitingForInput),
		PausedRetarget:     boolToInt64(card.PausedRetarget),
		GoalVersion:        int64(card.GoalVersion),
		SupersededByCardID: card.SupersededByCardID,
		UpdatedAt:          card.UpdatedAt.UnixMilli(),
	}, nil
}

// InsertRedoCycle persists a new Redo cycle for a work card.
func (s *Store) InsertRedoCycle(ctx context.Context, cycle domain.RedoCycle) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	var completedAt sql.NullInt64
	if cycle.CompletedAt != nil {
		completedAt = sql.NullInt64{Int64: cycle.CompletedAt.UnixMilli(), Valid: true}
	}
	return s.qw.InsertRedoCycle(ctx, gen.InsertRedoCycleParams{
		ID:          cycle.ID,
		CardID:      cycle.CardID,
		CycleNumber: int64(cycle.CycleNumber),
		Source:      cycle.Source,
		Summary:     cycle.Summary,
		CreatedAt:   cycle.CreatedAt.UnixMilli(),
		CompletedAt: completedAt,
	})
}

// GetLatestRedoCycle returns the latest Redo cycle for a card.
func (s *Store) GetLatestRedoCycle(ctx context.Context, cardID string) (domain.RedoCycle, bool, error) {
	row, err := s.qr.GetLatestRedoCycle(ctx, cardID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.RedoCycle{}, false, nil
	}
	if err != nil {
		return domain.RedoCycle{}, false, err
	}
	cycle := domain.RedoCycle{
		ID:          row.ID,
		CardID:      row.CardID,
		CycleNumber: int(row.CycleNumber),
		Source:      row.Source,
		Summary:     row.Summary,
		CreatedAt:   time.UnixMilli(row.CreatedAt).UTC(),
		CompletedAt: timeFromMillis(row.CompletedAt),
	}
	return cycle, true, nil
}

// ListRedoCycles returns all Redo cycles for a card ordered by cycle_number.
func (s *Store) ListRedoCycles(ctx context.Context, cardID string) ([]domain.RedoCycle, error) {
	rows, err := s.qr.ListRedoCyclesByCard(ctx, cardID)
	if err != nil {
		return nil, err
	}
	cycles := make([]domain.RedoCycle, 0, len(rows))
	for _, row := range rows {
		// Findings are read back per cycle rather than joined in the SQL
		// above: ListRedoFindings already owns unmarshaling FileRefsJSON, and
		// a redo cycle rarely has more than a handful of findings, so a
		// second small query per cycle is simpler than duplicating that
		// unmarshal here.
		findings, err := s.ListRedoFindings(ctx, row.ID)
		if err != nil {
			return nil, fmt.Errorf("list findings for redo cycle %s: %w", row.ID, err)
		}
		cycles = append(cycles, domain.RedoCycle{
			ID:          row.ID,
			CardID:      row.CardID,
			CycleNumber: int(row.CycleNumber),
			Source:      row.Source,
			Summary:     row.Summary,
			Findings:    findings,
			CreatedAt:   time.UnixMilli(row.CreatedAt).UTC(),
			CompletedAt: timeFromMillis(row.CompletedAt),
		})
	}
	return cycles, nil
}

// CompleteRedoCycle marks a Redo cycle as completed.
func (s *Store) CompleteRedoCycle(ctx context.Context, cycleID string, completedAt time.Time) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.qw.CompleteRedoCycle(ctx, gen.CompleteRedoCycleParams{
		ID:          cycleID,
		CompletedAt: sql.NullInt64{Int64: completedAt.UnixMilli(), Valid: true},
	})
}

// InsertRedoFinding records a finding in a Redo cycle.
func (s *Store) InsertRedoFinding(ctx context.Context, finding domain.RedoFinding) error {
	fileRefsJSON, err := json.Marshal(finding.FileRefs)
	if err != nil {
		return fmt.Errorf("marshal file refs: %w", err)
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.qw.InsertRedoFinding(ctx, gen.InsertRedoFindingParams{
		ID:           finding.ID,
		CycleID:      finding.CycleID,
		Sequence:     int64(finding.Sequence),
		Severity:     string(finding.Severity),
		Title:        finding.Title,
		Details:      finding.Details,
		Command:      finding.Command,
		ErrorOutput:  finding.ErrorOutput,
		FileRefsJson: string(fileRefsJSON),
		Status:       string(finding.Status),
		AttemptCount: int64(finding.AttemptCount),
		CreatedAt:    finding.CreatedAt.UnixMilli(),
		UpdatedAt:    finding.UpdatedAt.UnixMilli(),
	})
}

// ListRedoFindings returns all findings in a Redo cycle ordered by sequence.
func (s *Store) ListRedoFindings(ctx context.Context, cycleID string) ([]domain.RedoFinding, error) {
	rows, err := s.qr.ListRedoFindingsByCycle(ctx, cycleID)
	if err != nil {
		return nil, err
	}
	findings := make([]domain.RedoFinding, 0, len(rows))
	for _, row := range rows {
		var fileRefs []domain.FileRef
		if row.FileRefsJson != "" {
			_ = json.Unmarshal([]byte(row.FileRefsJson), &fileRefs)
		}
		findings = append(findings, domain.RedoFinding{
			ID:           row.ID,
			CycleID:      row.CycleID,
			Sequence:     int(row.Sequence),
			Severity:     domain.FindingSeverity(row.Severity),
			Title:        row.Title,
			Details:      row.Details,
			Command:      row.Command,
			ErrorOutput:  row.ErrorOutput,
			FileRefs:     fileRefs,
			Status:       domain.FindingStatus(row.Status),
			AttemptCount: int(row.AttemptCount),
			CreatedAt:    time.UnixMilli(row.CreatedAt).UTC(),
			UpdatedAt:    time.UnixMilli(row.UpdatedAt).UTC(),
		})
	}
	return findings, nil
}

// UpdateRedoFindingStatus updates status and increments attempt count for a finding.
func (s *Store) UpdateRedoFindingStatus(ctx context.Context, findingID string, status domain.FindingStatus, addAttempts int, at time.Time) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.qw.UpdateRedoFindingStatus(ctx, gen.UpdateRedoFindingStatusParams{
		ID:           findingID,
		Status:       string(status),
		AttemptCount: int64(addAttempts),
		UpdatedAt:    at.UnixMilli(),
	})
}

// InsertRedoAttempt records an attempt execution for a finding.
func (s *Store) InsertRedoAttempt(ctx context.Context, attempt domain.RedoAttempt) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	var finishedAt sql.NullInt64
	if attempt.FinishedAt != nil {
		finishedAt = sql.NullInt64{Int64: attempt.FinishedAt.UnixMilli(), Valid: true}
	}
	return s.qw.InsertRedoAttempt(ctx, gen.InsertRedoAttemptParams{
		ID:             attempt.ID,
		FindingID:      attempt.FindingID,
		AttemptNumber:  int64(attempt.AttemptNumber),
		Agent:          attempt.Agent,
		StartedAt:      attempt.StartedAt.UnixMilli(),
		FinishedAt:     finishedAt,
		Result:         attempt.Result,
		Output:         attempt.Output,
		ValidationJson: attempt.ValidationJSON,
	})
}

// ListRedoAttempts returns all attempt records for a finding.
func (s *Store) ListRedoAttempts(ctx context.Context, findingID string) ([]domain.RedoAttempt, error) {
	rows, err := s.qr.ListRedoAttemptsByFinding(ctx, findingID)
	if err != nil {
		return nil, err
	}
	attempts := make([]domain.RedoAttempt, 0, len(rows))
	for _, row := range rows {
		attempts = append(attempts, domain.RedoAttempt{
			ID:             row.ID,
			FindingID:      row.FindingID,
			AttemptNumber:  int(row.AttemptNumber),
			Agent:          row.Agent,
			StartedAt:      time.UnixMilli(row.StartedAt).UTC(),
			FinishedAt:     timeFromMillis(row.FinishedAt),
			Result:         row.Result,
			Output:         row.Output,
			ValidationJSON: row.ValidationJson,
		})
	}
	return attempts, nil
}

func millisFromTime(t *time.Time) sql.NullInt64 {
	if t == nil || t.IsZero() {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: t.UnixMilli(), Valid: true}
}

func timeFromMillis(value sql.NullInt64) *time.Time {
	if !value.Valid {
		return nil
	}
	t := time.UnixMilli(value.Int64).UTC()
	return &t
}

func boolToInt64(value bool) int64 {
	if value {
		return 1
	}
	return 0
}
