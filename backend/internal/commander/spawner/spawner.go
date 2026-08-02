// Package spawner implements the Hermes-aware session launcher for workboard cards.
package spawner

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/modernagent/modern-agent/backend/internal/adapters"
	"github.com/modernagent/modern-agent/backend/internal/domain"
	"github.com/modernagent/modern-agent/backend/internal/ports"
)

// Phase describes which phase of the card lifecycle a session belongs to.
type Phase string

const (
	PhaseCoding  Phase = "coding"
	PhaseReview  Phase = "review"
	PhaseTesting Phase = "testing"
)

// SessionHandle identifies a spawned session and carries the agent's native session ID
// (e.g. the hermes resume token) for future Inject or Stop calls.
type SessionHandle struct {
	ID            string // AO-internal session ID
	NativeID      string // agent-native ID (e.g. hermes session token)
	WorkspacePath string
}

// SpawnSpec describes the session to launch.
type SpawnSpec struct {
	CardID    string
	ProjectID string
	Phase     Phase
	Agent     string // harness id e.g. "hermes", "claude-code"
	Briefing  string

	// ParentCard is the full card record at spawn time. Nil for non-workboard sessions.
	ParentCard *domain.WorkCard
	// CycleHistory is the current RedoCycle with its Findings. Nil for first attempt.
	CycleHistory *domain.RedoCycle
}

// ActiveSessionStore persists a (card_id → session) fact so the orchestrator
// can detect orphans and the active_session table provides WIP-counting context.
// Implementations must enforce a unique constraint on card_id so concurrent
// inserts for the same card fail atomically.
type ActiveSessionStore interface {
	InsertActiveSession(ctx context.Context, s InsertActiveSession) error
}

// InsertActiveSession is the row value written to the active_session table.
type InsertActiveSession struct {
	CardID    string
	SessionID string
	Phase     Phase
	Agent     string
}

// AgentLauncher starts a session for a given harness.
type AgentLauncher interface {
	Spawn(ctx context.Context, spec SpawnSpec) (SessionHandle, error)
}

// ErrCardSpawnInProgress is returned when another goroutine is already launching
// a session for the same card. The caller can retry after that launch completes.
var ErrCardSpawnInProgress = errors.New("card spawn already in progress")

// cardLock prevents concurrent Spawn calls for the same card from racing on
// the active_session insert. It is per-card, not global — two different
// cards can spawn concurrently without blocking each other.
type cardLock struct {
	mu     sync.Mutex
	active bool
}

func newCardLock() *cardLock {
	return &cardLock{}
}

// TryAcquire reports whether this caller owns the lock. A caller that did not
// acquire it must not launch a second process for the same card.
func (l *cardLock) TryAcquire() (release func(), ok bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.active {
		return nil, false
	}
	l.active = true
	return func() {
		l.mu.Lock()
		l.active = false
		l.mu.Unlock()
	}, true
}

// Deps is the dependency set for a Spawner.
type Deps struct {
	Store    ActiveSessionStore
	Launcher AgentLauncher
	Clock    func() int64 // unix epoch ms
	NewID    func() string
}

// Spawner launches coding/reviewer/tester sessions with per-card concurrency guards
// and a full briefing payload.
type Spawner struct {
	store    ActiveSessionStore
	launcher AgentLauncher
	clock    func() int64
	newID    func() string
	reg      *adapters.Registry
	locks    map[string]*cardLock
	locksMu  sync.RWMutex
}

// New builds a Spawner from its dependencies.
func New(launcher AgentLauncher, store ActiveSessionStore, reg *adapters.Registry, clock func() int64, newID func() string) *Spawner {
	if clock == nil {
		clock = func() int64 { return time.Now().UnixMilli() }
	}
	if newID == nil {
		newID = func() string { return uuid.NewString() }
	}
	return &Spawner{
		store:    store,
		launcher: launcher,
		clock:    clock,
		newID:    newID,
		reg:      reg,
		locks:    make(map[string]*cardLock),
	}
}

func (s *Spawner) lockFor(cardID string) *cardLock {
	s.locksMu.RLock()
	l, ok := s.locks[cardID]
	s.locksMu.RUnlock()
	if ok {
		return l
	}
	s.locksMu.Lock()
	defer s.locksMu.Unlock()
	if l, ok := s.locks[cardID]; ok {
		return l
	}
	l = newCardLock()
	s.locks[cardID] = l
	return l
}

// Spawn launches a session for the given spec. It is safe for concurrent calls
// for different cards. A second concurrent call for the same card fails before
// it can launch another process. On success the card is recorded in
// active_session so the orchestrator can track it.
func (s *Spawner) Spawn(ctx context.Context, spec SpawnSpec) (SessionHandle, error) {
	if s.reg != nil {
		if _, ok := s.reg.Get(string(domain.AgentHarness(spec.Agent))); !ok {
			return SessionHandle{}, fmt.Errorf("agent harness %q not found", spec.Agent)
		}
	}

	cardLock := s.lockFor(spec.CardID)
	release, ok := cardLock.TryAcquire()
	if !ok {
		return SessionHandle{}, fmt.Errorf("%w: %s", ErrCardSpawnInProgress, spec.CardID)
	}
	defer release()

	handle, err := s.launcher.Spawn(ctx, spec)
	if err != nil {
		return SessionHandle{}, err
	}

	if s.store != nil {
		insert := InsertActiveSession{
			CardID:    spec.CardID,
			SessionID: handle.ID,
			Phase:     spec.Phase,
			Agent:     spec.Agent,
		}
		if err := s.store.InsertActiveSession(ctx, insert); err != nil {
			return SessionHandle{}, err
		}
	}

	return handle, nil
}

// RegistryLauncher looks up the harness adapter from the agent registry and
// delegates to the adapter's launch command.
type RegistryLauncher struct {
	Reg *adapters.Registry
}

// Spawn implements AgentLauncher. It resolves the harness adapter, builds the
// launch argv via GetLaunchCommand, and returns a handle with the session ID.
// The actual process start is handled by the session runtime (tmux/pty) which
// is owned by the session service — RegistryLauncher only produces the argv.
// TODO(Task 6): replace the CardID placeholder with the real session-service
// handle once the orchestrator is wired to session creation.
func (l *RegistryLauncher) Spawn(ctx context.Context, spec SpawnSpec) (SessionHandle, error) {
	if spec.Agent == "" {
		return SessionHandle{}, errors.New("agent harness is required")
	}
	a, ok := l.Reg.Get(string(domain.AgentHarness(spec.Agent)))
	if !ok {
		return SessionHandle{}, fmt.Errorf("agent harness %q not found", spec.Agent)
	}
	agent, ok := a.(ports.Agent)
	if !ok {
		return SessionHandle{}, fmt.Errorf("adapter for %q is not an agent", spec.Agent)
	}

	argv, err := agent.GetLaunchCommand(ctx, ports.LaunchConfig{
		SessionID: spec.CardID,
		Prompt:    spec.Briefing,
		WorkspacePath: func() string {
			if spec.ParentCard != nil {
				return spec.ParentCard.TargetPath
			}
			return ""
		}(),
	})
	if err != nil {
		return SessionHandle{}, fmt.Errorf("build launch command for %s: %w", spec.Agent, err)
	}

	_ = argv // argv is produced but the actual process spawn is handled by the session runtime
	return SessionHandle{
		ID:       spec.CardID,
		NativeID: "",
	}, nil
}
