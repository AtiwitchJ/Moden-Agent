package cli

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
)

type workCardDTO struct {
	ID                 string   `json:"id"`
	ProjectID          string   `json:"projectId"`
	Title              string   `json:"title"`
	Notes              string   `json:"notes"`
	Priority           string   `json:"priority"`
	Labels             []string `json:"labels"`
	Status             string   `json:"status"`
	Position           int64    `json:"position"`
	TargetPath         string   `json:"targetPath"`
	Agent              string   `json:"agent"`
	CodingAgent        string   `json:"codingAgent"`
	ReviewerMode       string   `json:"reviewerMode"`
	ReviewerAgent      string   `json:"reviewerAgent"`
	TestingAgent       string   `json:"testingAgent"`
	SessionID          string   `json:"sessionId"`
	WaitingForInput    bool     `json:"waitingForInput"`
	PausedRetarget     bool     `json:"pausedRetarget"`
	GoalVersion        int      `json:"goalVersion"`
	SupersededByCardID string   `json:"supersededByCardId"`
}

type workCardStatusRequest struct {
	Status string `json:"status"`
}

func newWorkboardCommand(ctx *commandContext) *cobra.Command {
	cmd := &cobra.Command{Use: "workboard", Short: "Inspect and update work cards"}
	cmd.AddCommand(newWorkboardGetCommand(ctx))
	cmd.AddCommand(newWorkboardStatusCommand(ctx))
	return cmd
}

func newWorkboardGetCommand(ctx *commandContext) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "get <card-id>",
		Short: "Fetch the current work card",
		Args:  oneWorkCardIDArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			return ctx.getWorkCard(cmd.Context(), cmd, args[0], asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func newWorkboardStatusCommand(ctx *commandContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status <card-id> <status>",
		Short: "Set a work card status",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 2 {
				return usageError{errors.New("usage: card id and status are required")}
			}
			if strings.TrimSpace(args[0]) == "" {
				return usageError{errors.New("usage: card id is required")}
			}
			if !validWorkCardStatus(args[1]) {
				return usageError{fmt.Errorf("usage: invalid work card status %q", args[1])}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return ctx.setWorkCardStatus(cmd.Context(), cmd, args[0], args[1])
		},
	}
	return cmd
}

func oneWorkCardIDArg(_ *cobra.Command, args []string) error {
	if len(args) != 1 || strings.TrimSpace(args[0]) == "" {
		return usageError{errors.New("usage: card id is required")}
	}
	return nil
}

func (c *commandContext) getWorkCard(ctx context.Context, cmd *cobra.Command, id string, asJSON bool) error {
	var card workCardDTO
	if err := c.getJSON(ctx, "workboard/cards/"+url.PathEscape(strings.TrimSpace(id)), &card); err != nil {
		return err
	}
	if asJSON {
		return writeJSON(cmd.OutOrStdout(), card)
	}
	_, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\nstatus: %s\nproject: %s\n", card.Title, card.Status, card.ProjectID)
	return err
}

func (c *commandContext) setWorkCardStatus(ctx context.Context, cmd *cobra.Command, id, status string) error {
	var card workCardDTO
	if err := c.patchJSON(ctx, "workboard/cards/"+url.PathEscape(strings.TrimSpace(id)), workCardStatusRequest{Status: status}, &card); err != nil {
		return err
	}
	_, err := fmt.Fprintf(cmd.OutOrStdout(), "work card %s moved to %s\n", card.ID, card.Status)
	return err
}

func validWorkCardStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case "triage", "backlog", "todo", "scheduled", "ready", "running", "review", "testing", "redo", "blocked", "done":
		return true
	default:
		return false
	}
}
