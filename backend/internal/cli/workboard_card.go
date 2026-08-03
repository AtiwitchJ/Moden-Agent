package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"

	"github.com/modernagent/modern-agent/backend/internal/domain"
)

// cardShowCmd, cardTransitionCmd, cardSetVerdictCmd, cardSetFindingCmd,
// cardSetTestResultCmd, and cardFailAttemptCmd correspond to the agent-facing
// reporting commands described in Phase 4 Task 10. Each command talks to the
// daemon via HTTP. When the daemon has not yet implemented the backing HTTP
// route the command prints a "not yet implemented" diagnostic and exits 1.

// ---------------------------------------------------------------------------
// DTOs
// ---------------------------------------------------------------------------

type cardShowResponse struct {
	ID        string               `json:"id"`
	ProjectID string               `json:"projectId"`
	Title     string               `json:"title"`
	Status    string               `json:"status"`
	Priority  string               `json:"priority"`
	Labels    []string             `json:"labels"`
	Agent     string               `json:"agent"`
	SessionID string               `json:"sessionId"`
	Handoffs  []cardHandoffPayload `json:"handoffs"`
}

type cardTransitionPayload struct {
	Status   string `json:"status"`
	Position int64  `json:"position"`
}

type cardVerdictPayload struct {
	Verdict string `json:"verdict"`
}

type cardFindingPayload struct {
	Severity string    `json:"severity"`
	Title    string    `json:"title"`
	Details  string    `json:"details"`
	Command  string    `json:"command,omitempty"`
	FileRefs []fileRef `json:"fileRefs,omitempty"`
}

type fileRef struct {
	File      string `json:"file"`
	StartLine int    `json:"startLine,omitempty"`
	EndLine   int    `json:"endLine,omitempty"`
}

type cardTestResultPayload struct {
	Command string `json:"command"`
	Exit    int    `json:"exit"`
	Output  string `json:"output"`
}

type cardFailAttemptPayload struct {
	Reason string `json:"reason"`
}

type cardHandoffPayload struct {
	Phase        string   `json:"phase"`
	Summary      string   `json:"summary"`
	ChangedFiles []string `json:"changedFiles,omitempty"`
	Checks       []string `json:"checks,omitempty"`
	Commit       string   `json:"commit,omitempty"`
	Next         string   `json:"next,omitempty"`
}

type cardEventRequest struct {
	Kind    string `json:"kind"`
	Payload string `json:"payload"`
}

// ---------------------------------------------------------------------------
// Command registration
// ---------------------------------------------------------------------------

func newWorkboardCardCmd(ctx *commandContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "card",
		Short: "Manage workboard cards (agent reporting)",
	}
	cmd.AddCommand(newCardShowCmd(ctx))
	cmd.AddCommand(newCardTransitionCmd(ctx))
	cmd.AddCommand(newCardSetVerdictCmd(ctx))
	cmd.AddCommand(newCardSetFindingCmd(ctx))
	cmd.AddCommand(newCardSetTestResultCmd(ctx))
	cmd.AddCommand(newCardHandoffCmd(ctx))
	cmd.AddCommand(newCardFailAttemptCmd(ctx))
	return cmd
}

// ---------------------------------------------------------------------------
// ao workboard card show
// ---------------------------------------------------------------------------

func newCardShowCmd(ctx *commandContext) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "show <cardId>",
		Short: "Show a workboard card",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCardShow(cmd.Context(), ctx, cmd, args[0], asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func runCardShow(ctx context.Context, c *commandContext, cmd *cobra.Command, cardID string, asJSON bool) error {
	var resp cardShowResponse
	if err := c.getJSON(ctx, "workboard/cards/"+url.PathEscape(strings.TrimSpace(cardID)), &resp); err != nil {
		if isNotFound(err) {
			return errors.New("not yet implemented: daemon route GET /api/v1/workboard/cards/{cardId} is not yet wired")
		}
		return err
	}
	if asJSON {
		return writeJSON(cmd.OutOrStdout(), resp)
	}
	_, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\nstatus: %s\nproject: %s\n", resp.Title, resp.Status, resp.ProjectID)
	return err
}

// ---------------------------------------------------------------------------
// ao workboard card transition
// ---------------------------------------------------------------------------

func newCardTransitionCmd(ctx *commandContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "transition <cardId> --to <status> --reason <text>",
		Short: "Transition a card to a new status",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCardTransition(cmd.Context(), ctx, cmd, args)
		},
	}
	cmd.Flags().String("to", "", "Target status (required)")
	cmd.Flags().String("reason", "", "Reason for transition (required)")
	cmd.MarkFlagRequired("to")
	cmd.MarkFlagRequired("reason")
	return cmd
}

func runCardTransition(ctx context.Context, c *commandContext, cmd *cobra.Command, args []string) error {
	cardID := strings.TrimSpace(args[0])
	toStatus := strings.TrimSpace(cmd.Flag("to").Value.String())
	reason := strings.TrimSpace(cmd.Flag("reason").Value.String())

	if toStatus == "" {
		return usageError{errors.New("usage: --to <status> is required")}
	}
	if reason == "" {
		return usageError{errors.New("usage: --reason <text> is required")}
	}

	newStatus := domain.CardStatus(toStatus)

	// Fetch current card so we can validate the real transition.
	var card workCardDTO
	if err := c.getJSON(ctx, "workboard/cards/"+url.PathEscape(cardID), &card); err != nil {
		if isNotFound(err) {
			return errors.New("not yet implemented: daemon route GET /api/v1/workboard/cards/{cardId} is not yet wired")
		}
		return err
	}
	if err := domain.ValidateWorkflowTransition(domain.CardStatus(card.Status), newStatus, "agent"); err != nil {
		return usageError{err}
	}

	// Write the agent_transition event.
	eventReq := cardEventRequest{
		Kind: "agent_transition",
		Payload: mustMarshal(cardTransitionPayload{
			Status:   toStatus,
			Position: 0,
		}),
	}
	var eventResp map[string]interface{}
	if err := c.postJSON(ctx, "workboard/cards/"+url.PathEscape(cardID)+"/events", eventReq, &eventResp); err != nil {
		if isNotImplemented(err) {
			return errors.New("not yet implemented: daemon route POST /api/v1/workboard/cards/{cardId}/events is not yet wired")
		}
		return err
	}
	_, err := fmt.Fprintf(cmd.OutOrStdout(), "card %s transitioned to %s\n", cardID, toStatus)
	return err
}

// ---------------------------------------------------------------------------
// ao workboard card set-verdict
// ---------------------------------------------------------------------------

var validVerdicts = map[string]bool{
	"approved":          true,
	"changes_requested": true,
	"inconclusive":      true,
}

func newCardSetVerdictCmd(ctx *commandContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set-verdict <cardId> --verdict <approved|changes_requested|inconclusive>",
		Short: "Record an agent verdict on a card",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCardSetVerdict(cmd.Context(), ctx, cmd, args[0])
		},
	}
	cmd.Flags().String("verdict", "", "Verdict value (required)")
	cmd.MarkFlagRequired("verdict")
	return cmd
}

func runCardSetVerdict(ctx context.Context, c *commandContext, cmd *cobra.Command, cardID string) error {
	verdict := strings.TrimSpace(cmd.Flag("verdict").Value.String())

	if verdict == "" {
		return usageError{errors.New("usage: --verdict <approved|changes_requested|inconclusive> is required")}
	}
	if !validVerdicts[verdict] {
		return usageError{fmt.Errorf("usage: invalid verdict %q; must be one of: approved, changes_requested, inconclusive", verdict)}
	}

	eventReq := cardEventRequest{
		Kind:    "agent_verdict",
		Payload: mustMarshal(cardVerdictPayload{Verdict: verdict}),
	}
	var eventResp map[string]interface{}
	if err := c.postJSON(ctx, "workboard/cards/"+url.PathEscape(strings.TrimSpace(cardID))+"/events", eventReq, &eventResp); err != nil {
		if isNotImplemented(err) {
			return errors.New("not yet implemented: daemon route POST /api/v1/workboard/cards/{cardId}/events is not yet wired")
		}
		return err
	}
	_, err := fmt.Fprintf(cmd.OutOrStdout(), "verdict %s recorded for card %s\n", verdict, cardID)
	return err
}

// ---------------------------------------------------------------------------
// ao workboard card set-finding
// ---------------------------------------------------------------------------

var validSeverities = map[string]bool{
	"critical": true,
	"high":     true,
	"normal":   true,
	"low":      true,
}

func newCardSetFindingCmd(ctx *commandContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set-finding <cardId> --severity <critical|high|normal|low> --title <t> --details <d> [--command <c>] [--file <path:line>]",
		Short: "Record a finding on a card",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCardSetFinding(cmd.Context(), ctx, cmd, args[0])
		},
	}
	cmd.Flags().String("severity", "", "Finding severity (required)")
	cmd.Flags().String("title", "", "Finding title (required)")
	cmd.Flags().String("details", "", "Finding details (required)")
	cmd.Flags().String("command", "", "Command that produced the finding")
	cmd.Flags().String("file", "", "File reference in path:line form")
	cmd.MarkFlagRequired("severity")
	cmd.MarkFlagRequired("title")
	cmd.MarkFlagRequired("details")
	return cmd
}

func runCardSetFinding(ctx context.Context, c *commandContext, cmd *cobra.Command, cardID string) error {
	severity := strings.TrimSpace(cmd.Flag("severity").Value.String())
	title := strings.TrimSpace(cmd.Flag("title").Value.String())
	details := strings.TrimSpace(cmd.Flag("details").Value.String())
	findingCmd := strings.TrimSpace(cmd.Flag("command").Value.String())
	fileRefStr := strings.TrimSpace(cmd.Flag("file").Value.String())

	if severity == "" {
		return usageError{errors.New("usage: --severity <critical|high|normal|low> is required")}
	}
	if !validSeverities[severity] {
		return usageError{fmt.Errorf("usage: invalid severity %q; must be one of: critical, high, normal, low", severity)}
	}
	if title == "" {
		return usageError{errors.New("usage: --title <text> is required")}
	}
	if details == "" {
		return usageError{errors.New("usage: --details <text> is required")}
	}

	var fileRefs []fileRef
	if fileRefStr != "" {
		parts := strings.Split(fileRefStr, ":")
		ref := fileRef{File: parts[0]}
		if len(parts) > 1 {
			ref.StartLine = atoi(parts[1])
		}
		if len(parts) > 2 {
			ref.EndLine = atoi(parts[2])
		} else if ref.StartLine > 0 {
			ref.EndLine = ref.StartLine
		}
		fileRefs = []fileRef{ref}
	}

	eventReq := cardEventRequest{
		Kind: "agent_finding",
		Payload: mustMarshal(cardFindingPayload{
			Severity: severity,
			Title:    title,
			Details:  details,
			Command:  findingCmd,
			FileRefs: fileRefs,
		}),
	}
	var eventResp map[string]interface{}
	if err := c.postJSON(ctx, "workboard/cards/"+url.PathEscape(strings.TrimSpace(cardID))+"/events", eventReq, &eventResp); err != nil {
		if isNotImplemented(err) {
			return errors.New("not yet implemented: daemon route POST /api/v1/workboard/cards/{cardId}/events is not yet wired")
		}
		return err
	}
	_, err := fmt.Fprintf(cmd.OutOrStdout(), "finding recorded for card %s\n", cardID)
	return err
}

// ---------------------------------------------------------------------------
// ao workboard card set-test-result
// ---------------------------------------------------------------------------

func newCardSetTestResultCmd(ctx *commandContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set-test-result <cardId> --command <c> --exit <n> --output <text>",
		Short: "Record a test result on a card",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCardSetTestResult(cmd.Context(), ctx, cmd, args[0])
		},
	}
	cmd.Flags().String("command", "", "Test command that was run (required)")
	cmd.Flags().Int("exit", 0, "Exit code of the test command (required)")
	cmd.Flags().String("output", "", "Output of the test command")
	cmd.MarkFlagRequired("command")
	cmd.MarkFlagRequired("exit")
	return cmd
}

func runCardSetTestResult(ctx context.Context, c *commandContext, cmd *cobra.Command, cardID string) error {
	testCmd := strings.TrimSpace(cmd.Flag("command").Value.String())
	exitCode, _ := cmd.Flags().GetInt("exit")
	output := strings.TrimSpace(cmd.Flag("output").Value.String())

	if testCmd == "" {
		return usageError{errors.New("usage: --command <text> is required")}
	}

	eventReq := cardEventRequest{
		Kind: "test_result",
		Payload: mustMarshal(cardTestResultPayload{
			Command: testCmd,
			Exit:    exitCode,
			Output:  output,
		}),
	}
	var eventResp map[string]interface{}
	if err := c.postJSON(ctx, "workboard/cards/"+url.PathEscape(strings.TrimSpace(cardID))+"/events", eventReq, &eventResp); err != nil {
		if isNotImplemented(err) {
			return errors.New("not yet implemented: daemon route POST /api/v1/workboard/cards/{cardId}/events is not yet wired")
		}
		return err
	}
	_, err := fmt.Fprintf(cmd.OutOrStdout(), "test result recorded for card %s\n", cardID)
	return err
}

// ---------------------------------------------------------------------------
// ao workboard card handoff
// ---------------------------------------------------------------------------

func newCardHandoffCmd(ctx *commandContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "handoff <cardId> --phase <coding|review|testing> --summary <text> [--changed <path>] [--check <result>] [--commit <sha>] [--next <text>]",
		Short: "Record context for the next work-card phase",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCardHandoff(cmd.Context(), ctx, cmd, args[0])
		},
	}
	cmd.Flags().String("phase", "", "Phase that completed: coding, review, or testing (required)")
	cmd.Flags().String("summary", "", "What was done or found (required)")
	cmd.Flags().StringSlice("changed", nil, "Changed file (repeatable)")
	cmd.Flags().StringSlice("check", nil, "Check command and result (repeatable)")
	cmd.Flags().String("commit", "", "Commit or PR reference")
	cmd.Flags().String("next", "", "What the next agent must verify")
	cmd.MarkFlagRequired("phase")
	cmd.MarkFlagRequired("summary")
	return cmd
}

func runCardHandoff(ctx context.Context, c *commandContext, cmd *cobra.Command, cardID string) error {
	phase := strings.TrimSpace(cmd.Flag("phase").Value.String())
	summary := strings.TrimSpace(cmd.Flag("summary").Value.String())
	if phase != "coding" && phase != "review" && phase != "testing" {
		return usageError{fmt.Errorf("usage: invalid phase %q; must be coding, review, or testing", phase)}
	}
	if summary == "" {
		return usageError{errors.New("usage: --summary <text> is required")}
	}
	changed, _ := cmd.Flags().GetStringSlice("changed")
	checks, _ := cmd.Flags().GetStringSlice("check")
	eventReq := cardEventRequest{Kind: "agent_handoff", Payload: mustMarshal(cardHandoffPayload{
		Phase: phase, Summary: summary, ChangedFiles: changed, Checks: checks,
		Commit: strings.TrimSpace(cmd.Flag("commit").Value.String()),
		Next:   strings.TrimSpace(cmd.Flag("next").Value.String()),
	})}
	var eventResp map[string]interface{}
	if err := c.postJSON(ctx, "workboard/cards/"+url.PathEscape(strings.TrimSpace(cardID))+"/events", eventReq, &eventResp); err != nil {
		return err
	}
	_, err := fmt.Fprintf(cmd.OutOrStdout(), "handoff recorded for card %s\n", cardID)
	return err
}

// ---------------------------------------------------------------------------
// ao workboard card fail-attempt
// ---------------------------------------------------------------------------

var validFailReasons = map[string]bool{
	"timeout":      true,
	"error":        true,
	"inconclusive": true,
	"spawn_failed": true,
}

func newCardFailAttemptCmd(ctx *commandContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fail-attempt <cardId> --reason <timeout|error|inconclusive|spawn_failed>",
		Short: "Report an agent attempt failure",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCardFailAttempt(cmd.Context(), ctx, cmd, args[0])
		},
	}
	cmd.Flags().String("reason", "", "Failure reason (required)")
	cmd.MarkFlagRequired("reason")
	return cmd
}

func runCardFailAttempt(ctx context.Context, c *commandContext, cmd *cobra.Command, cardID string) error {
	reason := strings.TrimSpace(cmd.Flag("reason").Value.String())

	if reason == "" {
		return usageError{errors.New("usage: --reason <timeout|error|inconclusive|spawn_failed> is required")}
	}
	if !validFailReasons[reason] {
		return usageError{fmt.Errorf("usage: invalid reason %q; must be one of: timeout, error, inconclusive, spawn_failed", reason)}
	}

	eventReq := cardEventRequest{
		Kind:    "agent_failed",
		Payload: mustMarshal(cardFailAttemptPayload{Reason: reason}),
	}
	var eventResp map[string]interface{}
	if err := c.postJSON(ctx, "workboard/cards/"+url.PathEscape(strings.TrimSpace(cardID))+"/events", eventReq, &eventResp); err != nil {
		if isNotImplemented(err) {
			return errors.New("not yet implemented: daemon route POST /api/v1/workboard/cards/{cardId}/events is not yet wired")
		}
		return err
	}
	_, err := fmt.Fprintf(cmd.OutOrStdout(), "attempt failure recorded for card %s\n", cardID)
	return err
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func mustMarshal(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// atoi parses a decimal integer string, returning 0 on error.
func atoi(s string) int {
	var n int
	fmt.Sscanf(s, "%d", &n)
	return n
}

// isNotFound reports whether err indicates a 404 from the daemon.
func isNotFound(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "Not Found") || strings.Contains(err.Error(), "not found"))
}

// isNotImplemented reports whether err indicates a 501 from the daemon.
func isNotImplemented(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "501") || strings.Contains(err.Error(), "Not Implemented") || strings.Contains(err.Error(), "not yet implemented"))
}
