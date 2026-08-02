package daemon

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/modernagent/modern-agent/backend/internal/observe"
	workboardsvc "github.com/modernagent/modern-agent/backend/internal/service/workboard"
	"github.com/modernagent/modern-agent/backend/internal/storage/sqlite"
)

const workboardDispatchInterval = time.Minute

// DispatchTrigger is the daemon-owned entry point for project dispatch.
// It multiplexes two wake sources onto a single per-project DispatchOnce call:
// the existing periodic reconciliation poll, and an explicit in-process Kick
// when durable state changes and may allow new cards to run.
type DispatchTrigger struct {
	dispatcher *workboardsvc.Dispatcher
	ctx        context.Context
	logger     *slog.Logger

	mu       sync.Mutex
	projects map[string]*dispatchProject
	closing  bool
	workers  sync.WaitGroup

	pollDone   <-chan struct{}
	done       chan struct{}
}

type dispatchProject struct {
	pending bool
	running bool
}

// NewDispatchTrigger wires a dispatcher and starts the periodic poll loop.
// The returned trigger is immediately usable; Kick becomes a no-op once ctx is
// done because the background goroutine has exited.
func NewDispatchTrigger(ctx context.Context, dispatcher *workboardsvc.Dispatcher, store *sqlite.Store, logger *slog.Logger) *DispatchTrigger {
	if logger == nil {
		logger = slog.Default()
	}
	trigger := &DispatchTrigger{
		dispatcher: dispatcher,
		ctx:        ctx,
		logger:     logger,
		projects:   make(map[string]*dispatchProject),
		done:       make(chan struct{}),
	}
	trigger.pollDone = observe.StartPollLoop(ctx, workboardDispatchInterval, func(ctx context.Context) error {
		projects, err := store.ListProjects(ctx)
		if err != nil {
			return err
		}
		for _, project := range projects {
			trigger.Kick(project.ID)
		}
		return nil
	}, logger, "workboard dispatch trigger")
	go trigger.wait()
	return trigger
}

// Kick asynchronously asks the trigger to dispatch one project. Concurrent
// kicks for a project are coalesced into at most one follow-up pass, while
// projects may dispatch independently. The database claim remains the durable
// WIP guard across daemon instances.
func (t *DispatchTrigger) Kick(projectID string) {
	if t == nil || t.dispatcher == nil || projectID == "" {
		return
	}
	t.mu.Lock()
	if t.closing || t.ctx.Err() != nil {
		t.mu.Unlock()
		return
	}
	project := t.projects[projectID]
	start := false
	if project == nil {
		project = &dispatchProject{running: true}
		t.projects[projectID] = project
		start = true
		t.workers.Add(1)
	}
	project.pending = true
	if !start {
		t.mu.Unlock()
		return
	}
	t.mu.Unlock()
	go func() {
		defer t.workers.Done()
		t.dispatchProject(projectID)
	}()
}

func (t *DispatchTrigger) dispatchProject(projectID string) {
	for {
		t.mu.Lock()
		project := t.projects[projectID]
		if project == nil || t.closing || t.ctx.Err() != nil {
			delete(t.projects, projectID)
			t.mu.Unlock()
			return
		}
		project.pending = false
		t.mu.Unlock()

		attemptCtx, cancel := context.WithTimeout(t.ctx, 30*time.Second)
		_, err := t.dispatcher.DispatchOnce(attemptCtx, projectID)
		cancel()
		if err != nil && t.ctx.Err() == nil {
			t.logger.Warn("workboard dispatch trigger: project dispatch failed", "project", projectID, "err", err)
		}

		t.mu.Lock()
		project = t.projects[projectID]
		if project == nil || t.closing || t.ctx.Err() != nil || !project.pending {
			delete(t.projects, projectID)
			t.mu.Unlock()
			return
		}
		t.mu.Unlock()
	}
}

func (t *DispatchTrigger) wait() {
	<-t.pollDone
	t.mu.Lock()
	t.closing = true
	t.mu.Unlock()
	t.workers.Wait()
	close(t.done)
}

// Done returns a channel that closes when the underlying periodic poll loop
// exits. It exists primarily for graceful-shutdown coordination.
func (t *DispatchTrigger) Done() <-chan struct{} {
	if t == nil || t.done == nil {
		closed := make(chan struct{})
		close(closed)
		return closed
	}
	return t.done
}
