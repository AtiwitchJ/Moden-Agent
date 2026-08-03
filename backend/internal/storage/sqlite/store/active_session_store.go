package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/modernagent/modern-agent/backend/internal/storage/sqlite/gen"
)

// ActiveSessionRow is the store's plain projection of an active_session row.
// It is deliberately storage-native (no commander/* types) so this package
// never has to import commander — callers that need commander/orchestrator's
// ActiveSessionRecord convert at their own boundary.
type ActiveSessionRow struct {
	CardID    string
	SessionID string
	Phase     string
	Agent     string
	CreatedAt time.Time
}

// GetActiveSession returns the active session linked to a card, or ok=false
// when the card has none.
func (s *Store) GetActiveSession(ctx context.Context, cardID string) (ActiveSessionRow, bool, error) {
	row, err := s.qr.GetActiveSession(ctx, cardID)
	if errors.Is(err, sql.ErrNoRows) {
		return ActiveSessionRow{}, false, nil
	}
	if err != nil {
		return ActiveSessionRow{}, false, fmt.Errorf("get active session for card %s: %w", cardID, err)
	}
	return ActiveSessionRow{
		CardID:    row.CardID,
		SessionID: row.SessionID,
		Phase:     row.Phase,
		Agent:     row.Agent,
		CreatedAt: time.UnixMilli(row.CreatedAt),
	}, true, nil
}

// InsertActiveSession records a (card_id -> session) fact. active_session.card_id
// is unique, so a second insert for a card that already has one fails.
func (s *Store) InsertActiveSession(ctx context.Context, cardID, sessionID, phase, agent string, at time.Time) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := s.qw.InsertActiveSession(ctx, gen.InsertActiveSessionParams{
		CardID:    cardID,
		SessionID: sessionID,
		Phase:     phase,
		Agent:     agent,
		CreatedAt: at.UnixMilli(),
	}); err != nil {
		return fmt.Errorf("insert active session for card %s: %w", cardID, err)
	}
	return nil
}

// DeleteActiveSession removes the active-session fact for a card. It is a
// no-op (not an error) when the card has none.
func (s *Store) DeleteActiveSession(ctx context.Context, cardID string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := s.qw.DeleteActiveSession(ctx, cardID); err != nil {
		return fmt.Errorf("delete active session for card %s: %w", cardID, err)
	}
	return nil
}
