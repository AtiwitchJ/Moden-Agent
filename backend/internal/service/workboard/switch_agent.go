package workboard

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/modernagent/modern-agent/backend/internal/domain"
	"github.com/modernagent/modern-agent/backend/internal/ports"
)

const (
	workCardEventAgentSwitched = "agent_switched"
	switchCaptureLines         = 80
)

// SwitchStore is the durable surface for rate-limit agent switching.
type SwitchStore interface {
	GetProject(ctx context.Context, id string) (domain.ProjectRecord, bool, error)
	ListWorkCards(ctx context.Context, projectID, boardID string) ([]domain.WorkCard, error)
	UpdateWorkCard(ctx context.Context, card domain.WorkCard) error
	ListSessions(ctx context.Context, projectID domain.ProjectID) ([]domain.SessionRecord, error)
	AppendWorkCardEvent(ctx context.Context, event domain.WorkCardEvent) error
}

// SessionKiller stops a worker so its branch can be reused by a replacement spawn.
type SessionKiller interface {
	Kill(ctx context.Context, id domain.SessionID) (bool, error)
}

// TerminalCapture reads recent TUI output for rate-limit substring detection.
type TerminalCapture interface {
	GetOutput(ctx context.Context, handle ports.RuntimeHandle, lines int) (string, error)
}

// SwitchDeps configures an AgentSwitcher.
type SwitchDeps struct {
	Store   SwitchStore
	Spawner WorkerSpawner
	Killer  SessionKiller
	Capture TerminalCapture
	Clock   func() time.Time
	NewID   func() string
}

// AgentSwitcher detects coding-agent rate limits on running cards and respawns
// with the next project fallback agent. Cooldowns are held in-memory for the
// daemon lifetime (LimitCooldownMinutes); restart clears them.
type AgentSwitcher struct {
	store    SwitchStore
	spawner  WorkerSpawner
	killer   SessionKiller
	capture  TerminalCapture
	clock    func() time.Time
	newID    func() string
	mu       sync.Mutex
	cooldown map[string]time.Time // key: projectID + "\x00" + agent
}

// NewAgentSwitcher constructs the rate-limit failover reconciler.
func NewAgentSwitcher(d SwitchDeps) *AgentSwitcher {
	clock := d.Clock
	if clock == nil {
		clock = time.Now
	}
	newID := d.NewID
	if newID == nil {
		newID = func() string { return "wce_" + uuid.NewString() }
	}
	return &AgentSwitcher{
		store:    d.Store,
		spawner:  d.Spawner,
		killer:   d.Killer,
		capture:  d.Capture,
		clock:    clock,
		newID:    newID,
		cooldown: map[string]time.Time{},
	}
}

var rateLimitNeedles = []string{
	"rate limit",
	"rate-limit",
	"usage limit",
	"usage limits",
	"quota exceeded",
	"quota exhausted",
	"hit your limit",
	"you've hit your usage",
	"too many requests",
	"429",
}

// DetectRateLimit reports whether terminal text indicates a coding-agent quota
// limit for the given harness. Unknown harnesses still use the shared needles.
func DetectRateLimit(_ domain.AgentHarness, terminalText string) bool {
	lower := strings.ToLower(terminalText)
	if lower == "" {
		return false
	}
	for _, needle := range rateLimitNeedles {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

// PickFallbackAgent returns the next agent in fallbacks that is not the current
// agent and not cooling down at now.
func PickFallbackAgent(current string, fallbacks []string, cooling map[string]time.Time, now time.Time) (string, bool) {
	for _, agent := range fallbacks {
		agent = strings.TrimSpace(agent)
		if agent == "" || agent == current {
			continue
		}
		if until, ok := cooling[agent]; ok && until.After(now) {
			continue
		}
		return agent, true
	}
	return "", false
}

// ReconcileProject scans running cards for rate-limit TUI signals and switches
// agents. It returns card IDs successfully switched.
func (s *AgentSwitcher) ReconcileProject(ctx context.Context, projectID string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.store == nil || s.spawner == nil || s.killer == nil || s.capture == nil {
		return nil, nil
	}
	project, ok, err := s.store.GetProject(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("get project %s: %w", projectID, err)
	}
	if !ok {
		return nil, fmt.Errorf("project %s not found", projectID)
	}
	cfg := project.Config.Workboard
	if len(cfg.FallbackAgents) == 0 {
		return nil, nil
	}
	if cfg.LimitCooldownMinutes <= 0 {
		cfg.LimitCooldownMinutes = domain.DefaultWorkboardConfig().LimitCooldownMinutes
	}

	cards, err := s.store.ListWorkCards(ctx, projectID, defaultBoardID)
	if err != nil {
		return nil, fmt.Errorf("list work cards: %w", err)
	}
	sessions, err := s.store.ListSessions(ctx, domain.ProjectID(projectID))
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	byID := map[domain.SessionID]domain.SessionRecord{}
	for _, sess := range sessions {
		byID[sess.ID] = sess
	}

	now := s.clock().UTC()
	var switched []string
	for _, card := range cards {
		if card.Status != domain.CardStatusRunning || card.SessionID == "" || card.PausedRetarget {
			continue
		}
		sess, exists := byID[domain.SessionID(card.SessionID)]
		if !exists || sess.IsTerminated || sess.Metadata.RuntimeHandleID == "" {
			continue
		}
		// A commander-linked card is coordinated by that session, not executed in
		// that terminal. Never rate-limit-switch (and kill) the commander.
		if isCardCommander(sess) {
			continue
		}
		out, err := s.capture.GetOutput(ctx, ports.RuntimeHandle{ID: sess.Metadata.RuntimeHandleID}, switchCaptureLines)
		if err != nil {
			continue
		}
		if !DetectRateLimit(sess.Harness, out) {
			continue
		}
		cooling := s.coolingForProject(projectID, now)
		s.markCooldown(projectID, card.Agent, now.Add(time.Duration(cfg.LimitCooldownMinutes)*time.Minute))
		cooling = s.coolingForProject(projectID, now)
		next, ok := PickFallbackAgent(card.Agent, cfg.FallbackAgents, cooling, now)
		if !ok {
			continue
		}
		if err := s.switchCard(ctx, card, sess, next, now); err != nil {
			return switched, err
		}
		switched = append(switched, card.ID)
	}
	return switched, nil
}

func (s *AgentSwitcher) switchCard(ctx context.Context, card domain.WorkCard, old domain.SessionRecord, nextAgent string, now time.Time) error {
	fromAgent := card.Agent
	fromSession := card.SessionID
	if _, err := s.killer.Kill(ctx, old.ID); err != nil {
		return fmt.Errorf("kill session %s for card %s: %w", old.ID, card.ID, err)
	}
	prompt := switchHandoffPrompt(card, fromAgent, nextAgent)
	session, err := s.spawner.Spawn(ctx, ports.SpawnConfig{
		ProjectID:   domain.ProjectID(card.ProjectID),
		Kind:        domain.KindWorker,
		Harness:     domain.AgentHarness(nextAgent),
		Branch:      old.Metadata.Branch,
		Prompt:      prompt,
		TargetPath:  firstNonEmpty(card.TargetPath, old.Metadata.TargetPath),
		DisplayName: card.Title,
	})
	if err != nil {
		return fmt.Errorf("spawn fallback agent %q for card %s: %w", nextAgent, card.ID, err)
	}
	card.Agent = nextAgent
	card.SessionID = string(session.ID)
	card.UpdatedAt = now
	if err := s.store.UpdateWorkCard(ctx, card); err != nil {
		return fmt.Errorf("update card %s after switch: %w", card.ID, err)
	}
	payload, _ := json.Marshal(map[string]string{
		"fromAgent":   fromAgent,
		"toAgent":     nextAgent,
		"fromSession": fromSession,
		"toSession":   string(session.ID),
		"reason":      "rate_limit",
	})
	return s.store.AppendWorkCardEvent(ctx, domain.WorkCardEvent{
		ID:        s.newID(),
		CardID:    card.ID,
		ProjectID: card.ProjectID,
		Kind:      workCardEventAgentSwitched,
		Payload:   string(payload),
		CreatedAt: now,
	})
}

func switchHandoffPrompt(card domain.WorkCard, fromAgent, toAgent string) string {
	var b strings.Builder
	b.WriteString("AO handoff: previous coding agent ")
	b.WriteString(fromAgent)
	b.WriteString(" hit a rate limit. Continue as ")
	b.WriteString(toAgent)
	b.WriteString(" on the same task.\n\n")
	b.WriteString(card.Title)
	if strings.TrimSpace(card.Notes) != "" {
		b.WriteString("\n\n")
		b.WriteString(card.Notes)
	}
	return b.String()
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func (s *AgentSwitcher) cooldownKey(projectID, agent string) string {
	return projectID + "\x00" + agent
}

func (s *AgentSwitcher) markCooldown(projectID, agent string, until time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cooldown == nil {
		s.cooldown = map[string]time.Time{}
	}
	s.cooldown[s.cooldownKey(projectID, agent)] = until
}

func (s *AgentSwitcher) coolingForProject(projectID string, now time.Time) map[string]time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]time.Time{}
	prefix := projectID + "\x00"
	for k, until := range s.cooldown {
		if !strings.HasPrefix(k, prefix) || !until.After(now) {
			continue
		}
		out[strings.TrimPrefix(k, prefix)] = until
	}
	return out
}
