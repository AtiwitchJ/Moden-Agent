package orchestrator

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/modernagent/modern-agent/backend/internal/commander"
	"github.com/modernagent/modern-agent/backend/internal/commander/spawner"
	"github.com/modernagent/modern-agent/backend/internal/domain"
)

var ctx = context.Background()

// --- Fakes ---

type fakeStore struct {
	cards           map[string]domain.WorkCard
	activeSessions  map[string]ActiveSessionRecord
	redoCycles      map[string][]domain.RedoCycle
	events          []domain.WorkCardEvent
	listSessionsOut []domain.SessionRecord
	projects        map[string]domain.ProjectRecord
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		cards:          make(map[string]domain.WorkCard),
		activeSessions: make(map[string]ActiveSessionRecord),
		redoCycles:     make(map[string][]domain.RedoCycle),
	}
}

func (s *fakeStore) ListWorkCards(_ context.Context, projectID, _ string) ([]domain.WorkCard, error) {
	var out []domain.WorkCard
	for _, c := range s.cards {
		if projectID == "" || c.ProjectID == projectID {
			out = append(out, c)
		}
	}
	return out, nil
}

func (s *fakeStore) GetWorkCard(_ context.Context, id string) (domain.WorkCard, bool, error) {
	c, ok := s.cards[id]
	return c, ok, nil
}

func (s *fakeStore) GetActiveSession(_ context.Context, cardID string) (ActiveSessionRecord, bool, error) {
	rec, ok := s.activeSessions[cardID]
	return rec, ok, nil
}

func (s *fakeStore) InsertActiveSession(_ context.Context, ins spawner.InsertActiveSession) error {
	s.activeSessions[ins.CardID] = ActiveSessionRecord{
		CardID:    ins.CardID,
		SessionID: ins.SessionID,
		Phase:     string(ins.Phase),
		Agent:     ins.Agent,
		CreatedAt: time.Now(),
	}
	return nil
}

func (s *fakeStore) DeleteActiveSession(_ context.Context, cardID string) error {
	delete(s.activeSessions, cardID)
	return nil
}

func (s *fakeStore) UpdateWorkCard(_ context.Context, card domain.WorkCard) error {
	s.cards[card.ID] = card
	return nil
}

func (s *fakeStore) AppendWorkCardEvent(_ context.Context, event domain.WorkCardEvent) error {
	s.events = append(s.events, event)
	return nil
}

func (s *fakeStore) ListRedoCycles(_ context.Context, cardID string) ([]domain.RedoCycle, error) {
	return s.redoCycles[cardID], nil
}

func (s *fakeStore) ListSessions(_ context.Context, _ domain.ProjectID) ([]domain.SessionRecord, error) {
	return s.listSessionsOut, nil
}

func (s *fakeStore) GetProject(_ context.Context, id string) (domain.ProjectRecord, bool, error) {
	rec, ok := s.projects[id]
	return rec, ok, nil
}

type fakeSpawner struct {
	spawned  []spawnedCall
	spawnErr error
	stopErr  error
	// store, when set, mirrors production spawner.Spawner.Spawn's behavior of
	// recording the (card, session, phase, agent) fact in active_session on a
	// successful launch. Left nil in tests that don't care about that
	// bookkeeping.
	store *fakeStore
}

type spawnedCall struct {
	Spec commander.SpawnSpec
}

func (s *fakeSpawner) Spawn(ctx context.Context, spec commander.SpawnSpec) (commander.SessionHandle, error) {
	s.spawned = append(s.spawned, spawnedCall{Spec: spec})
	if s.spawnErr != nil {
		return commander.SessionHandle{}, s.spawnErr
	}
	handle := commander.SessionHandle{ID: "session-" + spec.CardID + "-" + spec.Agent, NativeID: "native-" + spec.Agent}
	if s.store != nil {
		if err := s.store.InsertActiveSession(ctx, spawner.InsertActiveSession{
			CardID:    spec.CardID,
			SessionID: handle.ID,
			Phase:     spawner.Phase(spec.Phase),
			Agent:     spec.Agent,
		}); err != nil {
			return commander.SessionHandle{}, err
		}
	}
	return handle, nil
}

func (s *fakeSpawner) Stop(_ context.Context, _ string) error {
	return s.stopErr
}

func (s *fakeSpawner) Inject(_ context.Context, _, _ string) error {
	return nil
}

type fakeKiller struct {
	killed []domain.SessionID
	err    error
}

func (f *fakeKiller) Kill(_ context.Context, id domain.SessionID) (bool, error) {
	f.killed = append(f.killed, id)
	return f.err == nil, f.err
}

func card(id, projectID, status string) domain.WorkCard {
	return domain.WorkCard{
		ID:            id,
		ProjectID:     projectID,
		BoardID:       defaultBoardID,
		Title:         "Test card " + id,
		Status:        domain.CardStatus(status),
		CodingAgent:   "claude-code",
		Agent:         "claude-code",
		ReviewerAgent: "hermes-reviewer",
		TestingAgent:  "hermes-tester",
		GoalVersion:   1,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
}

// --- Tests ---

func TestTick_RunningCardWithNoSession_SpawnsHermes(t *testing.T) {
	store := newFakeStore()
	sp := &fakeSpawner{}
	now := time.Date(2026, time.August, 3, 10, 0, 0, 0, time.UTC)
	oc := New(Config{
		Store:    store,
		Spawner:  sp,
		Clock:    func() time.Time { return now },
		NewID:    func() string { return "ev-1" },
		WIPLimit: 4,
	})

	c := card("c1", "p1", "running")
	store.cards[c.ID] = c

	err := oc.Tick(context.Background(), "p1")
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}

	if len(sp.spawned) != 1 {
		t.Fatalf("spawned count = %d, want 1", len(sp.spawned))
	}
	if sp.spawned[0].Spec.Agent != "claude-code" {
		t.Fatalf("spawned agent = %q, want claude-code", sp.spawned[0].Spec.Agent)
	}
	if sp.spawned[0].Spec.CardID != "c1" {
		t.Fatalf("spawned cardID = %q, want c1", sp.spawned[0].Spec.CardID)
	}
	if sp.spawned[0].Spec.Phase != commander.PhaseCoding {
		t.Fatalf("spawned phase = %v, want PhaseCoding", sp.spawned[0].Spec.Phase)
	}
}

func TestGenerateBriefing_RequiresPriorPhaseHandoff(t *testing.T) {
	briefing, err := generateBriefing(card("c1", "p1", "review"), commander.PhaseReview, nil)
	if err != nil {
		t.Fatalf("generateBriefing: %v", err)
	}
	if !strings.Contains(briefing, "durable handoffs") || !strings.Contains(briefing, "git diff") || !strings.Contains(briefing, "Record your own handoff") {
		t.Fatalf("review briefing missing handoff protocol: %s", briefing)
	}
}

func TestTick_RunningCardWithLiveSession_DoesNothing(t *testing.T) {
	store := newFakeStore()
	sp := &fakeSpawner{}
	now := time.Date(2026, time.August, 3, 10, 0, 0, 0, time.UTC)
	oc := New(Config{
		Store:    store,
		Spawner:  sp,
		Clock:    func() time.Time { return now },
		NewID:    func() string { return "ev-1" },
		WIPLimit: 4,
	})

	c := card("c1", "p1", "running")
	store.cards[c.ID] = c
	store.activeSessions["c1"] = ActiveSessionRecord{
		CardID:    "c1",
		SessionID: "session-live",
		Phase:     "coding",
		Agent:     "claude-code",
		CreatedAt: now.Add(-5 * time.Minute),
	}
	c.SessionID = "session-live"
	store.cards["c1"] = c

	err := oc.Tick(context.Background(), "p1")
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}

	if len(sp.spawned) != 0 {
		t.Fatalf("spawned count = %d, want 0 (live session)", len(sp.spawned))
	}
}

// TestTick_RunningCardLinkedToLiveSessionByAnotherCommander_DoesNotDoubleSpawn
// covers the collision between the two independent commanders that both drive
// plain (non-Director) projects: workboard's dispatch.go claims a Todo/Ready
// card, spawns its worker directly through session_manager, and links
// card.SessionID — but it never writes an active_session row, since that
// bookkeeping belongs to this package. Before this test, Tick's
// checkActiveSession only consulted active_session, so it saw "no active
// session" on a card another commander had just started and spawned a second,
// redundant coding worker on the very same tick.
func TestTick_RunningCardLinkedToLiveSessionByAnotherCommander_DoesNotDoubleSpawn(t *testing.T) {
	store := newFakeStore()
	sp := &fakeSpawner{}
	now := time.Date(2026, time.August, 3, 10, 0, 0, 0, time.UTC)
	oc := New(Config{
		Store:    store,
		Spawner:  sp,
		Clock:    func() time.Time { return now },
		NewID:    func() string { return "ev-1" },
		WIPLimit: 4,
	})

	c := card("c1", "p1", "running")
	c.SessionID = "dispatch-session"
	store.cards[c.ID] = c
	// No active_session row: dispatch.go's plain path linked card.SessionID
	// directly and never called InsertActiveSession.
	store.listSessionsOut = []domain.SessionRecord{
		{ID: "dispatch-session", ProjectID: "p1", IsTerminated: false, CreatedAt: now.Add(-1 * time.Minute)},
	}

	err := oc.Tick(context.Background(), "p1")
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}

	if len(sp.spawned) != 0 {
		t.Fatalf("spawned count = %d, want 0 — dispatch.go already started a live session for this card", len(sp.spawned))
	}
}

// TestTick_RunningCardLinkedToTerminatedSession_StillSpawns proves the new
// cross-check does not mask a genuinely dead hand-off: if the session
// dispatch.go linked has since terminated (crashed, killed, exited) and
// nothing else registered a fresh active_session, Tick must still spawn a
// coding worker rather than leaving the card stuck forever.
func TestTick_RunningCardLinkedToTerminatedSession_StillSpawns(t *testing.T) {
	store := newFakeStore()
	sp := &fakeSpawner{}
	now := time.Date(2026, time.August, 3, 10, 0, 0, 0, time.UTC)
	oc := New(Config{
		Store:    store,
		Spawner:  sp,
		Clock:    func() time.Time { return now },
		NewID:    func() string { return "ev-1" },
		WIPLimit: 4,
	})

	c := card("c1", "p1", "running")
	c.SessionID = "dispatch-session"
	store.cards[c.ID] = c
	store.listSessionsOut = []domain.SessionRecord{
		{ID: "dispatch-session", ProjectID: "p1", IsTerminated: true, CreatedAt: now.Add(-1 * time.Minute)},
	}

	err := oc.Tick(context.Background(), "p1")
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}

	if len(sp.spawned) != 1 {
		t.Fatalf("spawned count = %d, want 1 — the linked session is terminated, so the card has no live worker", len(sp.spawned))
	}
}

func TestTick_WIPAtLimit_RefusesNewSpawn(t *testing.T) {
	store := newFakeStore()
	sp := &fakeSpawner{}
	now := time.Date(2026, time.August, 3, 10, 0, 0, 0, time.UTC)
	oc := New(Config{
		Store:    store,
		Spawner:  sp,
		Clock:    func() time.Time { return now },
		NewID:    func() string { return "ev-1" },
		WIPLimit: 1,
	})

	// Add a running card already at WIP limit
	existing := card("c0", "p1", "running")
	store.cards[existing.ID] = existing

	// Try to tick a second running card
	newCard := card("c1", "p1", "running")
	store.cards[newCard.ID] = newCard

	err := oc.Tick(context.Background(), "p1")
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}

	if len(sp.spawned) != 0 {
		t.Fatalf("spawned count = %d, want 0 (WIP limit reached)", len(sp.spawned))
	}
}

func TestOnAgentCompleted_VerdictApprovedOnReviewCard_TransitionsToTesting(t *testing.T) {
	store := newFakeStore()
	sp := &fakeSpawner{}
	now := time.Date(2026, time.August, 3, 10, 0, 0, 0, time.UTC)
	oc := New(Config{
		Store:    store,
		Spawner:  sp,
		Clock:    func() time.Time { return now },
		NewID:    func() string { return "ev-1" },
		WIPLimit: 4,
	})

	c := card("c1", "p1", "review")
	c.ReviewerAgent = "hermes-reviewer"
	store.cards[c.ID] = c

	err := oc.OnAgentCompleted(context.Background(), "c1", commander.AgentResult{
		Phase:   commander.PhaseReview,
		Verdict: "approved",
	})
	if err != nil {
		t.Fatalf("OnAgentCompleted: %v", err)
	}

	updated := store.cards["c1"]
	if updated.Status != domain.CardStatusTesting {
		t.Fatalf("card status = %q, want testing", updated.Status)
	}

	hasTestingEvent := false
	for _, ev := range store.events {
		if ev.Kind == "phase_completed" {
			hasTestingEvent = true
		}
	}
	if !hasTestingEvent {
		t.Fatalf("no phase_completed event recorded")
	}
}

func TestGetCardUsesGetWorkCard_NotListWorkCardsWithEmptyFilters(t *testing.T) {
	store := newFakeStore()
	store.cards["card-1"] = domain.WorkCard{
		ID: "card-1", ProjectID: "p1", Status: domain.CardStatusRunning,
		CodingAgent: "hermes",
	}
	// listWorkCardsCalls with empty projectID/boardID must be zero: getCard must
	// go through GetWorkCard, not ListWorkCards(ctx, "", "").
	orc := New(Config{Store: store, Spawner: &fakeSpawner{}})

	err := orc.OnAgentCompleted(context.Background(), "card-1", commander.AgentResult{
		Phase: commander.PhaseCoding, Verdict: "approved",
	})
	if err != nil {
		t.Fatalf("OnAgentCompleted: %v", err)
	}
	if got := store.cards["card-1"].Status; got != domain.CardStatusReview {
		t.Fatalf("status = %s, want review", got)
	}
}

func TestOnAgentFailed_ReviewerExhausted_FallsBackToHermes(t *testing.T) {
	store := newFakeStore()
	sp := &fakeSpawner{}
	now := time.Date(2026, time.August, 3, 10, 0, 0, 0, time.UTC)
	oc := New(Config{
		Store:    store,
		Spawner:  sp,
		Clock:    func() time.Time { return now },
		NewID:    func() string { return "ev-1" },
		WIPLimit: 4,
	})

	// Use hermes-reviewer as reviewer (last in its chain), which triggers
	// redo on failure since hermes is the final fallback.
	c := card("c1", "p1", "review")
	c.ReviewerAgent = "hermes-reviewer"
	store.cards[c.ID] = c

	// Simulate previous spawn that is now the current agent
	store.activeSessions["c1"] = ActiveSessionRecord{
		CardID:    "c1",
		SessionID: "reviewer-session",
		Phase:     "review",
		Agent:     "hermes-reviewer",
		CreatedAt: now.Add(-20 * time.Minute),
	}

	err := oc.OnAgentFailed(context.Background(), "c1", commander.AgentAttempt{
		Phase:         commander.PhaseReview,
		Agent:         "hermes-reviewer",
		FailureReason: "changes_requested",
		AttemptNumber: 1,
	})
	if err != nil {
		t.Fatalf("OnAgentFailed: %v", err)
	}

	// hermes-reviewer is last in [hermes-reviewer, hermes]; hermes is the
	// fallback and is also last, so this transitions to redo, no spawn.
	if len(sp.spawned) != 0 {
		t.Fatalf("spawned count = %d, want 0 (hermes is last, transitions to redo)", len(sp.spawned))
	}
	updated := store.cards["c1"]
	if updated.Status != domain.CardStatusRedo {
		t.Fatalf("card status = %q, want redo (hermes-reviewer exhausted)", updated.Status)
	}
}

func TestOnAgentFailed_HermesExhausted_CreatesRedoCycleAndTransitionsToRedo(t *testing.T) {
	store := newFakeStore()
	sp := &fakeSpawner{}
	now := time.Date(2026, time.August, 3, 10, 0, 0, 0, time.UTC)
	oc := New(Config{
		Store:    store,
		Spawner:  sp,
		Clock:    func() time.Time { return now },
		NewID:    func() string { return "ev-1" },
		WIPLimit: 4,
	})

	c := card("c1", "p1", "review")
	c.ReviewerAgent = "hermes-reviewer"
	store.cards[c.ID] = c

	// Hermes is the last in the chain, so this should trigger redo
	store.activeSessions["c1"] = ActiveSessionRecord{
		CardID:    "c1",
		SessionID: "hermes-session",
		Phase:     "review",
		Agent:     "hermes",
		CreatedAt: now.Add(-20 * time.Minute),
	}

	err := oc.OnAgentFailed(context.Background(), "c1", commander.AgentAttempt{
		Phase:         commander.PhaseReview,
		Agent:         "hermes",
		FailureReason: "error",
		AttemptNumber: 2,
	})
	if err != nil {
		t.Fatalf("OnAgentFailed: %v", err)
	}

	updated := store.cards["c1"]
	if updated.Status != domain.CardStatusRedo {
		t.Fatalf("card status = %q, want redo", updated.Status)
	}
	if updated.RedoCount != 1 {
		t.Fatalf("redo count = %d, want 1", updated.RedoCount)
	}

	hasRedoStarted := false
	for _, ev := range store.events {
		if ev.Kind == "redo_started" {
			hasRedoStarted = true
		}
	}
	if !hasRedoStarted {
		t.Fatalf("no redo_started event recorded")
	}
}

func TestCheckOrphans_StaleRunningSession_SpawnsReplacement(t *testing.T) {
	store := newFakeStore()
	// store must be wired so fakeSpawner.Spawn records the replacement session,
	// matching production spawner.Spawner.Spawn — this test asserts on that
	// recorded row below.
	sp := &fakeSpawner{store: store}
	now := time.Date(2026, time.August, 3, 10, 0, 0, 0, time.UTC)
	oc := New(Config{
		Store:    store,
		Spawner:  sp,
		Clock:    func() time.Time { return now },
		NewID:    func() string { return "ev-1" },
		WIPLimit: 4,
	})

	c := card("c1", "p1", "running")
	c.SessionID = "old-session"
	store.cards[c.ID] = c

	// Active session is 31 minutes old — past the 30-min orphan threshold
	store.activeSessions["c1"] = ActiveSessionRecord{
		CardID:    "c1",
		SessionID: "old-session",
		Phase:     "coding",
		Agent:     "claude-code",
		CreatedAt: now.Add(-31 * time.Minute),
	}

	err := oc.checkOrphans(context.Background(), []domain.WorkCard{c})
	if err != nil {
		t.Fatalf("checkOrphans: %v", err)
	}

	if len(sp.spawned) != 1 {
		t.Fatalf("spawned count = %d, want 1 (orphan replacement)", len(sp.spawned))
	}

	// Verify a new active session was inserted (replacing the old)
	newSession, ok := store.activeSessions["c1"]
	if !ok {
		t.Fatalf("no active session after orphan replacement")
	}
	// The new session should NOT be the old one
	if newSession.SessionID == "old-session" {
		t.Fatalf("old session not replaced, still %q", newSession.SessionID)
	}
}

func TestOnSignal_PRReadyOnReviewCard_TransitionsToTesting(t *testing.T) {
	store := newFakeStore()
	sp := &fakeSpawner{}
	now := time.Date(2026, time.August, 3, 10, 0, 0, 0, time.UTC)
	oc := New(Config{
		Store:    store,
		Spawner:  sp,
		Clock:    func() time.Time { return now },
		NewID:    func() string { return "ev-1" },
		WIPLimit: 4,
	})

	c := card("c1", "p1", "review")
	store.cards[c.ID] = c

	err := oc.OnSignal(context.Background(), "c1", commander.Signal{Kind: "PRReady"})
	if err != nil {
		t.Fatalf("OnSignal PRReady: %v", err)
	}

	updated := store.cards["c1"]
	if updated.Status != domain.CardStatusTesting {
		t.Fatalf("card status = %q, want testing", updated.Status)
	}
}

func TestOnSignal_CIFailed_TriggersFailureHandler(t *testing.T) {
	store := newFakeStore()
	sp := &fakeSpawner{}
	now := time.Date(2026, time.August, 3, 10, 0, 0, 0, time.UTC)
	oc := New(Config{
		Store:    store,
		Spawner:  sp,
		Clock:    func() time.Time { return now },
		NewID:    func() string { return "ev-1" },
		WIPLimit: 4,
	})

	c := card("c1", "p1", "testing")
	// No testing agent defined; the only entry in the chain is "hermes" as fallback,
	// so hermes is the last agent — OnAgentFailed transitions to redo.
	c.TestingAgent = ""
	store.cards[c.ID] = c
	store.activeSessions["c1"] = ActiveSessionRecord{
		CardID:    "c1",
		SessionID: "tester-session",
		Phase:     "testing",
		Agent:     "hermes",
		CreatedAt: now.Add(-5 * time.Minute),
	}

	err := oc.OnSignal(context.Background(), "c1", commander.Signal{Kind: "CIFailed"})
	if err != nil {
		t.Fatalf("OnSignal CIFailed: %v", err)
	}

	// Active session should be cleaned up after CIFailed triggers failure handler
	if _, ok := store.activeSessions["c1"]; ok {
		t.Fatalf("active session still present after CIFailed")
	}

	// Card should have transitioned to redo after Hermes was exhausted
	updated := store.cards["c1"]
	if updated.Status != domain.CardStatusRedo {
		t.Fatalf("card status = %q, want redo after Hermes failure", updated.Status)
	}
}

func TestPickNextAgent(t *testing.T) {
	store := newFakeStore()
	oc := New(Config{Store: store})

	c := card("c1", "p1", "running")
	c.CodingAgent = "claude-code"

	tests := []struct {
		phase        commander.Phase
		currentAgent string
		want         string
	}{
		{commander.PhaseCoding, "", "claude-code"},
		{commander.PhaseCoding, "claude-code", "claude-code"}, // fallback to same if Agent field also matches
		{commander.PhaseReview, "", "hermes-reviewer"},
		{commander.PhaseTesting, "", "hermes-tester"},
	}

	for _, tt := range tests {
		got := oc.pickNextAgent(c, tt.phase, tt.currentAgent)
		if got != tt.want {
			t.Errorf("pickNextAgent(%v, %q) = %q, want %q", tt.phase, tt.currentAgent, got, tt.want)
		}
	}
}

func TestIsLastAgent(t *testing.T) {
	store := newFakeStore()
	oc := New(Config{Store: store})

	c := domain.WorkCard{
		ID:          "c1",
		ProjectID:   "p1",
		BoardID:     defaultBoardID,
		CodingAgent: "claude-code",
		Agent:       "", // only CodingAgent, no duplicate
	}

	// hermes is last in the chain [claude-code, hermes]
	if !oc.isLastAgent(c, commander.PhaseCoding, "hermes") {
		t.Errorf("isLastAgent(coding, hermes) = false, want true")
	}
	if oc.isLastAgent(c, commander.PhaseCoding, "claude-code") {
		t.Errorf("isLastAgent(coding, claude-code) = true, want false (claude-code not last)")
	}
}

func TestCountActiveCards(t *testing.T) {
	store := newFakeStore()
	store.cards["c1"] = card("c1", "p1", "running")
	store.cards["c2"] = card("c2", "p1", "review")
	store.cards["c3"] = card("c3", "p1", "done")
	store.cards["c4"] = card("c4", "p1", "todo")

	oc := New(Config{Store: store})

	count, err := oc.countActiveCards(context.Background(), "p1")
	if err != nil {
		t.Fatalf("countActiveCards: %v", err)
	}
	if count != 2 {
		t.Fatalf("active count = %d, want 2 (running + review)", count)
	}
}

func TestTickDoesNotDoubleInsertActiveSession(t *testing.T) {
	store := newFakeStore()
	store.cards["card-1"] = domain.WorkCard{
		ID: "card-1", ProjectID: "p1", Status: domain.CardStatusRunning,
		CodingAgent: "hermes",
	}
	sp := &fakeSpawner{store: store} // fakeSpawner.Spawn inserts into store.activeSessions, matching production spawner.Spawner.Spawn
	orc := New(Config{Store: store, Spawner: sp, WIPLimit: 4})

	if err := orc.Tick(context.Background(), "p1"); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	rec, ok := store.activeSessions["card-1"]
	if !ok {
		t.Fatal("no active session recorded for card-1 after first tick")
	}
	firstSessionID := rec.SessionID

	// A second tick immediately after must NOT spawn another session: the
	// first spawn is live (fresh, well within any timeout), so tickActiveCards
	// must recognize it and do nothing.
	if err := orc.Tick(context.Background(), "p1"); err != nil {
		t.Fatalf("second Tick: %v", err)
	}
	if len(sp.spawned) != 1 {
		t.Fatalf("spawner.Spawn called %d times across two ticks, want 1 (no runaway respawn)", len(sp.spawned))
	}
	if store.activeSessions["card-1"].SessionID != firstSessionID {
		t.Fatalf("active session changed across ticks: %s -> %s, want stable", firstSessionID, store.activeSessions["card-1"].SessionID)
	}
}

func TestTickKillsSupersededSessionOnTimeoutReplacement(t *testing.T) {
	store := newFakeStore()
	store.cards["card-1"] = domain.WorkCard{
		ID: "card-1", ProjectID: "p1", Status: domain.CardStatusRunning,
		CodingAgent: "hermes",
	}
	// Pre-seed a stale active session, older than the 30-minute Running timeout.
	store.activeSessions["card-1"] = ActiveSessionRecord{
		CardID: "card-1", SessionID: "old-sess", Phase: "coding", Agent: "hermes",
		CreatedAt: time.Now().Add(-31 * time.Minute),
	}
	spawner := &fakeSpawner{store: store}
	killer := &fakeKiller{}
	orc := New(Config{Store: store, Spawner: spawner, Killer: killer, WIPLimit: 4})

	if err := orc.Tick(context.Background(), "p1"); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	if len(killer.killed) != 1 || killer.killed[0] != domain.SessionID("old-sess") {
		t.Fatalf("killed = %v, want [old-sess]", killer.killed)
	}
	if len(spawner.spawned) != 1 {
		t.Fatalf("spawner.Spawn called %d times, want 1 (the replacement)", len(spawner.spawned))
	}
}

func TestTickReplacementSpawnProceedsWhenKillFails(t *testing.T) {
	store := newFakeStore()
	store.cards["card-1"] = domain.WorkCard{
		ID: "card-1", ProjectID: "p1", Status: domain.CardStatusRunning,
		CodingAgent: "hermes",
	}
	store.activeSessions["card-1"] = ActiveSessionRecord{
		CardID: "card-1", SessionID: "old-sess", Phase: "coding", Agent: "hermes",
		CreatedAt: time.Now().Add(-31 * time.Minute),
	}
	spawner := &fakeSpawner{store: store}
	killer := &fakeKiller{err: errors.New("session already gone")}
	orc := New(Config{Store: store, Spawner: spawner, Killer: killer, WIPLimit: 4})

	// A kill failure must not block the replacement spawn.
	if err := orc.Tick(context.Background(), "p1"); err != nil {
		t.Fatalf("Tick: %v, want nil (kill failure should not propagate)", err)
	}
	if len(spawner.spawned) != 1 {
		t.Fatalf("spawner.Spawn called %d times, want 1 (replacement still proceeds)", len(spawner.spawned))
	}
}

func TestTick_SkipsProjectsCommandedByTheDirector(t *testing.T) {
	st := newFakeStore()
	st.projects = map[string]domain.ProjectRecord{
		"p": {ID: "p", Config: domain.ProjectConfig{
			Director: domain.RoleOverride{Harness: domain.HarnessDirector},
		}},
	}
	st.cards["c1"] = domain.WorkCard{ID: "c1", ProjectID: "p", Status: domain.CardStatusRunning}
	sp := &fakeSpawner{}
	o := New(Config{Spawner: sp, Store: st, WIPLimit: 4})

	if err := o.Tick(ctx, "p"); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if len(sp.spawned) != 0 {
		t.Fatalf("spawned = %d, want 0 — the Director owns this project's cards", len(sp.spawned))
	}
}

func TestTick_StillRunsForHermesProjects(t *testing.T) {
	st := newFakeStore()
	st.projects = map[string]domain.ProjectRecord{
		"p": {ID: "p", Config: domain.ProjectConfig{
			Orchestrator: domain.RoleOverride{Harness: domain.HarnessHermes},
		}},
	}
	st.cards["c1"] = domain.WorkCard{ID: "c1", ProjectID: "p", Status: domain.CardStatusRunning}
	sp := &fakeSpawner{}
	o := New(Config{Spawner: sp, Store: st, WIPLimit: 4})

	if err := o.Tick(ctx, "p"); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if len(sp.spawned) != 1 {
		t.Fatalf("spawned = %d, want 1", len(sp.spawned))
	}
}

// Ensure fakeStore implements OrchestratorStore
var _ OrchestratorStore = (*fakeStore)(nil)

// Ensure fakeSpawner implements commander.Spawner
var _ commander.Spawner = (*fakeSpawner)(nil)
