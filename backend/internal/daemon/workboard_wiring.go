package daemon

import (
	"context"
	"log/slog"
	"time"

	"github.com/modernagent/modern-agent/backend/internal/adapters/runtime/runtimeselect"
	"github.com/modernagent/modern-agent/backend/internal/observe"
	sessionsvc "github.com/modernagent/modern-agent/backend/internal/service/session"
	workboardsvc "github.com/modernagent/modern-agent/backend/internal/service/workboard"
	"github.com/modernagent/modern-agent/backend/internal/storage/sqlite"
)

const workboardDispatchInterval = time.Minute

// startWorkboardDispatcher periodically gives every active project a chance
// to promote due cards, claim ready work, answer timed-out prompts, and switch
// agents that hit rate limits. DispatchOnce owns per-project serialization, so
// the loop stays a thin daemon lifecycle wrapper.
func startWorkboardDispatcher(ctx context.Context, store *sqlite.Store, sessions *sessionsvc.Service, runtime runtimeselect.Runtime, logger *slog.Logger) <-chan struct{} {
	if logger == nil {
		logger = slog.Default()
	}
	dispatcher := workboardsvc.NewDispatcher(workboardsvc.DispatchDeps{Store: store, Spawner: sessions})
	answerer := workboardsvc.NewAnswerer(workboardsvc.AnswerDeps{Store: store, Sender: sessions})
	switcher := workboardsvc.NewAgentSwitcher(workboardsvc.SwitchDeps{
		Store: store, Spawner: sessions, Killer: sessions, Capture: runtime,
	})
	return observe.StartPollLoop(ctx, workboardDispatchInterval, func(ctx context.Context) error {
		projects, err := store.ListProjects(ctx)
		if err != nil {
			return err
		}
		for _, project := range projects {
			if _, err := dispatcher.DispatchOnce(ctx, project.ID); err != nil {
				logger.Warn("workboard dispatcher: project dispatch failed", "project", project.ID, "err", err)
			}
			if _, err := answerer.ReconcileProject(ctx, project.ID); err != nil {
				logger.Warn("workboard answerer: project reconcile failed", "project", project.ID, "err", err)
			}
			if _, err := switcher.ReconcileProject(ctx, project.ID); err != nil {
				logger.Warn("workboard switcher: project reconcile failed", "project", project.ID, "err", err)
			}
		}
		return nil
	}, logger, "workboard dispatcher")
}
