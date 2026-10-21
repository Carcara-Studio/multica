package main

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
)

func init() { issueCmd.AddCommand(newIssueDelegateCommand()) }

func newIssueDelegateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delegate <parent-id-or-key>",
		Short: "Delegate work to a direct Project report using a task token",
		Long:  "Delegate one child Issue through the hierarchy admission endpoint. Requires a task-scoped mat_ token and server authorization. Persist --request-id before calling; reuse that ID and unchanged intent after an ambiguous failure or in a later task turn. Use a new ID only for deliberately new work. This command never retries or falls back to ordinary Issue creation or assignment.",
		Args:  cobra.ExactArgs(1),
		RunE:  runIssueDelegate,
	}
	f := cmd.Flags()
	f.String("agent", "", "Direct report Agent UUID (not a display name)")
	f.String("request-id", "", "Persisted UUID identifying this delegation intent")
	f.String("title", "", "Child Issue title")
	f.String("priority", "none", "Child Issue priority")
	f.String("description", "", "Child Issue description")
	f.String("description-file", "", "Read description from a file within the working directory")
	f.Bool("description-stdin", false, "Read description from stdin")
	f.Bool("allow-external-file", false, "Allow description file outside the working directory")
	f.String("output", "json", "Output format (json)")
	return cmd
}

func delegationUUID(value string) bool {
	return uuidRegexp.MatchString(value) && value != "00000000-0000-0000-0000-000000000000"
}

func runIssueDelegate(cmd *cobra.Command, args []string) error {
	agent, _ := cmd.Flags().GetString("agent")
	requestID, _ := cmd.Flags().GetString("request-id")
	title, _ := cmd.Flags().GetString("title")
	priority, _ := cmd.Flags().GetString("priority")
	output, _ := cmd.Flags().GetString("output")
	if !delegationUUID(agent) || !delegationUUID(requestID) {
		return fmt.Errorf("--agent and --request-id must be nonzero UUIDs in canonical dashed form")
	}
	if strings.TrimSpace(title) == "" {
		return fmt.Errorf("--title is required")
	}
	if err := validateIssuePriority(priority); err != nil {
		return err
	}
	if output != "json" {
		return fmt.Errorf("--output must be json")
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(client.Token, cli.TaskTokenPrefix) {
		return fmt.Errorf("hierarchy delegation requires a task-scoped mat_ token")
	}
	description, provided, err := resolveTextFlag(cmd, "description")
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(cmd.Context())
	defer cancel()
	parent, err := resolveIssueRef(ctx, client, args[0])
	if err != nil {
		return err
	}
	if !delegationUUID(parent.ID) {
		return fmt.Errorf("parent Issue did not resolve to a nonzero UUID")
	}
	body := map[string]any{
		"request_id": strings.ToLower(requestID), "target_agent_id": strings.ToLower(agent),
		"title": title, "priority": priority,
	}
	if provided {
		body["description"] = description
	}
	var receipt struct {
		IssueID  string `json:"issue_id"`
		Replayed *bool  `json:"replayed"`
	}
	path := "/api/issues/" + url.PathEscape(parent.ID) + "/hierarchy-delegations"
	if err := client.PostJSON(ctx, path, body, &receipt); err != nil {
		return fmt.Errorf("delegation failed; preserve request ID %s and unchanged intent if the outcome is uncertain: %w", strings.ToLower(requestID), err)
	}
	if !delegationUUID(receipt.IssueID) || receipt.Replayed == nil {
		return fmt.Errorf("invalid delegation receipt; preserve request ID %s and unchanged intent before checking the outcome", strings.ToLower(requestID))
	}
	receipt.IssueID = strings.ToLower(receipt.IssueID)
	return cli.PrintJSON(cmd.OutOrStdout(), receipt)
}
