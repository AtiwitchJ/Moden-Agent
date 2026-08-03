package commander

import (
	"context"
	"time"

	"github.com/modernagent/modern-agent/backend/internal/domain"
)

// Phase represents the active phase of a work card's lifecycle.
type Phase string

const (
	PhaseCoding  Phase = "coding"
	PhaseReview  Phase = "review"
	PhaseTesting Phase = "testing"
)

// Orchestrator is the command side of the Hermes Director pattern. The daemon
// is the only orchestrator; agents are workers that report back through the
// result/failure callbacks.
type Orchestrator interface {
	// Tick is called periodically to advance work cards through their phases.
	Tick(ctx context.Context, projectID string) error
	// OnAgentFailed is called when a worker agent fails or times out.
	OnAgentFailed(ctx context.Context, cardID string, attempt AgentAttempt) error
	// OnAgentCompleted is called when a worker agent finishes successfully.
	OnAgentCompleted(ctx context.Context, cardID string, result AgentResult) error
	// OnSignal is called when an external signal (PRReady, CIFailed, PRClosed, …) arrives.
	OnSignal(ctx context.Context, cardID string, signal Signal) error
}

// Spawner drives native agent sessions on behalf of the orchestrator.
type Spawner interface {
	Spawn(ctx context.Context, spec SpawnSpec) (SessionHandle, error)
	Stop(ctx context.Context, sessionID string) error
	Inject(ctx context.Context, sessionID string, message string) error
}

// SpawnSpec describes a session to spawn for a work card at a given phase.
type SpawnSpec struct {
	CardID       string
	ProjectID    string
	Phase        Phase
	Agent        string
	Briefing     string
	ParentCard   *WorkCard
	CycleHistory *RedoCycle
}

// WorkCard aliases domain.WorkCard to keep the port interface self-contained.
type WorkCard = domain.WorkCard

// RedoCycle aliases domain.RedoCycle to keep the port interface self-contained.
type RedoCycle = domain.RedoCycle

// AgentAttempt records a failed or inconclusive agent run.
type AgentAttempt struct {
	Phase         Phase
	Agent         string
	FailureReason string // timeout|error|inconclusive|spawn_failed
	AttemptNumber int
}

// AgentResult records a completed agent run.
type AgentResult struct {
	Phase   Phase
	Verdict string // approved|changes_requested|inconclusive|pass|fail
	Output  string
}

// Signal represents an external event that can affect a work card.
type Signal struct {
	Kind    string // PRReady|CIFailed|PRClosed|...
	Payload string
}

// SessionHandle identifies a spawned native session.
type SessionHandle struct {
	ID            string
	NativeID      string
	WorkspacePath string
}

// TickMetadata carries timing information for observability.
type TickMetadata struct {
	TickAt time.Time
}