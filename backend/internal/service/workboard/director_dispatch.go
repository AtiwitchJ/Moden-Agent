package workboard

import (
	"context"
	"fmt"
	"time"

	"github.com/modernagent/modern-agent/backend/internal/domain"
	"github.com/modernagent/modern-agent/backend/internal/ports"
)

// directorEnabled reports whether the project opted into AO's Director agent.
func directorEnabled(project domain.ProjectRecord) bool {
	return project.Config.Director.Harness != ""
}

// isProjectDirector reports whether a session is this project's live Director.
// Deliberately independent of isHermesCommander: the two commander mechanisms
// share no state and must not share a predicate.
func isProjectDirector(session domain.SessionRecord, harness domain.AgentHarness) bool {
	return !session.IsTerminated && session.Kind == domain.KindOrchestrator && session.Harness == harness
}

// dispatchToDirector ensures the project has one live Director session. The
// Director drives cards itself through `ao workboard card transition`, so this
// path does not claim cards the way the worker path does — it only guarantees
// the commander is running.
func (d *Dispatcher) dispatchToDirector(ctx context.Context, project domain.ProjectRecord, _ []domain.WorkCard, _ time.Time) ([]string, error) {
	harness := project.Config.Director.Harness

	sessions, err := d.store.ListSessions(ctx, domain.ProjectID(project.ID))
	if err != nil {
		return nil, fmt.Errorf("list sessions for project %s: %w", project.ID, err)
	}
	for _, session := range sessions {
		if isProjectDirector(session, harness) {
			return nil, nil // already running
		}
	}

	// Spawn directly rather than through SpawnOrchestrator: that helper takes no
	// harness and resolves it from Config.Orchestrator.Harness, which is the
	// wrong field for the Director.
	if _, err := d.spawner.Spawn(ctx, ports.SpawnConfig{
		ProjectID: domain.ProjectID(project.ID),
		Kind:      domain.KindOrchestrator,
		Harness:   harness,
		Prompt:    fmt.Sprintf("Drive the work cards for project %s.", project.ID),
	}); err != nil {
		return nil, fmt.Errorf("spawn director for project %s: %w", project.ID, err)
	}
	return nil, nil
}
