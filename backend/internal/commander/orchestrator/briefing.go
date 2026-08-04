package orchestrator

import (
	"fmt"
	"strings"

	"github.com/modernagent/modern-agent/backend/internal/commander"
	"github.com/modernagent/modern-agent/backend/internal/domain"
)

// generateBriefing builds the briefing string for a card at a given phase,
// optionally including redo cycle history.
func generateBriefing(card domain.WorkCard, phase commander.Phase, cycle *domain.RedoCycle) (string, error) {
	var b strings.Builder

	fmt.Fprintf(&b, "Work card: %s\n", card.ID)
	fmt.Fprintf(&b, "Goal version: %d\n", card.GoalVersion)
	fmt.Fprintf(&b, "Title: %s\n", card.Title)
	if card.TargetPath != "" {
		fmt.Fprintf(&b, "Target: %s\n", card.TargetPath)
	}
	fmt.Fprintf(&b, "Phase: %s\n", phase)

	if cycle != nil {
		fmt.Fprintf(&b, "\n--- Redo Cycle %d ---\n", cycle.CycleNumber)
		fmt.Fprintf(&b, "%s\n", cycle.Summary)
		if len(cycle.Findings) > 0 {
			fmt.Fprintf(&b, "\nFindings:\n")
			for _, f := range cycle.Findings {
				fmt.Fprintf(&b, "  [%s] %s (attempt %d)\n", f.Severity, f.Title, f.AttemptCount)
				if f.Details != "" {
					fmt.Fprintf(&b, "    Details: %s\n", f.Details)
				}
				if f.ErrorOutput != "" {
					fmt.Fprintf(&b, "    Error: %s\n", f.ErrorOutput)
				}
			}
		}
		fmt.Fprintf(&b, "\n")
	}

	switch phase {
	case commander.PhaseCoding:
		fmt.Fprintf(&b, "Coding agent: %s\n", firstNonEmpty(card.CodingAgent, card.Agent, "hermes"))
		fmt.Fprintf(&b, "\nRead the latest card state:\nao workboard get %s --json\n\n", card.ID)
		fmt.Fprintf(&b, "Then plan and implement. Before completing, record a handoff with changed files, checks/results, commit or PR, and the review focus: ao workboard card handoff %s --phase coding --summary \"...\".", card.ID)

	case commander.PhaseReview:
		fmt.Fprintf(&b, "Reviewer agent: %s\n", firstNonEmpty(card.ReviewerAgent, "hermes"))
		fmt.Fprintf(&b, "\nRead the card and PR:\nao workboard get %s --json\n\n", card.ID)
		fmt.Fprintf(&b, "The card JSON includes durable handoffs from earlier phases. Use the coding handoff to inspect the real diff and choose focused checks; if it is missing, inspect git diff, git status, and the repository test scripts first. Record your own handoff before reporting a verdict: approved | changes_requested | inconclusive.")

	case commander.PhaseTesting:
		fmt.Fprintf(&b, "Testing agent: %s\n", firstNonEmpty(card.TestingAgent, "hermes"))
		fmt.Fprintf(&b, "\nRead the card and PR:\nao workboard get %s --json\n\n", card.ID)
		fmt.Fprintf(&b, "The card JSON includes durable coding and review handoffs. Use their changed files, prior checks, and remaining focus to select tests; if either is missing, inspect git diff, git status, and the repository test scripts first. Record your own handoff, then report the result: ao workboard card set-test-result --command \"<test command>\" --exit <0 for pass, non-zero for fail> --output \"<summary>\".")
	}

	return b.String(), nil
}

// firstNonEmpty returns the first non-empty string from the given arguments,
// or an empty string if all are empty.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
