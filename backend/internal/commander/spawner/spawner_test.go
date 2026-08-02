package spawner_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/modernagent/modern-agent/backend/internal/adapters/agent/registry"
	"github.com/modernagent/modern-agent/backend/internal/commander/spawner"
	"github.com/modernagent/modern-agent/backend/internal/domain"
)

// fakeLauncher stores every Spawn call and returns configured handles.
type fakeLauncher struct {
	Calls   []spawner.SpawnSpec // specs observed by the launcher
	Handle  spawner.SessionHandle
	SpawnErr error
}

func (f *fakeLauncher) Spawn(ctx context.Context, spec spawner.SpawnSpec) (spawner.SessionHandle, error) {
	f.Calls = append(f.Calls, spec)
	return f.Handle, f.SpawnErr
}

// fakeStore records InsertActiveSession calls in memory.
type fakeStore struct {
	mu      sync.Mutex
	Inserts []spawner.InsertActiveSession
}

func (f *fakeStore) InsertActiveSession(ctx context.Context, s spawner.InsertActiveSession) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Inserts = append(f.Inserts, s)
	return nil
}

// concurrentStore is a thread-safe store that rejects duplicate card IDs.
type concurrentStore struct {
	mu      sync.Mutex
	active  map[string]struct{}
}

func (c *concurrentStore) InsertActiveSession(ctx context.Context, s spawner.InsertActiveSession) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.active[s.CardID]; exists {
		return errors.New("active_session for this card already exists")
	}
	c.active[s.CardID] = struct{}{}
	return nil
}

func makeCard(id, projectID, title, notes, path string) *domain.WorkCard {
	return &domain.WorkCard{
		ID: id, ProjectID: projectID, Title: title, Notes: notes,
		TargetPath: path, Priority: domain.CardPriorityNormal,
		Status: domain.CardStatusRunning,
	}
}

func TestSpawn_Hermes(t *testing.T) {
	t.Parallel()
	reg, err := registry.Build()
	require.NoError(t, err)

	fl := &fakeLauncher{Handle: spawner.SessionHandle{ID: "sess-1"}}
	store := &fakeStore{}
	s := spawner.New(fl, store, reg, func() int64 { return 1234567890 }, func() string { return "id-1" })

	ctx := context.Background()
	card := makeCard("card-1", "proj-1", "Fix bug", "Fix the login bug", "/repo/src")
	spec := spawner.SpawnSpec{
		CardID: "card-1", ProjectID: "proj-1",
		Phase: spawner.PhaseCoding, Agent: "hermes",
		Briefing: "hello hermes",
		ParentCard: card,
	}

	handle, err := s.Spawn(ctx, spec)
	require.NoError(t, err)
	assert.Equal(t, "sess-1", handle.ID)

	require.Len(t, fl.Calls, 1)
	assert.Equal(t, "hermes", fl.Calls[0].Agent)
	assert.Equal(t, spawner.PhaseCoding, fl.Calls[0].Phase)

	require.Len(t, store.Inserts, 1)
	assert.Equal(t, "card-1", store.Inserts[0].CardID)
	assert.Equal(t, "sess-1", store.Inserts[0].SessionID)
	assert.Equal(t, spawner.PhaseCoding, store.Inserts[0].Phase)
	assert.Equal(t, "hermes", store.Inserts[0].Agent)
}

func TestSpawn_ClaudeCode(t *testing.T) {
	t.Parallel()
	reg, err := registry.Build()
	require.NoError(t, err)

	fl := &fakeLauncher{Handle: spawner.SessionHandle{ID: "sess-2"}}
	store := &fakeStore{}
	s := spawner.New(fl, store, reg, func() int64 { return 1234567890 }, func() string { return "id-2" })

	ctx := context.Background()
	card := makeCard("card-2", "proj-1", "Add feature", "Add dark mode", "/repo")
	spec := spawner.SpawnSpec{
		CardID: "card-2", ProjectID: "proj-1",
		Phase: spawner.PhaseReview, Agent: "claude-code",
		Briefing: "review the PR",
		ParentCard: card,
	}

	handle, err := s.Spawn(ctx, spec)
	require.NoError(t, err)
	assert.Equal(t, "sess-2", handle.ID)
	require.Len(t, fl.Calls, 1)
	assert.Equal(t, "claude-code", fl.Calls[0].Agent)
	assert.Equal(t, spawner.PhaseReview, fl.Calls[0].Phase)
}

func TestSpawn_UnknownAgent(t *testing.T) {
	t.Parallel()
	reg, err := registry.Build()
	require.NoError(t, err)

	fl := &fakeLauncher{Handle: spawner.SessionHandle{ID: "sess-3"}}
	store := &fakeStore{}
	s := spawner.New(fl, store, reg, func() int64 { return 1234567890 }, func() string { return "id-3" })

	ctx := context.Background()
	spec := spawner.SpawnSpec{
		CardID: "card-3", ProjectID: "proj-1",
		Phase: spawner.PhaseCoding, Agent: "nonexistent-agent",
		Briefing: "oops",
		ParentCard: makeCard("card-3", "proj-1", "", "", ""),
	}

	_, err = s.Spawn(ctx, spec)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestSpawn_ConcurrentSameCard(t *testing.T) {
	t.Parallel()
	reg, err := registry.Build()
	require.NoError(t, err)

	// concurrentStore rejects a second insert for the same card — this
	// simulates the DB unique-constraint enforcement described in the plan.
	store := &concurrentStore{active: make(map[string]struct{})}
	fl := &fakeLauncher{Handle: spawner.SessionHandle{ID: "sess-x"}}
	s := spawner.New(fl, store, reg, func() int64 { return 1234567890 }, func() string { return "id-x" })

	ctx := context.Background()
	card := makeCard("card-concurrent", "proj-1", "Concurrent", "", "/repo")

	// First spawn succeeds.
	spec1 := spawner.SpawnSpec{
		CardID: "card-concurrent", ProjectID: "proj-1",
		Phase: spawner.PhaseCoding, Agent: "hermes",
		Briefing: "first",
		ParentCard: card,
	}
	h1, err := s.Spawn(ctx, spec1)
	require.NoError(t, err)
	assert.NotEmpty(t, h1.ID)

	// Second concurrent spawn for same card fails — concurrentStore rejects duplicate.
	spec2 := spawner.SpawnSpec{
		CardID: "card-concurrent", ProjectID: "proj-1",
		Phase: spawner.PhaseCoding, Agent: "hermes",
		Briefing: "second",
		ParentCard: card,
	}
	_, err = s.Spawn(ctx, spec2)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestSpawn_LancherError(t *testing.T) {
	t.Parallel()
	reg, err := registry.Build()
	require.NoError(t, err)

	fl := &fakeLauncher{
		Handle:  spawner.SessionHandle{ID: "sess-e"},
		SpawnErr: errors.New("binary not found"),
	}
	store := &fakeStore{}
	s := spawner.New(fl, store, reg, func() int64 { return 1234567890 }, func() string { return "id-e" })

	ctx := context.Background()
	spec := spawner.SpawnSpec{
		CardID: "card-e", ProjectID: "proj-1",
		Phase: spawner.PhaseCoding, Agent: "hermes",
		Briefing: "will fail",
		ParentCard: makeCard("card-e", "proj-1", "", "", ""),
	}

	_, err = s.Spawn(ctx, spec)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "binary not found")
	// Store was NOT called because launcher failed before InsertActiveSession.
	assert.Empty(t, store.Inserts)
}
