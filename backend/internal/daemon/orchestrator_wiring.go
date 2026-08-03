package daemon

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/modernagent/modern-agent/backend/internal/cdc"
	"github.com/modernagent/modern-agent/backend/internal/storage/sqlite"
)

// OrchestratorWiring holds the daemon-side wiring for the workboard orchestrator.
type OrchestratorWiring struct {
	orchestrator  OrchestratorTick
	cdcSub       <-chan CardChangeEvent
	tickInterval time.Duration
	stopCh       chan struct{}
	wg           sync.WaitGroup
}

// CardChangeEvent is received from the CDC work_card_changed subscription.
type CardChangeEvent struct {
	CardID    string
	ProjectID string
	NewStatus string
	OldStatus string
}

// OrchestratorTick is the minimal interface the orchestrator must implement to be
// wired into the daemon tick loop. *commander.orchestrator.ConfiguredOrchestrator
// satisfies it.
type OrchestratorTick interface {
	Tick(ctx context.Context, projectID string) error
}

// OrchestratorConfig holds the dependencies for wiring the orchestrator.
type OrchestratorConfig struct {
	Orchestrator OrchestratorTick
}

// WireOrchestrator subscribes the orchestrator to CDC card-change events and
// starts the periodic tick goroutine. Call from daemon boot. Returns nil safely
// if the orchestrator is not yet available (Task 6 pending).
func WireOrchestrator(ctx context.Context, cfg OrchestratorConfig, bcast *cdc.Broadcaster, store *sqlite.Store, logger *slog.Logger) (*OrchestratorWiring, error) {
	if cfg.Orchestrator == nil {
		return nil, nil // orchestrator not available yet (Task 6 pending)
	}
	if logger == nil {
		logger = slog.Default()
	}
	wiring := &OrchestratorWiring{
		orchestrator:  cfg.Orchestrator,
		cdcSub:       SubscribeWorkCardChanges(ctx, bcast, logger),
		tickInterval: 30 * time.Second,
		stopCh:       make(chan struct{}),
	}

	wiring.wg.Add(1)
	go wiring.run(ctx, store, logger)

	return wiring, nil
}

// run fans the tick interval and CDC events into orchestrator.Tick calls.
func (w *OrchestratorWiring) run(ctx context.Context, store *sqlite.Store, logger *slog.Logger) {
	defer w.wg.Done()
	ticker := time.NewTicker(w.tickInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stopCh:
			return
		case ev := <-w.cdcSub:
			if err := w.orchestrator.Tick(ctx, ev.ProjectID); err != nil {
				logger.Warn("orchestrator wiring: tick on card change failed", "project", ev.ProjectID, "err", err)
			}
		case <-ticker.C:
			projects, err := store.ListProjects(ctx)
			if err != nil {
				logger.Warn("orchestrator wiring: list projects failed", "err", err)
				continue
			}
			for _, p := range projects {
				if err := w.orchestrator.Tick(ctx, p.ID); err != nil {
					logger.Warn("orchestrator wiring: periodic tick failed", "project", p.ID, "err", err)
				}
			}
		}
	}
}

// Stop gracefully stops the tick goroutine.
func (w *OrchestratorWiring) Stop() {
	if w == nil {
		return
	}
	close(w.stopCh)
	w.wg.Wait()
}
