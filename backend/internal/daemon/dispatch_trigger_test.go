package daemon

import (
	"context"
	"io"
	"log/slog"
	"testing"

	workboardsvc "github.com/modernagent/modern-agent/backend/internal/service/workboard"
)

func TestDispatchTriggerRetainsLastAttemptAfterWorkerExits(t *testing.T) {
	trigger := &DispatchTrigger{
		dispatcher: workboardsvc.NewDispatcher(workboardsvc.DispatchDeps{}),
		ctx:        context.Background(),
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		projects:   make(map[string]*dispatchProject),
	}

	trigger.Kick("project-1")
	trigger.workers.Wait()

	got := trigger.LastDispatchAttempt("project-1")
	if got.AttemptedAt.IsZero() {
		t.Fatal("LastDispatchAttempt() returned an empty result after dispatch completed")
	}
	if got.Result != "wip_full" {
		t.Fatalf("LastDispatchAttempt().Result = %q, want wip_full", got.Result)
	}
}
