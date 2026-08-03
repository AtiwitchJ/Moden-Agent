package daemon

import (
	"context"

	"github.com/modernagent/modern-agent/backend/internal/commander"
	"github.com/modernagent/modern-agent/backend/internal/commander/spawner"
)

// spawnerAdapter adapts *spawner.Spawner (commander/spawner's Hermes-aware
// session launcher, typed on spawner.SpawnSpec/spawner.SessionHandle) to
// commander.Spawner (the port orchestrator.Config.Spawner requires, typed on
// commander.SpawnSpec/commander.SessionHandle). The two packages define
// distinct named types for these shapes, so *spawner.Spawner does not satisfy
// commander.Spawner structurally -- this adapter converts field-by-field.
type spawnerAdapter struct {
	spawner *spawner.Spawner
}

func (a spawnerAdapter) Spawn(ctx context.Context, spec commander.SpawnSpec) (commander.SessionHandle, error) {
	handle, err := a.spawner.Spawn(ctx, spawner.SpawnSpec{
		CardID:       spec.CardID,
		ProjectID:    spec.ProjectID,
		Phase:        spawner.Phase(spec.Phase),
		Agent:        spec.Agent,
		Briefing:     spec.Briefing,
		ParentCard:   spec.ParentCard,
		CycleHistory: spec.CycleHistory,
	})
	if err != nil {
		return commander.SessionHandle{}, err
	}
	return commander.SessionHandle{
		ID:            handle.ID,
		NativeID:      handle.NativeID,
		WorkspacePath: handle.WorkspacePath,
	}, nil
}

// Stop and Inject are no-ops: nothing in the orchestrator tick/fallback/
// recovery loop calls them yet (commander/spawner.AgentLauncher only exposes
// Spawn today), and *spawner.Spawner has no stop/inject capability to
// delegate to.
// ponytail: stubbed until a real stop/inject path is wired through
// AgentLauncher; upgrade when the orchestrator needs to kill or nudge a
// spawned session directly instead of waiting for OnAgentFailed/OnSignal.
func (a spawnerAdapter) Stop(ctx context.Context, sessionID string) error {
	return nil
}

func (a spawnerAdapter) Inject(ctx context.Context, sessionID string, message string) error {
	return nil
}
