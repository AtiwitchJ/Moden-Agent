# Trim Hermes Card Briefing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: use `superpowers:executing-plans` to implement this plan task-by-task (single task, no need for subagent-driven-development). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stop resending the full card JSON + the entire automation-policy paragraph to the Hermes commander on every card dispatch/re-dispatch; send only the minimum needed to identify and start the card, and point Hermes at `ao workboard get` for everything else.

**Architecture:** `hermesCardBriefing` (`backend/internal/service/workboard/dispatch.go:375`) currently builds a JSON blob of the full card plus a hardcoded paragraph repeating the automation policy. That policy is already permanently present in every Hermes commander's system prompt via `hermesWorkboardPrompt()` (`backend/internal/session_manager/manager.go:1465`), wired in at session spawn time (`manager.go:1319-1320`) whenever the project's orchestrator harness is Hermes. `SpawnOrchestrator` (`backend/internal/service/session/service.go:304`) sends this same briefing string both when spawning a brand-new Hermes session (`service.go:340`, as the initial task prompt) and when reusing an already-running one for a later card (`service.go:332-336`, via `s.Send`) — so today, the full JSON + full policy paragraph repeats on every single card, forever, even though the policy never changes and the CLI (`ao workboard get <card-id> --json`, already implemented at `backend/internal/cli/workboard.go:46`) can fetch the live card at any time. This plan replaces the JSON+policy blob with a short plain-text pointer: card id, goal version, title, target path, coding/review/testing agent (only when set), and an instruction to fetch the rest via `ao workboard get`.

**Tech Stack:** Go (backend), no frontend or API-contract changes — `hermesCardBriefing` is an internal string builder, its output is opaque prompt text to the session/CLI layer.

## Global Constraints

- No behavior change to the non-Hermes worker dispatch path (`dispatch.go:244-250`, `card.Title + "\n\n" + card.Notes`) — out of scope, already minimal.
- No change to `hermesWorkboardPrompt()` itself (`manager.go:1465`) — it already carries the full policy once per session; this plan only stops repeating it per-card.
- Keep `firstNonEmpty(card.CodingAgent, card.Agent)` for the coding-agent fallback — this is the existing convention (`dispatch.go:392`), don't invent a new one.
- Do not invent a "same as coding agent" fallback for `ReviewerAgent`/`TestingAgent` that doesn't already exist elsewhere in the codebase (checked: none does, `internal/service/workboard/service.go:298` stores `TestingAgent` as-is, empty allowed). Omit the Review/Testing line entirely when the field is empty instead of guessing a default — Hermes reads the authoritative `reviewerMode`/`reviewerAgent`/`testingAgent` from `ao workboard get` anyway.

---

### Task 1: Replace the JSON+policy briefing with a trimmed plain-text pointer

**Files:**
- Modify: `backend/internal/service/workboard/dispatch.go:371-401` (`hermesCardBriefing`)
- Modify: `backend/internal/service/workboard/dispatch_test.go:222-247` (`TestDispatchOnce_HermesProjectBriefsCommanderInsteadOfSpawningWorker`)
- Modify: `backend/internal/service/workboard/dispatch_test.go:713-721` (`extractCardIDFromBriefing` test helper — currently parses the JSON `"cardId":"..."` substring, must parse the new plain-text line instead)

**Interfaces:**
- Consumes: `domain.WorkCard` fields `ID`, `GoalVersion`, `Title`, `TargetPath`, `CodingAgent`, `Agent`, `ReviewerAgent`, `TestingAgent` (all already exist on `domain.WorkCard`, `backend/internal/domain/workboard.go:127-139`). `firstNonEmpty(values ...string) string` (`backend/internal/service/workboard/switch_agent.go:261`, already exists, no change).
- Produces: `hermesCardBriefing(card domain.WorkCard) (string, error)` — same signature, same two call sites (`dispatch.go:233`, feeding `orchestrator.SpawnOrchestrator(ctx, ..., briefing)` at `dispatch.go:237`) — no caller changes needed since the signature is unchanged.

- [ ] **Step 1: Write the failing test — assert the new trimmed format, not the old JSON+policy blob**

Replace the assertion block in `TestDispatchOnce_HermesProjectBriefsCommanderInsteadOfSpawningWorker` (`dispatch_test.go:244-246`):

```go
	prompt := spawner.orchestratorPrompts
	if len(prompt) != 1 {
		t.Fatalf("orchestrator prompts = %v, want exactly 1", prompt)
	}
	got := prompt[0]
	wantLines := []string{
		"New work card: card",
		"Goal version: 0",
		"Title: card title",
		"Coding: claude-code",
		"ao workboard get card --json",
	}
	for _, want := range wantLines {
		if !strings.Contains(got, want) {
			t.Fatalf("briefing = %q, want it to contain %q", got, want)
		}
	}
	// The full card JSON and the repeated automation-policy paragraph must be
	// gone — that policy already lives once in the Hermes system prompt
	// (hermesWorkboardPrompt), repeating it per card was pure waste.
	if strings.Contains(got, `"cardId"`) {
		t.Fatalf("briefing = %q, want no JSON blob (policy now lives only in the system prompt)", got)
	}
	if strings.Contains(got, "command the selected reviewer and testing agents") {
		t.Fatalf("briefing = %q, want no repeated automation-policy paragraph", got)
	}
	// card has no TargetPath/ReviewerAgent/TestingAgent set in this fixture,
	// so those optional lines must not appear at all.
	if strings.Contains(got, "Target:") || strings.Contains(got, "Review:") || strings.Contains(got, "Testing:") {
		t.Fatalf("briefing = %q, want optional lines omitted when unset", got)
	}
```

Also update `extractCardIDFromBriefing` (`dispatch_test.go:713-721`) to parse the new line instead of the old JSON key:

```go
func extractCardIDFromBriefing(prompt string) string {
	const marker = "New work card: "
	idx := strings.Index(prompt, marker)
	if idx == -1 {
		return ""
	}
	rest := prompt[idx+len(marker):]
	if end := strings.IndexByte(rest, '\n'); end != -1 {
		return strings.TrimSpace(rest[:end])
	}
	return strings.TrimSpace(rest)
}
```

- [ ] **Step 2: Run the test suite to verify it fails**

Run: `cd backend && go test ./internal/service/workboard/... -run TestDispatchOnce_HermesProjectBriefsCommanderInsteadOfSpawningWorker -v`
Expected: FAIL — the old implementation still emits the JSON blob and the policy paragraph, so the new `wantLines`/must-not-contain assertions fail against it.

- [ ] **Step 3: Rewrite `hermesCardBriefing` to emit the trimmed format**

Replace `hermesCardBriefing` (`dispatch.go:371-401`) with:

```go
// hermesCardBriefing is intentionally short: Hermes's system prompt
// (hermesWorkboardPrompt, session_manager/manager.go) already carries the
// full automation policy once per session, and `ao workboard get` returns
// the live card on demand. Repeating the full card JSON and the policy
// paragraph on every dispatch — including every later card sent to an
// already-running commander via SpawnOrchestrator's reuse path — was pure
// waste. This keeps only what Hermes needs before it can even run that
// command: which card, which goal version, and enough identity to act if
// the read fails.
func hermesCardBriefing(card domain.WorkCard) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "New work card: %s\n", card.ID)
	fmt.Fprintf(&b, "Goal version: %d\n", card.GoalVersion)
	fmt.Fprintf(&b, "Title: %s\n", card.Title)
	if card.TargetPath != "" {
		fmt.Fprintf(&b, "Target: %s\n", card.TargetPath)
	}
	fmt.Fprintf(&b, "Coding: %s\n", firstNonEmpty(card.CodingAgent, card.Agent))
	if card.ReviewerAgent != "" {
		fmt.Fprintf(&b, "Review: %s\n", card.ReviewerAgent)
	}
	if card.TestingAgent != "" {
		fmt.Fprintf(&b, "Testing: %s\n", card.TestingAgent)
	}
	fmt.Fprintf(&b, "\nRead the latest source of truth first:\nao workboard get %s --json\n\nThen plan, delegate, and drive the card through its configured workflow.", card.ID)
	return b.String(), nil
}
```

`dispatch.go`'s import block (top of file) does **not** currently import `"strings"` — add it. `encoding/json` stays: `recoverCardFromSpawnFailure` elsewhere in the file still marshals the `dispatch_failed` event payload, so don't remove that import even though `hermesCardBriefing` itself no longer calls `json.Marshal`.

```go
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/modernagent/modern-agent/backend/internal/domain"
	"github.com/modernagent/modern-agent/backend/internal/ports"
	sessionsvc "github.com/modernagent/modern-agent/backend/internal/service/session"
)
```

The function keeps returning `(string, error)` with `error` always nil now (there is no more `json.Marshal` that can fail) — **do not** change the signature; `dispatch.go:233-236` already does `briefing, briefErr := hermesCardBriefing(card); if briefErr != nil { return claimed, briefErr }` and changing the signature would ripple into that caller for no benefit. Keeping `error` in the signature costs nothing and avoids an unrelated caller-side diff.

- [ ] **Step 4: Run the test suite to verify it passes**

Run: `cd backend && go test ./internal/service/workboard/... -v`
Expected: PASS — including every other existing test in the package (`TestDispatchOnce_HermesUnavailableReleasesCardWithoutWorker` and the rest), since none of them assert on the old JSON shape except the one test updated in Step 1.

- [ ] **Step 5: Full backend verification**

Run:
```bash
cd backend
go build ./...
go vet ./...
go test ./internal/service/workboard/... ./internal/session_manager/... ./internal/service/session/...
```
Expected: all green. `session_manager`/`service/session` are included because they own `hermesWorkboardPrompt`/`SpawnOrchestrator`, which this task relies on but does not modify — this confirms nothing there silently depended on the old briefing shape.

- [ ] **Step 6: Commit**

```bash
cd /Users/up-mac/wokrspace/mind/moden-agent
git add backend/internal/service/workboard/dispatch.go backend/internal/service/workboard/dispatch_test.go
git commit -m "$(cat <<'EOF'
fix(workboard): trim Hermes card briefing to essentials

The full card JSON and the automation-policy paragraph were being resent on
every dispatch, including every later card sent to an already-running Hermes
commander. The policy already lives once in the commander's system prompt
(hermesWorkboardPrompt); the card JSON is one `ao workboard get` away. Send
only card id, goal version, title, target, and configured agents instead.
EOF
)"
```

---

## Out of scope

- The non-Hermes direct-worker prompt (`dispatch.go:244-250`) — already minimal (`title + notes`), not touched.
- `hermesWorkboardPrompt()` itself (`manager.go:1465`) — the standing policy text is unchanged; this plan only stops repeating it per card.
- Any change to `ao workboard get`/`ao workboard status` CLI — both already exist and already do what the new briefing tells Hermes to rely on.
- Frontend / API contract — `hermesCardBriefing`'s output is internal prompt text, never serialized through the OpenAPI surface.

## Verification plan

```bash
cd backend
go test ./internal/service/workboard/... -run TestDispatchOnce_HermesProjectBriefsCommanderInsteadOfSpawningWorker -v
go build ./...
go vet ./...
go test ./internal/service/workboard/... ./internal/session_manager/... ./internal/service/session/...
```

Manual sanity check (optional, requires a running daemon + a Hermes-harness project): create two cards in the same Hermes project, dispatch both, and read back the two prompts sent to the commander session (`session_manager` activity log or `ao session activity <id>`) — the second card's prompt should be a few short lines, not the full JSON blob, and should not repeat the automation-policy paragraph.
