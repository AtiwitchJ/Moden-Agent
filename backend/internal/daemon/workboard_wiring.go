package daemon

import (
	"context"
	"log/slog"

	"github.com/modernagent/modern-agent/backend/internal/adapters/runtime/runtimeselect"
	"github.com/modernagent/modern-agent/backend/internal/observe"
	sessionsvc "github.com/modernagent/modern-agent/backend/internal/service/session"
	workboardsvc "github.com/modernagent/modern-agent/backend/internal/service/workboard"
	"github.com/modernagent/modern-agent/backend/internal/storage/sqlite"
)

// startWorkboardDispatcher gives every active project a chance to promote due
// cards, claim ready work, answer timed-out prompts, and switch agents that hit
// rate limits. It returns the daemon-owned dispatch trigger and a channel that
// closes once all background reconcilers have exited.
func startWorkboardDispatcher(ctx context.Context, store *sqlite.Store, sessions *sessionsvc.Service, runtime runtimeselect.Runtime, logger *slog.Logger) (*DispatchTrigger, <-chan struct{}) {
	if logger == nil {
		logger = slog.Default()
	}
	dispatcher := workboardsvc.NewDispatcher(workboardsvc.DispatchDeps{Store: store, Spawner: sessions})
	trigger := NewDispatchTrigger(ctx, dispatcher, store, logger)

	answerer := workboardsvc.NewAnswerer(workboardsvc.AnswerDeps{Store: store, Sender: sessions})
	switcher := workboardsvc.NewAgentSwitcher(workboardsvc.SwitchDeps{
		Store: store, Spawner: sessions, Killer: sessions, Capture: runtime,
	})
	nudger := workboardsvc.NewStallNudger(workboardsvc.StallNudgeDeps{Store: store, Sender: sessions})

	answererDone := startProjectReconciler(ctx, store, answerer, logger, "workboard answerer")
	switcherDone := startProjectReconciler(ctx, store, switcher, logger, "workboard switcher")
	nudgerDone := startProjectReconciler(ctx, store, nudger, logger, "workboard stall nudger")

	allDone := make(chan struct{})
	go func() {
		defer close(allDone)
		<-trigger.Done()
		<-answererDone
		<-switcherDone
		<-nudgerDone
	}()
	return trigger, allDone
}

// projectReconciler is the shared shape of the answerer and switcher, both of
// which run a periodic pass over every active project.
type projectReconciler interface {
	ReconcileProject(ctx context.Context, projectID string) ([]string, error)
}

// startProjectReconciler runs a one-minute poll loop that reconciles every
// active project. Errors are logged and never abort the loop.
func startProjectReconciler(ctx context.Context, store *sqlite.Store, reconciler projectReconciler, logger *slog.Logger, name string) <-chan struct{} {
	return observe.StartPollLoop(ctx, workboardDispatchInterval, func(ctx context.Context) error {
		projects, err := store.ListProjects(ctx)
		if err != nil {
			return err
		}
		for _, project := range projects {
			if _, err := reconciler.ReconcileProject(ctx, project.ID); err != nil {
				logger.Warn(name+": project reconcile failed", "project", project.ID, "err", err)
			}
		}
		return nil
	}, logger, name)
}
