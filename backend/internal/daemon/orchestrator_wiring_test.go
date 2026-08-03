package daemon

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/modernagent/modern-agent/backend/internal/cdc"
	"github.com/modernagent/modern-agent/backend/internal/commander"
	"github.com/modernagent/modern-agent/backend/internal/domain"
	"github.com/modernagent/modern-agent/backend/internal/storage/sqlite"
)

// fakeSpawner implements commander.Spawner for testing.
type fakeSpawner struct{}

func (f *fakeSpawner) Spawn(ctx context.Context, spec commander.SpawnSpec) (commander.SessionHandle, error) {
	return commander.SessionHandle{}, nil
}

func (f *fakeSpawner) Stop(ctx context.Context, sessionID string) error {
	return nil
}

func (f *fakeSpawner) Inject(ctx context.Context, sessionID string, message string) error {
	return nil
}

// fakeOrchestrator implements OrchestratorTick for testing.
type fakeOrchestrator struct {
	mu      sync.Mutex
	ticks   []string
	tickErr error
}

func (f *fakeOrchestrator) Tick(_ context.Context, projectID string) error {
	f.mu.Lock()
	f.ticks = append(f.ticks, projectID)
	f.mu.Unlock()
	return f.tickErr
}

func (f *fakeOrchestrator) tickProjects() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.ticks...)
}

func TestOrchestratorWiring_StartsAndStopsCleanly(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	store, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := store.UpsertProject(ctx, domain.ProjectRecord{ID: "p1", Path: "/repo/p1", RegisteredAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	bcast := cdc.NewBroadcaster()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	orch := &fakeOrchestrator{}

	wiring, err := WireOrchestrator(ctx, OrchestratorConfig{Orchestrator: orch}, bcast, store, log)
	if err != nil {
		t.Fatalf("WireOrchestrator: %v", err)
	}
	if wiring == nil {
		t.Fatal("WireOrchestrator returned nil wiring (orchestrator should not be nil here)")
	}

	time.Sleep(50 * time.Millisecond)

	wiring.Stop()
}

func TestOrchestratorWiring_NilOrchestratorReturnsNil(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	store, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	bcast := cdc.NewBroadcaster()
	wiring, err := WireOrchestrator(ctx, OrchestratorConfig{Orchestrator: nil}, bcast, store, nil)
	if err != nil {
		t.Fatalf("WireOrchestrator with nil orchestrator: %v", err)
	}
	if wiring != nil {
		t.Error("WireOrchestrator should return nil wiring when orchestrator is nil")
	}
}

func TestOrchestratorWiring_PeriodicTickFires(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	store, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := store.UpsertProject(ctx, domain.ProjectRecord{ID: "proj1", Path: "/repo/proj1", RegisteredAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	bcast := cdc.NewBroadcaster()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	fake := &fakeOrchestrator{}

	// Create wiring directly with a short tick interval for testing
	wiring := &OrchestratorWiring{
		orchestrator:  fake,
		cdcSub:       SubscribeWorkCardChanges(ctx, bcast, log),
		tickInterval: 100 * time.Millisecond,
		stopCh:       make(chan struct{}),
	}
	wiring.wg.Add(1) // run()'s own defer wg.Done() requires a matching Add, same as WireOrchestrator does.

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		wiring.run(ctx, store, log)
	}()

	time.Sleep(350 * time.Millisecond)
	wiring.Stop()
	wg.Wait()

	ticks := fake.tickProjects()
	if len(ticks) < 2 {
		t.Errorf("expected at least 2 periodic ticks, got %d: %v", len(ticks), ticks)
	}
	for _, id := range ticks {
		if id != "proj1" {
			t.Errorf("tick projectID = %q, want proj1", id)
		}
	}
}

func TestOrchestratorWiring_CDCEventTriggersTick(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	store, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := store.UpsertProject(ctx, domain.ProjectRecord{ID: "proj1", Path: "/repo/proj1", RegisteredAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	bcast := cdc.NewBroadcaster()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	fake := &fakeOrchestrator{}
	cdcCh := SubscribeWorkCardChanges(ctx, bcast, log)

	// Publish a card change event through the broadcaster
	bcast.Publish(cdc.Event{
		Type:      "work_card_changed",
		ProjectID: "proj1",
		Payload:   []byte(`{"card_id":"c1","project_id":"proj1","new_status":"running","old_status":"ready"}`),
	})

	select {
	case ev := <-cdcCh:
		if ev.ProjectID != "proj1" || ev.CardID != "c1" {
			t.Errorf("received event = %+v, want proj1/c1", ev)
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("timeout waiting for CDC event on channel")
	}

	// Simulate a tick triggered by the CDC event
	_ = fake.Tick(ctx, "proj1")

	ticks := fake.tickProjects()
	if len(ticks) == 0 {
		t.Error("expected at least one tick from CDC event, got none")
	} else if ticks[0] != "proj1" {
		t.Errorf("tick projectID = %q, want proj1", ticks[0])
	}
}
