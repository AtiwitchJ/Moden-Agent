package spawner_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/modernagent/modern-agent/backend/internal/adapters/agent/registry"
	"github.com/modernagent/modern-agent/backend/internal/commander/spawner"
	"github.com/modernagent/modern-agent/backend/internal/domain"
	"github.com/modernagent/modern-agent/backend/internal/ports"
)

// fakeLauncher stores every Spawn call and returns configured handles.
type fakeLauncher struct {
	mu       sync.Mutex
	Calls    []spawner.SpawnSpec // specs observed by the launcher
	Handle   spawner.SessionHandle
	SpawnErr error
	Started  chan<- struct{}
	Block    <-chan struct{}
}

func (f *fakeLauncher) Spawn(ctx context.Context, spec spawner.SpawnSpec) (spawner.SessionHandle, error) {
	f.mu.Lock()
	f.Calls = append(f.Calls, spec)
	handle, spawnErr := f.Handle, f.SpawnErr
	started, block := f.Started, f.Block
	f.mu.Unlock()
	if started != nil {
		select {
		case started <- struct{}{}:
		default:
		}
	}
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return spawner.SessionHandle{}, ctx.Err()
		}
	}
	return handle, spawnErr
}

func (f *fakeLauncher) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.Calls)
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
		Briefing:   "hello hermes",
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
		Briefing:   "review the PR",
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
		Briefing:   "oops",
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

	store := &fakeStore{}
	started := make(chan struct{}, 1)
	unblock := make(chan struct{})
	fl := &fakeLauncher{
		Handle:  spawner.SessionHandle{ID: "sess-x"},
		Started: started,
		Block:   unblock,
	}
	s := spawner.New(fl, store, reg, func() int64 { return 1234567890 }, func() string { return "id-x" })

	ctx := context.Background()
	card := makeCard("card-concurrent", "proj-1", "Concurrent", "", "/repo")
	spec := spawner.SpawnSpec{
		CardID: "card-concurrent", ProjectID: "proj-1",
		Phase: spawner.PhaseCoding, Agent: "hermes",
		Briefing:   "concurrent",
		ParentCard: card,
	}

	firstResult := make(chan error, 1)
	go func() {
		_, err := s.Spawn(ctx, spec)
		firstResult <- err
	}()
	require.Eventually(t, func() bool {
		select {
		case <-started:
			return true
		default:
			return false
		}
	}, time.Second, time.Millisecond)

	_, err = s.Spawn(ctx, spec)
	require.ErrorIs(t, err, spawner.ErrCardSpawnInProgress)
	assert.Equal(t, 1, fl.callCount(), "the rejected spawn must not reach the launcher")

	close(unblock)
	require.NoError(t, <-firstResult)
	require.Len(t, store.Inserts, 1)
}

func TestSpawn_ConcurrentDifferentCards(t *testing.T) {
	t.Parallel()
	reg, err := registry.Build()
	require.NoError(t, err)

	const cardCount = 8
	store := &fakeStore{}
	fl := &fakeLauncher{Handle: spawner.SessionHandle{ID: "sess-parallel"}}
	s := spawner.New(fl, store, reg, func() int64 { return 1234567890 }, func() string { return "id-parallel" })

	start := make(chan struct{})
	errs := make(chan error, cardCount)
	var wg sync.WaitGroup
	for i := 0; i < cardCount; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, err := s.Spawn(context.Background(), spawner.SpawnSpec{
				CardID:     fmt.Sprintf("card-%d", i),
				ProjectID:  "proj-1",
				Phase:      spawner.PhaseCoding,
				Agent:      "hermes",
				ParentCard: makeCard(fmt.Sprintf("card-%d", i), "proj-1", "Parallel", "", "/repo"),
			})
			errs <- err
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}
	assert.Equal(t, cardCount, fl.callCount())
	require.Len(t, store.Inserts, cardCount)
	seen := make(map[string]struct{}, cardCount)
	for _, insert := range store.Inserts {
		seen[insert.CardID] = struct{}{}
	}
	assert.Len(t, seen, cardCount)
}

// fakeSessionSpawner stores every Spawn call and returns configured session/error.
type fakeSessionSpawner struct {
	calls []ports.SpawnConfig
	out   domain.Session
	err   error
}

func (f *fakeSessionSpawner) Spawn(_ context.Context, cfg ports.SpawnConfig) (domain.Session, error) {
	f.calls = append(f.calls, cfg)
	return f.out, f.err
}

func TestSessionServiceLauncher_SpawnsRealSession(t *testing.T) {
	t.Parallel()
	sessions := &fakeSessionSpawner{out: domain.Session{
		SessionRecord: domain.SessionRecord{ID: "sess-real-1"},
	}}
	launcher := &spawner.SessionServiceLauncher{Sessions: sessions}

	card := makeCard("card-1", "proj-1", "Fix bug", "details", "/repo/app")
	handle, err := launcher.Spawn(context.Background(), spawner.SpawnSpec{
		CardID: "card-1", ProjectID: "proj-1", Phase: spawner.PhaseCoding,
		Agent: "hermes", Briefing: "do the work", ParentCard: card,
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if handle.ID != "sess-real-1" {
		t.Fatalf("handle.ID = %q, want sess-real-1 (the real session ID, not the card ID)", handle.ID)
	}
	if len(sessions.calls) != 1 {
		t.Fatalf("Sessions.Spawn calls = %d, want 1", len(sessions.calls))
	}
	got := sessions.calls[0]
	if got.ProjectID != "proj-1" || got.Harness != "hermes" || got.Prompt != "do the work" || got.TargetPath != "/repo/app" {
		t.Fatalf("SpawnConfig = %+v, want project=proj-1 harness=hermes prompt='do the work' targetPath=/repo/app", got)
	}
	if got.Kind != domain.KindWorker {
		t.Fatalf("Kind = %s, want %s", got.Kind, domain.KindWorker)
	}
}

func TestSessionServiceLauncher_RequiresAgent(t *testing.T) {
	t.Parallel()
	launcher := &spawner.SessionServiceLauncher{Sessions: &fakeSessionSpawner{}}
	_, err := launcher.Spawn(context.Background(), spawner.SpawnSpec{CardID: "card-1"})
	if err == nil {
		t.Fatal("Spawn: want error for empty Agent, got nil")
	}
}

func TestSessionServiceLauncher_PropagatesSpawnError(t *testing.T) {
	t.Parallel()
	sessions := &fakeSessionSpawner{err: errors.New("boom")}
	launcher := &spawner.SessionServiceLauncher{Sessions: sessions}
	_, err := launcher.Spawn(context.Background(), spawner.SpawnSpec{
		CardID: "card-1", ProjectID: "proj-1", Phase: spawner.PhaseReview, Agent: "codex",
	})
	if err == nil {
		t.Fatal("Spawn: want propagated error, got nil")
	}
}

func TestSpawn_LancherError(t *testing.T) {
	t.Parallel()
	reg, err := registry.Build()
	require.NoError(t, err)

	fl := &fakeLauncher{
		Handle:   spawner.SessionHandle{ID: "sess-e"},
		SpawnErr: errors.New("binary not found"),
	}
	store := &fakeStore{}
	s := spawner.New(fl, store, reg, func() int64 { return 1234567890 }, func() string { return "id-e" })

	ctx := context.Background()
	spec := spawner.SpawnSpec{
		CardID: "card-e", ProjectID: "proj-1",
		Phase: spawner.PhaseCoding, Agent: "hermes",
		Briefing:   "will fail",
		ParentCard: makeCard("card-e", "proj-1", "", "", ""),
	}

	_, err = s.Spawn(ctx, spec)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "binary not found")
	// Store was NOT called because launcher failed before InsertActiveSession.
	assert.Empty(t, store.Inserts)
}
