package main

import (
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
)

func init() { issueCmd.AddCommand(newIssueEscalateCommand()) }
func newIssueEscalateCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "escalate <issue-id-or-key>", Short: "Request a direct-manager decision or root human intervention", Long: "Requires a running task's mat_ token. Persist --request-id before calling. Reuse it only with unchanged reason/context after an uncertain outcome or in a later authenticated turn. The server derives the direct manager; root records human intervention. No automatic retry, manager skipping, reassignment or fallback to ordinary Issue creation.", Args: cobra.ExactArgs(1), RunE: runIssueEscalate}
	f := cmd.Flags()
	f.String("request-id", "", "Persisted nonzero request UUID")
	f.String("reason", "", "Decision needed (required, at most 8000 bytes)")
	f.String("context", "", "Relevant context (at most 16000 bytes)")
	f.String("context-file", "", "Read context from a file within the working directory")
	f.Bool("context-stdin", false, "Read context from stdin")
	f.Bool("allow-external-file", false, "Allow a context file outside the working directory")
	f.String("output", "json", "Output format (json)")
	return cmd
}
func runIssueEscalate(cmd *cobra.Command, args []string) error {
	key, _ := cmd.Flags().GetString("request-id")
	reason, _ := cmd.Flags().GetString("reason")
	output, _ := cmd.Flags().GetString("output")
	if !delegationUUID(key) {
		return fmt.Errorf("--request-id must be a persisted nonzero UUID")
	}
	reason = strings.TrimSpace(reason)
	if reason == "" || len(reason) > 8000 || !utf8.ValidString(reason) || strings.ContainsRune(reason, 0) {
		return fmt.Errorf("--reason must be valid text of 1-8000 bytes")
	}
	if output != "json" {
		return fmt.Errorf("--output must be json")
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(client.Token, cli.TaskTokenPrefix) {
		return fmt.Errorf("hierarchy escalation requires a task-scoped mat_ token")
	}
	context, _, err := resolveTextFlag(cmd, "context")
	if err != nil {
		return err
	}
	context = strings.TrimSpace(context)
	if len(context) > 16000 || !utf8.ValidString(context) || strings.ContainsRune(context, 0) {
		return fmt.Errorf("context must be valid text of at most 16000 bytes")
	}
	ctx, cancel := cli.APIContext(cmd.Context())
	defer cancel()
	issue, err := resolveIssueRef(ctx, client, args[0])
	if err != nil {
		return err
	}
	if !delegationUUID(issue.ID) {
		return fmt.Errorf("Issue did not resolve to a nonzero UUID")
	}
	var receipt struct {
		IssueID  string `json:"issue_id"`
		Replayed *bool  `json:"replayed"`
	}
	key = strings.ToLower(key)
	if err = client.PostJSON(ctx, "/api/issues/"+url.PathEscape(issue.ID)+"/hierarchy-escalations", map[string]string{"request_id": key, "reason": reason, "context": context}, &receipt); err != nil {
		return fmt.Errorf("escalation failed; preserve request ID %s and unchanged reason/context if outcome is uncertain: %w", key, err)
	}
	if !delegationUUID(receipt.IssueID) || receipt.Replayed == nil {
		return fmt.Errorf("invalid escalation receipt; preserve request ID %s and unchanged reason/context", key)
	}
	receipt.IssueID = strings.ToLower(receipt.IssueID)
	return cli.PrintJSON(cmd.OutOrStdout(), receipt)
}
