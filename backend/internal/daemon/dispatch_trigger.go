package daemon

import (
	"context"
	"log/slog"
	"strings"
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
	last    workboardsvc.DispatchResult
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
		claimed, err := t.dispatcher.DispatchOnce(attemptCtx, projectID)
		cancel()
		now := time.Now().UTC()
		result := workboardsvc.DispatchResult{AttemptedAt: now, Result: "success"}
		if err != nil {
			result.Result = "error"
			result.Error = safeDispatchError(err)
			if t.ctx.Err() == nil {
				t.logger.Warn("workboard dispatch trigger: project dispatch failed", "project", projectID, "err", err)
			}
		} else if len(claimed) == 0 {
			result.Result = "wip_full"
		}
		t.mu.Lock()
		project = t.projects[projectID]
		if project != nil {
			project.last = result
		}
		t.mu.Unlock()

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

// Ready reports whether the trigger is currently accepting kicks. It answers
// false once the daemon context is cancelled or the periodic poll loop exits.
func (t *DispatchTrigger) Ready() bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return !t.closing && t.ctx.Err() == nil
}

// LastDispatchAttempt returns the most recent result held by the daemon process
// for the project, or an empty result when the project has not been dispatched
// yet.
func (t *DispatchTrigger) LastDispatchAttempt(projectID string) workboardsvc.DispatchResult {
	if t == nil || projectID == "" {
		return workboardsvc.DispatchResult{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if project, ok := t.projects[projectID]; ok {
		return project.last
	}
	return workboardsvc.DispatchResult{}
}

// safeDispatchError maps internal errors to a stable, non-secret code for the UI.
func safeDispatchError(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	switch {
	case strings.Contains(s, "not found"):
		return "PROJECT_NOT_FOUND"
	case strings.Contains(s, "requires spawn rollback support"):
		return "ROLLBACK_UNAVAILABLE"
	case strings.Contains(s, "requires orchestrator spawn support"):
		return "ORCHESTRATOR_UNAVAILABLE"
	default:
		return "DISPATCH_FAILED"
	}
}
