package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/projectmemory"
	"github.com/spf13/cobra"
)

func newProjectContinuityCmd() *cobra.Command {
	group := &cobra.Command{Use: "continuity", Short: "Publish or read a bounded project continuity checkpoint"}
	for _, action := range []string{"get", "set", "clear", "draft"} {
		cmd := &cobra.Command{Use: action + " <project-id>", Short: action + " the project continuity checkpoint", Args: exactArgs(1)}
		cmd.RunE = func(cmd *cobra.Command, args []string) error { return runProjectContinuity(cmd, args, action) }
		if action == "set" || action == "clear" {
			cmd.Flags().String("file", "", "Request JSON file; - reads stdin (includes expected_revision)")
			_ = cmd.MarkFlagRequired("file")
		}
		group.AddCommand(cmd)
	}
	return group
}

func init() { projectCmd.AddCommand(newProjectContinuityCmd()) }

func readContinuityRequest(cmd *cobra.Command, action string) (json.RawMessage, error) {
	file, _ := cmd.Flags().GetString("file")
	if file == "" {
		return nil, fmt.Errorf("--file is required; use - for stdin")
	}
	var input io.Reader = cmd.InOrStdin()
	if file != "-" {
		f, err := os.Open(file)
		if err != nil {
			return nil, fmt.Errorf("could not open continuity request file")
		}
		defer f.Close()
		input = f
	}
	raw, err := io.ReadAll(io.LimitReader(input, 16385))
	if err != nil || len(raw) > 16384 {
		return nil, fmt.Errorf("continuity request exceeds 16 KiB or could not be read")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return nil, fmt.Errorf("continuity request must be one JSON object")
	}
	var expected string
	if json.Unmarshal(fields["expected_revision"], &expected) != nil || string(fields["expected_revision"]) == "null" || (expected != "" && !projectmemory.ValidUUID(expected)) {
		return nil, fmt.Errorf("supply expected_revision from continuity get (empty for first publication)")
	}
	if action == "clear" {
		if len(fields) != 1 {
			return nil, fmt.Errorf("clear accepts only expected_revision")
		}
	} else {
		var content string
		if len(fields) != 2 || json.Unmarshal(fields["content"], &content) != nil || projectmemory.ValidateContent(content) != nil {
			return nil, fmt.Errorf("set requires bounded content and expected_revision only")
		}
	}
	return json.RawMessage(raw), nil
}

func runProjectContinuity(cmd *cobra.Command, args []string, action string) error {
	if err := requireHumanLocalCommand("project continuity " + action); err != nil {
		return err
	}
	var raw json.RawMessage
	var err error
	if action == "set" || action == "clear" {
		raw, err = readContinuityRequest(cmd, action)
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
	path := "/api/projects/" + url.PathEscape(args[0]) + "/continuity"
	var checkpoint projectmemory.Checkpoint
	var draft projectmemory.Draft
	switch action {
	case "draft":
		err = client.GetJSON(ctx, path+"/draft", &draft)
	case "get":
		err = client.GetJSON(ctx, path, &checkpoint)
	case "set":
		err = client.PutJSON(ctx, path, raw, &checkpoint)
	case "clear":
		// DELETE returns the retained revision so the next publication can use CAS.
		err = client.DeleteJSONWithBodyResponse(ctx, path, raw, &checkpoint)
	}
	if err != nil {
		var httpErr *cli.HTTPError
		if errors.As(err, &httpErr) {
			safe := *httpErr
			safe.Body = "continuity request rejected; check input and permissions"
			if safe.StatusCode == 409 {
				safe.Body = "revision changed; get the current checkpoint and review before retrying"
			}
			return &safe
		}
		return fmt.Errorf("continuity request failed; check connection and response")
	}
	if action == "draft" {
		if !draft.Valid() {
			return fmt.Errorf("invalid continuity draft response")
		}
		return cli.PrintJSON(cmd.OutOrStdout(), draft)
	}
	if !checkpoint.Valid() || (action == "set" && checkpoint.Content == "") || (action == "clear" && checkpoint.Content != "") {
		return fmt.Errorf("invalid continuity receipt; publication could not be confirmed")
	}
	return cli.PrintJSON(cmd.OutOrStdout(), checkpoint)
}
