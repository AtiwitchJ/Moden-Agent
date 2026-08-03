// Package director implements AO's first-party Director agent harness. Unlike
// the other adapters, which wrap an externally installed CLI, the Director is a
// DeepAgents loop AO ships itself (see internal/directorassets) — which is what
// makes agentConfig.model a real engine selector rather than an advisory hint.
package director

import (
	"context"
	"errors"

	"github.com/modernagent/modern-agent/backend/internal/adapters"
	"github.com/modernagent/modern-agent/backend/internal/directorassets"
	"github.com/modernagent/modern-agent/backend/internal/ports"
)

const adapterID = "director"

// Plugin is the Director harness adapter.
type Plugin struct {
	dataDir string
}

// Option configures a Plugin.
type Option func(*Plugin)

// WithDataDir sets the AO data dir the installed Director bundle lives under.
func WithDataDir(dir string) Option {
	return func(p *Plugin) { p.dataDir = dir }
}

// New returns a ready-to-register Director adapter.
func New(opts ...Option) *Plugin {
	p := &Plugin{}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

var _ adapters.Adapter = (*Plugin)(nil)
var _ ports.Agent = (*Plugin)(nil)
var _ ports.AgentAuthChecker = (*Plugin)(nil)

// Manifest returns the adapter's static self-description.
func (p *Plugin) Manifest() adapters.Manifest {
	return adapters.Manifest{
		ID:          adapterID,
		Name:        "Director",
		Description: "Drive work cards with AO's first-party Director agent on a configurable engine.",
		Version:     "0.0.1",
		Capabilities: []adapters.Capability{
			adapters.CapabilityAgent,
		},
	}
}

// GetConfigSpec reports the engine field exposed in project config.
func (p *Plugin) GetConfigSpec(ctx context.Context) (ports.ConfigSpec, error) {
	if err := ctx.Err(); err != nil {
		return ports.ConfigSpec{}, err
	}
	return ports.ConfigSpec{
		Fields: []ports.ConfigField{
			{
				Key:         "model",
				Type:        ports.ConfigFieldString,
				Description: `Engine as "provider:model-name" (e.g. openai:gpt-5, openrouter:minimax/minimax-m2, ollama:qwen2.5).`,
				Required:    false,
			},
		},
	}, nil
}

// GetLaunchCommand runs the installed Director bundle with node.
func (p *Plugin) GetLaunchCommand(ctx context.Context, cfg ports.LaunchConfig) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if p.dataDir == "" {
		return nil, errors.New("director harness requires the AO data dir to locate its bundle")
	}
	return []string{"node", directorassets.EntrypointPath(p.dataDir)}, nil
}

// GetPromptDeliveryStrategy reports that prompts arrive via env vars.
func (p *Plugin) GetPromptDeliveryStrategy(ctx context.Context, _ ports.LaunchConfig) (ports.PromptDeliveryStrategy, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return ports.PromptDeliveryAfterStart, nil
}

// GetAgentHooks is a no-op: the Director reports activity through `ao hooks`.
func (p *Plugin) GetAgentHooks(context.Context, ports.WorkspaceHookConfig) error {
	return nil
}

// GetRestoreCommand reports that Director sessions are not natively resumable
// yet: the loop rebuilds its context from the card on each run, so a fresh
// launch is equivalent to a resume.
func (p *Plugin) GetRestoreCommand(ctx context.Context, _ ports.RestoreConfig) ([]string, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	return nil, false, nil
}

// SessionInfo reports no agent-owned metadata.
func (p *Plugin) SessionInfo(ctx context.Context, _ ports.SessionRef) (ports.SessionInfo, bool, error) {
	if err := ctx.Err(); err != nil {
		return ports.SessionInfo{}, false, err
	}
	return ports.SessionInfo{}, false, nil
}

// AuthStatus reports authorized: the Director's provider key is validated by the
// agent itself at startup, where the configured provider is known.
func (p *Plugin) AuthStatus(ctx context.Context) (ports.AgentAuthStatus, error) {
	if err := ctx.Err(); err != nil {
		return ports.AgentAuthStatusUnknown, err
	}
	return ports.AgentAuthStatusAuthorized, nil
}
