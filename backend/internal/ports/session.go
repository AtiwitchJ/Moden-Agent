package ports

import (
	"errors"

	"github.com/modernagent/modern-agent/backend/internal/domain"
)

// ErrSessionNotFound reports an observation for an unknown session id.
var ErrSessionNotFound = errors.New("session not found")

// SpawnConfig is the request to start a new session: which project/issue, which
// agent harness, and the branch/prompt the agent launches with.
type SpawnConfig struct {
	ProjectID domain.ProjectID
	IssueID   domain.IssueID
	Kind      domain.SessionKind
	Harness   domain.AgentHarness
	Branch    string
	Prompt    string
	// TargetPath is an optional absolute source path under the project's
	// registered repository. Session Manager maps it into the new worktree
	// before launching the agent; empty preserves the worktree-root default.
	TargetPath string
	// DisplayName is the user-facing sidebar label. Empty falls back to the
	// session id in the read model (e.g. orchestrator sessions).
	DisplayName string
	// DirectorCardID scopes a Director harness launch to the work card it owns.
	// It is intentionally harness-specific: ordinary agent sessions must not
	// acquire workboard authority merely by receiving an environment variable.
	DirectorCardID string
	// OneShot runs the agent in its headless mode: it executes the prompt to
	// completion and exits, instead of holding an interactive session open.
	// The session is otherwise ordinary — worktree, runtime pane, sidebar row —
	// so a caller can still watch it. Requires the harness's adapter to
	// implement AgentHeadless.
	OneShot bool
}
