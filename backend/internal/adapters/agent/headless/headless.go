// Package headless adapts agent adapters whose interactive launch command is
// already a non-interactive, self-terminating run (`aider -m`, `pi --print`,
// `goose run -t`, …) onto ports.AgentHeadless.
//
// Those adapters need no second argv builder: their launch command is already
// the one-shot command. What a one-shot spawn additionally requires is that the
// run cannot stall on an approval prompt and cannot degrade into an interactive
// session for want of a prompt. FromLaunch enforces both in one place so a
// dozen adapters do not each repeat the same five lines.
package headless

import (
	"context"
	"errors"
	"strings"

	"github.com/modernagent/modern-agent/backend/internal/ports"
)

// ErrPromptRequired reports a one-shot launch with no instruction to run.
var ErrPromptRequired = errors.New("a one-shot launch requires a prompt")

// FromLaunch builds a headless command by calling the adapter's own launch
// command with approvals bypassed. A headless run has nobody to answer an
// approval prompt, so a mapped permission mode would be a way to hang.
func FromLaunch(
	ctx context.Context,
	launch func(context.Context, ports.LaunchConfig) ([]string, error),
	cfg ports.LaunchConfig,
) ([]string, error) {
	if strings.TrimSpace(cfg.Prompt) == "" {
		return nil, ErrPromptRequired
	}
	cfg.Permissions = ports.PermissionModeBypassPermissions
	return launch(ctx, cfg)
}
