package workboard

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/modernagent/modern-agent/backend/internal/domain"
	"github.com/modernagent/modern-agent/backend/internal/ports"
)

func TestDetectRateLimit(t *testing.T) {
	tests := []struct {
		name    string
		harness domain.AgentHarness
		text    string
		want    bool
	}{
		{name: "codex hit", harness: domain.HarnessCodex, text: "You've hit your usage limits. Try again later.", want: true},
		{name: "claude hit", harness: domain.HarnessClaudeCode, text: "Rate limit reached. Please wait before retrying.", want: true},
		{name: "quota exhausted", harness: domain.HarnessCodex, text: "API Error: quota exceeded for requests", want: true},
		{name: "unrelated", harness: domain.HarnessCodex, text: "Running tests… all green", want: false},
		{name: "empty", harness: domain.HarnessClaudeCode, text: "", want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := DetectRateLimit(tc.harness, tc.text); got != tc.want {
				t.Fatalf("DetectRateLimit(%q) = %v, want %v", tc.text, got, tc.want)
			}
		})
	}
}

func TestPickFallbackAgent(t *testing.T) {
	now := time.Date(2026, time.July, 17, 12, 0, 0, 0, time.UTC)
	cooling := map[string]time.Time{
		"codex": now.Add(30 * time.Minute),
	}
	next, ok := PickFallbackAgent("codex", []string{"codex", "claude-code", "kilo"}, cooling, now)
	if !ok || next != "claude-code" {
		t.Fatalf("got %q ok=%v, want claude-code", next, ok)
	}
	if _, ok := PickFallbackAgent("kilo", []string{"codex"}, cooling, now); ok {
		t.Fatal("expected no fallback when only cooling agents remain")
	}
}

func TestAgentSwitcherReconcileProject_SwitchesOnDetectedLimit(t *testing.T) {
	now := time.Date(2026, time.July, 17, 12, 0, 0, 0, time.UTC)
	card := domain.WorkCard{
		ID:         "card-1",
		ProjectID:  "p1",
		BoardID:    defaultBoardID,
		Title:      "Fix login",
		Notes:      "Preserve errors",
		Status:     domain.CardStatusRunning,
		Agent:      "codex",
		SessionID:  "sess-old",
		TargetPath: "/repo/app",
		UpdatedAt:  now.Add(-time.Hour),
	}
	store := &switchStoreFake{
		project: domain.ProjectRecord{
			ID: "p1",
			Config: domain.ProjectConfig{
				Workboard: domain.WorkboardConfig{
					FallbackAgents:       []string{"codex", "claude-code", "kilo"},
					LimitCooldownMinutes: 60,
				},
			},
		},
		cards: []domain.WorkCard{card},
		sessions: []domain.SessionRecord{{
			ID:           "sess-old",
			ProjectID:    "p1",
			Kind:         domain.KindWorker,
			Harness:      domain.HarnessCodex,
			IsTerminated: false,
			Metadata: domain.SessionMetadata{
				RuntimeHandleID: "rt-old",
				Branch:          "session/sess-old",
				WorkspacePath:   "/wt/sess-old",
			},
		}},
	}
	capture := &switchCaptureFake{out: map[string]string{"rt-old": "Error: rate limit reached for model"}}
	spawner := &switchSpawnerFake{nextID: "sess-new"}
	killer := &switchKillerFake{}
	switcher := NewAgentSwitcher(SwitchDeps{
		Store:   store,
		Spawner: spawner,
		Killer:  killer,
		Capture: capture,
		Clock:   func() time.Time { return now },
		NewID:   func() string { return "evt-1" },
	})

	switched, err := switcher.ReconcileProject(context.Background(), "p1")
	if err != nil {
		t.Fatalf("ReconcileProject: %v", err)
	}
	if len(switched) != 1 || switched[0] != "card-1" {
		t.Fatalf("switched = %v", switched)
	}
	if killer.killed != "sess-old" {
		t.Fatalf("killed = %q", killer.killed)
	}
	if spawner.last.Harness != domain.HarnessClaudeCode {
		t.Fatalf("spawn harness = %q", spawner.last.Harness)
	}
	if spawner.last.TargetPath != "/repo/app" {
		t.Fatalf("spawn target = %q", spawner.last.TargetPath)
	}
	if !strings.Contains(spawner.last.Prompt, "Fix login") {
		t.Fatalf("prompt missing handoff: %q", spawner.last.Prompt)
	}
	got := store.cards[0]
	if got.Agent != "claude-code" || got.SessionID != "sess-new" {
		t.Fatalf("card after switch: agent=%q session=%q", got.Agent, got.SessionID)
	}
	if len(store.events) != 1 || store.events[0].Kind != workCardEventAgentSwitched {
		t.Fatalf("events = %+v", store.events)
	}
}

func TestAgentSwitcherReconcileProject_DoesNotKillHermesCommander(t *testing.T) {
	now := time.Date(2026, time.July, 17, 12, 0, 0, 0, time.UTC)
	card := domain.WorkCard{ID: "card-1", ProjectID: "p1", BoardID: defaultBoardID, Status: domain.CardStatusRunning, Agent: "claude-code", SessionID: "hermes-1"}
	store := &switchStoreFake{project: domain.ProjectRecord{ID: "p1", Config: domain.ProjectConfig{Workboard: domain.WorkboardConfig{FallbackAgents: []string{"codex"}}}}, cards: []domain.WorkCard{card}, sessions: []domain.SessionRecord{{ID: "hermes-1", ProjectID: "p1", Kind: domain.KindOrchestrator, Harness: domain.HarnessHermes, Metadata: domain.SessionMetadata{RuntimeHandleID: "rt-hermes"}}}}
	killer := &switchKillerFake{}
	switcher := NewAgentSwitcher(SwitchDeps{Store: store, Spawner: &switchSpawnerFake{nextID: "worker-1"}, Killer: killer, Capture: &switchCaptureFake{out: map[string]string{"rt-hermes": "rate limit"}}, Clock: func() time.Time { return now }})

	switched, err := switcher.ReconcileProject(context.Background(), "p1")
	if err != nil {
		t.Fatalf("ReconcileProject: %v", err)
	}
	if len(switched) != 0 || killer.killed != "" {
		t.Fatalf("switched=%v killed=%q", switched, killer.killed)
	}
}

type switchStoreFake struct {
	project  domain.ProjectRecord
	cards    []domain.WorkCard
	sessions []domain.SessionRecord
	events   []domain.WorkCardEvent
}

func (s *switchStoreFake) GetProject(context.Context, string) (domain.ProjectRecord, bool, error) {
	return s.project, true, nil
}
func (s *switchStoreFake) ListWorkCards(context.Context, string, string) ([]domain.WorkCard, error) {
	out := append([]domain.WorkCard(nil), s.cards...)
	return out, nil
}
func (s *switchStoreFake) UpdateWorkCard(_ context.Context, card domain.WorkCard) error {
	for i := range s.cards {
		if s.cards[i].ID == card.ID {
			s.cards[i] = card
			return nil
		}
	}
	s.cards = append(s.cards, card)
	return nil
}
func (s *switchStoreFake) ListSessions(context.Context, domain.ProjectID) ([]domain.SessionRecord, error) {
	return append([]domain.SessionRecord(nil), s.sessions...), nil
}
func (s *switchStoreFake) AppendWorkCardEvent(_ context.Context, event domain.WorkCardEvent) error {
	s.events = append(s.events, event)
	return nil
}

type switchCaptureFake struct{ out map[string]string }

func (c *switchCaptureFake) GetOutput(_ context.Context, handle ports.RuntimeHandle, _ int) (string, error) {
	return c.out[handle.ID], nil
}

type switchSpawnerFake struct {
	nextID string
	last   ports.SpawnConfig
}

func (s *switchSpawnerFake) Spawn(_ context.Context, cfg ports.SpawnConfig) (domain.Session, error) {
	s.last = cfg
	return domain.Session{SessionRecord: domain.SessionRecord{
		ID: domain.SessionID(s.nextID), ProjectID: cfg.ProjectID, Kind: cfg.Kind, Harness: cfg.Harness,
	}}, nil
}

type switchKillerFake struct{ killed string }

func (k *switchKillerFake) Kill(_ context.Context, id domain.SessionID) (bool, error) {
	k.killed = string(id)
	return true, nil
}
