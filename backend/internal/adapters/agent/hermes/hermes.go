// Package hermes implements the hermes agent adapter: launching new sessions,
// resuming sessions by native ID, and reading session info.
//
// hermes differs from other agents in that it doesn't have full hooks support,
// so GetAgentHooks and SessionInfo are no-ops for now. Session tracking is
// done through basic session ID management only.
package hermes

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/modernagent/modern-agent/backend/internal/adapters"
	"github.com/modernagent/modern-agent/backend/internal/ports"
)

const (
	// adapterID is the registry id and the value users pass to
	// `ao spawn --agent`. It matches domain.HarnessHermes.
	adapterID = "hermes"
)

// Plugin is the hermes agent adapter. It is safe for concurrent use; the
// binary path is resolved once and cached under binaryMu.
type Plugin struct {
	binaryMu       sync.Mutex
	resolvedBinary string
}

// New returns a ready-to-register hermes adapter.
func New() *Plugin {
	return &Plugin{}
}

var _ adapters.Adapter = (*Plugin)(nil)
var _ ports.Agent = (*Plugin)(nil)
var _ ports.AgentHeadless = (*Plugin)(nil)

// Manifest returns the adapter's static self-description.
func (p *Plugin) Manifest() adapters.Manifest {
	return adapters.Manifest{
		ID:          adapterID,
		Name:        "hermes",
		Description: "Run hermes worker sessions.",
		Version:     "0.0.1",
		Capabilities: []adapters.Capability{
			adapters.CapabilityAgent,
		},
	}
}

// GetConfigSpec reports the agent-specific config keys. hermes exposes none yet.
func (p *Plugin) GetConfigSpec(ctx context.Context) (ports.ConfigSpec, error) {
	if err := ctx.Err(); err != nil {
		return ports.ConfigSpec{}, err
	}
	return ports.ConfigSpec{}, nil
}

// GetLaunchCommand builds the argv to start an interactive hermes session.
// Shape:
//
//	hermes [--yolo]
//
// The session runs in the worktree because the runtime sets its cwd. Hermes
// does not support --cwd or a positional interactive prompt, so cfg.WorkspacePath,
// cfg.Prompt, cfg.SystemPrompt, and cfg.SystemPromptFile are intentionally not
// represented in argv. The --yolo flag corresponds to bypass-permissions mode.
//
// We intentionally do not pass --session on launch: cfg.SessionID is the
// AO-internal id, not a hermes-native session id. Letting hermes mint its own
// native session id (captured by hooks into session metadata) keeps launch
// consistent with GetRestoreCommand, which resumes using that native id.
func (p *Plugin) GetLaunchCommand(ctx context.Context, cfg ports.LaunchConfig) (cmd []string, err error) {
	binary, err := p.hermesBinary(ctx)
	if err != nil {
		return nil, err
	}

	cmd = []string{binary}

	// Handle permission modes
	if cfg.Permissions == ports.PermissionModeBypassPermissions {
		cmd = append(cmd, "--yolo")
	}

	return cmd, nil
}

// GetHeadlessCommand builds the argv for a one-shot hermes run:
//
//	hermes chat -Q --yolo -q <prompt>
//
// `chat -q` is hermes's single-query mode: it answers once and exits, which is
// why this adapter's PromptDeliveryAfterStart strategy (needed for the TUI
// launch, which takes no prompt at all) does not apply here — the prompt rides
// the command. `-Q` suppresses the banner so the output is programmatic, and
// `--yolo` bypasses the approval prompts a headless run cannot answer.
func (p *Plugin) GetHeadlessCommand(ctx context.Context, cfg ports.LaunchConfig) (cmd []string, err error) {
	if strings.TrimSpace(cfg.Prompt) == "" {
		return nil, fmt.Errorf("hermes: a one-shot launch requires a prompt")
	}
	binary, err := p.hermesBinary(ctx)
	if err != nil {
		return nil, err
	}
	return []string{binary, "chat", "-Q", "--yolo", "-q", cfg.Prompt}, nil
}

// GetPromptDeliveryStrategy reports that Hermes receives prompts through its
// interactive terminal after it starts.
func (p *Plugin) GetPromptDeliveryStrategy(ctx context.Context, cfg ports.LaunchConfig) (ports.PromptDeliveryStrategy, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return ports.PromptDeliveryAfterStart, nil
}

// GetRestoreCommand rebuilds the argv that continues an existing hermes session:
// `hermes [--yolo] --resume <agentSessionId>`.
// It re-applies the permission flag but not the prompt, which the session
// already carries. ok is false when the native session id is not available.
func (p *Plugin) GetRestoreCommand(ctx context.Context, cfg ports.RestoreConfig) (cmd []string, ok bool, err error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	agentSessionID := strings.TrimSpace(cfg.Session.Metadata[ports.MetadataKeyAgentSessionID])
	if agentSessionID == "" {
		return nil, false, nil
	}

	binary, err := p.hermesBinary(ctx)
	if err != nil {
		return nil, false, err
	}

	cmd = []string{binary}

	if cfg.Permissions == ports.PermissionModeBypassPermissions {
		cmd = append(cmd, "--yolo")
	}

	cmd = append(cmd, "--resume", agentSessionID)
	return cmd, true, nil
}

// SessionInfo surfaces hermes session metadata. Currently a no-op since hermes
// doesn't have full hooks support like Claude Code and Codex. Returns false
// to indicate no metadata is available.
func (p *Plugin) SessionInfo(ctx context.Context, session ports.SessionRef) (ports.SessionInfo, bool, error) {
	if err := ctx.Err(); err != nil {
		return ports.SessionInfo{}, false, err
	}
	// No-op for now since hermes doesn't have full hooks support
	return ports.SessionInfo{}, false, nil
}

// ResolvehermesBinary returns the path to the hermes binary on this machine,
// searching PATH then a handful of well-known install locations.
// Returns "hermes" as a last-ditch fallback.
func ResolvehermesBinary(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	if runtime.GOOS == "windows" {
		for _, name := range []string{"hermes.cmd", "hermes.exe", "hermes"} {
			path, err := exec.LookPath(name)
			if err == nil && path != "" {
				return path, nil
			}
			if err := ctx.Err(); err != nil {
				return "", err
			}
		}

		candidates := []string{}
		if appData := os.Getenv("APPDATA"); appData != "" {
			candidates = append(candidates,
				filepath.Join(appData, "npm", "hermes.cmd"),
				filepath.Join(appData, "npm", "hermes.exe"),
			)
		}
		if home, err := os.UserHomeDir(); err == nil {
			candidates = append(candidates, filepath.Join(home, ".cargo", "bin", "hermes.exe"))
		}
		for _, candidate := range candidates {
			if fileExists(candidate) {
				return candidate, nil
			}
			if err := ctx.Err(); err != nil {
				return "", err
			}
		}

		return "", fmt.Errorf("hermes: %w", ports.ErrAgentBinaryNotFound)
	}

	if path, err := exec.LookPath("hermes"); err == nil && path != "" {
		return path, nil
	}

	candidates := []string{
		"/usr/local/bin/hermes",
		"/opt/homebrew/bin/hermes",
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates,
			filepath.Join(home, ".local", "bin", "hermes"),
			filepath.Join(home, ".cargo", "bin", "hermes"),
			filepath.Join(home, ".npm", "bin", "hermes"),
		)
	}
	for _, candidate := range candidates {
		if fileExists(candidate) {
			return candidate, nil
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
	}

	return "", fmt.Errorf("hermes: %w", ports.ErrAgentBinaryNotFound)
}

func (p *Plugin) hermesBinary(ctx context.Context) (string, error) {
	p.binaryMu.Lock()
	defer p.binaryMu.Unlock()

	if p.resolvedBinary != "" {
		return p.resolvedBinary, nil
	}

	binary, err := ResolvehermesBinary(ctx)
	if err != nil {
		return "", err
	}
	p.resolvedBinary = binary
	return binary, nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
