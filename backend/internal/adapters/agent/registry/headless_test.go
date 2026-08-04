package registry

import (
	"testing"

	"github.com/modernagent/modern-agent/backend/internal/ports"
)

// noHeadlessHarnesses lists every registered harness that deliberately has no
// one-shot command, with the reason. A harness here cannot be spawned with
// `ao spawn --oneshot` and cannot be used for a Director-delegated card phase.
//
// This list is the contract: adding a harness to the registry without either
// implementing ports.AgentHeadless or naming it here fails the test below, so
// a new adapter cannot silently become un-delegatable.
var noHeadlessHarnesses = map[string]string{
	"command":  "runs a user-supplied command; one-shot semantics are the caller's, not ours",
	"director": "AO's own Director loop, which drives cards rather than executing one",

	// TUI-only launches with no confirmed self-terminating mode. Out of scope
	// deliberately: a half-working headless command would hang a Director
	// waiting on a process that never exits, which is worse than this honest
	// rejection. Revisit one only with its binary installed and its --help
	// showing a non-interactive mode.
	"copilot":  "interactive TUI; the adapter delivers its prompt after start",
	"droid":    "interactive TUI launch",
	"amp":      "interactive TUI launch",
	"crush":    "interactive TUI launch",
	"kilocode": "interactive TUI launch",
	"autohand": "interactive TUI launch",
	"openclaw": "interactive TUI launch",

	// The adapter's own launch command already looks headless per its doc
	// comment, but the binary is not installed on this machine, so the claim
	// is unverified per the plan's "never guess a flag" rule. Implement once
	// the binary is available to run --help against.
	"grok":     "unverified: binary not installed",
	"qwen":     "unverified: binary not installed",
	"kimi":     "unverified: binary not installed",
	"aider":    "unverified: binary not installed",
	"goose":    "unverified: binary not installed",
	"auggie":   "unverified: binary not installed",
	"continue": "unverified: binary not installed",
	"devin":    "unverified: binary not installed",
	"cline":    "unverified: binary not installed",
	"kiro":     "unverified: binary not installed",
	"vibe":     "unverified: binary not installed",
	"pi":       "unverified: binary not installed",
}

func TestEveryHarnessIsHeadlessOrDocumented(t *testing.T) {
	for _, a := range Constructors(t.TempDir()) {
		id := a.Manifest().ID
		agent, ok := a.(ports.Agent)
		if !ok {
			continue
		}
		_, headless := agent.(ports.AgentHeadless)
		reason, excluded := noHeadlessHarnesses[id]

		switch {
		case headless && excluded:
			t.Errorf("harness %q implements AgentHeadless but is still listed as excluded (%q); remove it from noHeadlessHarnesses", id, reason)
		case !headless && !excluded:
			t.Errorf("harness %q has no headless command and no documented reason; implement ports.AgentHeadless or add it to noHeadlessHarnesses", id)
		}
	}
}
