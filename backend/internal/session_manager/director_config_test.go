package sessionmanager

import (
	"testing"

	"github.com/modernagent/modern-agent/backend/internal/domain"
)

func TestEffectiveAgentConfigForDirectorUsesDirectorOverride(t *testing.T) {
	cfg := domain.ProjectConfig{
		AgentConfig:  domain.AgentConfig{Model: "openai:base", Permissions: "read"},
		Orchestrator: domain.RoleOverride{AgentConfig: domain.AgentConfig{Model: "anthropic:orchestrator"}},
		Director:     domain.RoleOverride{AgentConfig: domain.AgentConfig{Model: "openrouter:director", Permissions: "write"}},
	}

	got := effectiveAgentConfigForHarness(domain.KindOrchestrator, domain.HarnessDirector, cfg)
	if got.Model != "openrouter:director" || got.Permissions != "write" {
		t.Fatalf("Director config = %+v, want Director role override", got)
	}
}

func TestEffectiveAgentConfigForNonDirectorKeepsOrchestratorOverride(t *testing.T) {
	cfg := domain.ProjectConfig{
		AgentConfig:  domain.AgentConfig{Model: "openai:base"},
		Orchestrator: domain.RoleOverride{AgentConfig: domain.AgentConfig{Model: "anthropic:orchestrator"}},
		Director:     domain.RoleOverride{AgentConfig: domain.AgentConfig{Model: "openrouter:director"}},
	}

	got := effectiveAgentConfigForHarness(domain.KindOrchestrator, domain.HarnessHermes, cfg)
	if got.Model != "anthropic:orchestrator" {
		t.Fatalf("non-Director config model = %q, want orchestrator override", got.Model)
	}
}
