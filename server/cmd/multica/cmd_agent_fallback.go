package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
)

func newAgentFallbackCmd() *cobra.Command {
	group := &cobra.Command{Use: "fallback", Short: "Manage fallback configuration intent (execution remains disabled)"}
	for _, action := range []string{"get", "set", "remove"} {
		command := &cobra.Command{Use: action + " <agent-id>", Short: action + " fallback configuration intent", Args: exactArgs(1)}
		command.RunE = func(cmd *cobra.Command, args []string) error { return runAgentFallback(cmd, args, action) }
		if action != "get" {
			command.Flags().String("file", "", "Read the complete API request JSON from a private file; use - for stdin")
			_ = command.MarkFlagRequired("file")
		}
		group.AddCommand(command)
	}
	return group
}

func init() { agentCmd.AddCommand(newAgentFallbackCmd()) }

type fallbackCLIRevision struct {
	Generation        string `json:"generation"`
	PrimaryGeneration string `json:"primary_generation"`
	StateVersion      *int64 `json:"state_version"`
}

type fallbackCLIConfiguration struct {
	AgentID          string   `json:"agent_id"`
	RuntimeID        string   `json:"runtime_id"`
	Mode             string   `json:"mode"`
	Model            string   `json:"model"`
	ThinkingLevel    string   `json:"thinking_level"`
	ServiceTier      string   `json:"service_tier"`
	EnvironmentKeys  []string `json:"environment_keys"`
	ArgumentCount    int      `json:"argument_count"`
	HasMCPConfig     bool     `json:"has_mcp_config"`
	HasRuntimeConfig bool     `json:"has_runtime_config"`
}

// Only these metadata fields may reach stdout, even if a server adds fields.
type fallbackCLIReceipt struct {
	Configured       *bool                     `json:"configured"`
	ExecutionEnabled *bool                     `json:"execution_enabled"`
	Revision         fallbackCLIRevision       `json:"revision"`
	Configuration    *fallbackCLIConfiguration `json:"configuration"`
}

func fallbackCLIUUID(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id != uuid.Nil
}

func (r fallbackCLIRevision) valid() bool {
	if r.StateVersion == nil || *r.StateVersion < 0 {
		return false
	}
	if *r.StateVersion == 0 {
		return r.Generation == "" && r.PrimaryGeneration == ""
	}
	return fallbackCLIUUID(r.PrimaryGeneration) && (r.Generation == "" || fallbackCLIUUID(r.Generation))
}

func (r *fallbackCLIReceipt) valid(action string) bool {
	if r.Configured == nil || r.ExecutionEnabled == nil || *r.ExecutionEnabled || !r.Revision.valid() {
		return false
	}
	if !*r.Configured {
		return action != "set" && r.Configuration == nil && r.Revision.Generation == ""
	}
	c := r.Configuration
	if c == nil || !fallbackCLIUUID(c.AgentID) || !fallbackCLIUUID(c.RuntimeID) || r.Revision.Generation == "" || c.ArgumentCount < 0 {
		return false
	}
	if action == "set" && c.Mode != "native_login_path" {
		return false
	}
	if c.Mode != "strict" && c.Mode != "native_login_path" {
		c.Mode = "unknown"
	}
	if c.EnvironmentKeys == nil {
		c.EnvironmentKeys = []string{}
	}
	return true
}

func readFallbackCLIRequest(cmd *cobra.Command, action string) (json.RawMessage, error) {
	file, _ := cmd.Flags().GetString("file")
	var input io.Reader = cmd.InOrStdin()
	if file == "" {
		return nil, fmt.Errorf("--file is required (use - for stdin)")
	}
	if file != "-" {
		f, err := os.Open(file)
		if err != nil {
			return nil, fmt.Errorf("could not open fallback request file")
		}
		defer f.Close()
		input = f
	}
	raw, err := io.ReadAll(io.LimitReader(input, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		return nil, fmt.Errorf("could not read fallback request (maximum 1 MiB)")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	var body map[string]json.RawMessage
	if d.Decode(&body) != nil || body == nil || d.Decode(new(any)) != io.EOF {
		return nil, fmt.Errorf("fallback request must be one JSON object")
	}
	var revision fallbackCLIRevision
	if json.Unmarshal(body["expected_revision"], &revision) != nil || !revision.valid() {
		return nil, fmt.Errorf("supply expected_revision from the latest fallback get; no automatic revision refresh is performed")
	}
	if action == "remove" && len(body) != 1 {
		return nil, fmt.Errorf("remove accepts only expected_revision")
	}
	if action == "set" {
		var mode string
		if json.Unmarshal(body["mode"], &mode) != nil || mode != "native_login_path" {
			return nil, fmt.Errorf("set requires explicit native_login_path mode")
		}
	}
	return json.RawMessage(raw), nil
}

func fallbackCLIError(err error) error {
	var httpError *cli.HTTPError
	if errors.As(err, &httpError) {
		copy := *httpError
		copy.Body = "fallback request rejected; check input and permissions"
		if copy.StatusCode == 409 {
			copy.Body = "revision changed; read fallback get and review the request before retrying"
		}
		return &copy
	}
	// An untrusted proxy/decoder error may contain fragments of secret input.
	return fmt.Errorf("fallback request failed; check the connection and server response")
}

func runAgentFallback(cmd *cobra.Command, args []string, action string) error {
	if err := requireHumanLocalCommand("agent fallback " + action); err != nil {
		return err
	}
	var body json.RawMessage
	var err error
	if action != "get" {
		body, err = readFallbackCLIRequest(cmd, action)
		if err != nil {
			return err
		}
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	path := "/api/agents/" + url.PathEscape(args[0]) + "/fallback"
	var receipt fallbackCLIReceipt
	switch action {
	case "get":
		err = client.GetJSON(ctx, path, &receipt)
	case "set":
		err = client.PutJSON(ctx, path, body, &receipt)
	case "remove":
		err = client.DeleteJSONWithBody(ctx, path, body)
	default:
		return fmt.Errorf("unknown fallback operation")
	}
	if err != nil {
		return fallbackCLIError(err)
	}
	if action == "remove" {
		_, err = fmt.Fprintln(cmd.OutOrStdout(), "Fallback configuration removed; execution remains disabled.")
		return err
	}
	if !receipt.valid(action) {
		return fmt.Errorf("invalid fallback receipt; configuration success could not be confirmed")
	}
	return cli.PrintJSON(cmd.OutOrStdout(), receipt)
}
