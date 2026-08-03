package orchestrator

import (
	"github.com/modernagent/modern-agent/backend/internal/commander"
	"github.com/modernagent/modern-agent/backend/internal/domain"
)

// pickNextAgent returns the next agent in the chain after currentAgent.
// If currentAgent is "" (first attempt), returns the first agent.
// If currentAgent is the last in the chain (including Hermes), returns ""
// to signal the phase is exhausted.
func (o *ConfiguredOrchestrator) pickNextAgent(card domain.WorkCard, phase commander.Phase, currentAgent string) string {
	switch phase {
	case commander.PhaseCoding:
		chain := codingAgentChain(card)
		return pickFromChain(chain, currentAgent)
	case commander.PhaseReview:
		chain := reviewerAgentChain(card)
		return pickFromChain(chain, currentAgent)
	case commander.PhaseTesting:
		chain := testingAgentChain(card)
		return pickFromChain(chain, currentAgent)
	}
	return ""
}

// codingAgentChain returns the ordered list of coding agents for a card,
// with a fallback to "hermes" as the final entry.
func codingAgentChain(card domain.WorkCard) []string {
	var chain []string
	if card.CodingAgent != "" {
		chain = append(chain, card.CodingAgent)
	}
	if card.Agent != "" {
		chain = append(chain, card.Agent)
	}
	chain = append(chain, "hermes")
	return chain
}

// reviewerAgentChain returns the ordered list of reviewer agents for a card.
func reviewerAgentChain(card domain.WorkCard) []string {
	var chain []string
	if card.ReviewerAgent != "" {
		chain = append(chain, card.ReviewerAgent)
	}
	chain = append(chain, "hermes")
	return chain
}

// testingAgentChain returns the ordered list of testing agents for a card.
func testingAgentChain(card domain.WorkCard) []string {
	var chain []string
	if card.TestingAgent != "" {
		chain = append(chain, card.TestingAgent)
	}
	chain = append(chain, "hermes")
	return chain
}

// pickFromChain returns the agent after currentAgent in chain.
// Returns "" if currentAgent is the last entry in the chain.
func pickFromChain(chain []string, currentAgent string) string {
	if currentAgent == "" {
		return chain[0]
	}
	for i, agent := range chain {
		if agent == currentAgent {
			if i+1 < len(chain) {
				return chain[i+1]
			}
			return ""
		}
	}
	return ""
}

// isLastAgent returns true if agent is the final entry in the chain for phase.
func (o *ConfiguredOrchestrator) isLastAgent(card domain.WorkCard, phase commander.Phase, agent string) bool {
	var chain []string
	switch phase {
	case commander.PhaseCoding:
		chain = codingAgentChain(card)
	case commander.PhaseReview:
		chain = reviewerAgentChain(card)
	case commander.PhaseTesting:
		chain = testingAgentChain(card)
	}
	if len(chain) == 0 {
		return true
	}
	return chain[len(chain)-1] == agent
}
