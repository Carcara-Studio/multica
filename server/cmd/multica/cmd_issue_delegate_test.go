package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/multica-ai/multica/server/internal/cli"
)

const delegationParent = "a57c0511-1ebc-471d-a314-438ca16cc75d"
const delegationAgent = "356c8712-6100-4fb2-ac42-513770588468"
const delegationRequest = "10203040-0000-4000-8000-000000000001"
const delegationChild = "10203040-0000-4000-8000-000000000002"

func delegationArgs() []string {
	return []string{delegationParent, "--agent", delegationAgent, "--request-id", delegationRequest, "--title", "Revisão 分析"}
}

func delegationTestServer(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	t.Chdir(t.TempDir())
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "mat_test")
}

func executeDelegation(args []string) (string, error) {
	cmd := newIssueDelegateCommand()
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(io.Discard)
	cmd.SilenceErrors, cmd.SilenceUsage = true, true
	cmd.SetArgs(args)
	err := cmd.Execute()
	return output.String(), err
}

func TestIssueDelegateRegistered(t *testing.T) {
	cmd, _, err := issueCmd.Find([]string{"delegate"})
	if err != nil || cmd.Name() != "delegate" {
		t.Fatalf("delegation command missing: %v", err)
	}
}

func TestIssueDelegateStableIntentAndReceipt(t *testing.T) {
	var bodies []map[string]any
	delegationTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/issues/"+delegationParent+"/hierarchy-delegations" {
			t.Errorf("unexpected endpoint: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer mat_test" || r.Header.Get("X-Workspace-ID") != "ws-1" {
			t.Error("native credential/workspace headers missing")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		bodies = append(bodies, body)
		if len(bodies) == 1 {
			w.WriteHeader(http.StatusCreated)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"issue_id": delegationChild, "replayed": len(bodies) > 1})
	})
	for i := range 2 {
		out, err := executeDelegation(append(delegationArgs(), "--description", "检查"))
		if err != nil {
			t.Fatal(err)
		}
		var receipt struct {
			IssueID  string `json:"issue_id"`
			Replayed bool   `json:"replayed"`
		}
		if err := json.Unmarshal([]byte(out), &receipt); err != nil || receipt.IssueID != delegationChild || receipt.Replayed != (i == 1) {
			t.Fatalf("invalid receipt %s: %v", out, err)
		}
	}
	want := map[string]any{"request_id": delegationRequest, "target_agent_id": delegationAgent, "title": "Revisão 分析", "priority": "none", "description": "检查"}
	if len(bodies) != 2 || !reflect.DeepEqual(bodies[0], want) || !reflect.DeepEqual(bodies[1], want) {
		t.Fatalf("intent changed or unexpected requests: %+v", bodies)
	}
}

func TestIssueDelegateErrorsNeverRetryOrFallback(t *testing.T) {
	for _, status := range []int{401, 403, 404, 409, 410, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			delegationTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"error":"denied"}`)
			})
			out, err := executeDelegation(delegationArgs())
			var httpErr *cli.HTTPError
			if !errors.As(err, &httpErr) || httpErr.StatusCode != status || !httpErr.TaskScoped || calls != 1 || out != "" {
				t.Fatalf("calls=%d output=%q error=%v", calls, out, err)
			}
		})
	}
}

func TestIssueDelegateAmbiguousTransportPreservesRequestID(t *testing.T) {
	var calls atomic.Int32
	delegationTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		_ = conn.Close()
	})
	out, err := executeDelegation(delegationArgs())
	if err == nil || !strings.Contains(err.Error(), delegationRequest) || calls.Load() != 1 || out != "" {
		t.Fatalf("calls=%d output=%q error=%v", calls.Load(), out, err)
	}
}

func TestIssueDelegateRejectsMalformedReceipts(t *testing.T) {
	for _, body := range []string{
		`{`, `null`, `{}`, `{"issue_id":"bad","replayed":false}`,
		`{"issue_id":"00000000-0000-0000-0000-000000000000","replayed":false}`,
		`{"issue_id":"` + delegationChild + `"}`,
		`{"issue_id":"` + delegationChild + `","replayed":null}`,
		`{"issue_id":"` + delegationChild + `","replayed":"false"}`,
	} {
		t.Run(body, func(t *testing.T) {
			calls := 0
			delegationTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				_, _ = io.WriteString(w, body)
			})
			out, err := executeDelegation(delegationArgs())
			if err == nil || !strings.Contains(err.Error(), delegationRequest) || calls != 1 || out != "" {
				t.Fatalf("calls=%d output=%q error=%v", calls, out, err)
			}
		})
	}
}

func TestIssueDelegateValidatesBeforeHTTP(t *testing.T) {
	calls := 0
	delegationTestServer(t, func(w http.ResponseWriter, r *http.Request) { calls++ })
	for _, flags := range [][]string{
		{"--agent", "display name"}, {"--request-id", ""},
		{"--request-id", "00000000-0000-0000-0000-000000000000"},
		{"--title", " "}, {"--priority", "unknown"}, {"--output", "table"},
		{"--description", "inline", "--description-stdin"},
	} {
		if _, err := executeDelegation(append(delegationArgs(), flags...)); err == nil {
			t.Errorf("accepted invalid flags %v", flags)
		}
	}
	t.Setenv("MULTICA_TOKEN", "human-test-token")
	if _, err := executeDelegation(delegationArgs()); err == nil {
		t.Error("accepted a human token")
	}
	if calls != 0 {
		t.Fatalf("invalid invocation performed %d requests", calls)
	}
}

func TestIssueDelegateDescriptionSources(t *testing.T) {
	var got string
	delegationTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		got, _ = body["description"].(string)
		_ = json.NewEncoder(w).Encode(map[string]any{"issue_id": delegationChild, "replayed": false})
	})
	const note = "说明\nLiteral \\n\n"
	if err := os.WriteFile("description.md", []byte(note), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := executeDelegation(append(delegationArgs(), "--description-file", "description.md")); err != nil {
		t.Fatal(err)
	}
	if got != strings.TrimSuffix(note, "\n") {
		t.Fatalf("file description changed: %q", got)
	}
	pipeStdin(t, note, func() {
		if _, err := executeDelegation(append(delegationArgs(), "--description-stdin")); err != nil {
			t.Fatal(err)
		}
	})
	if got != strings.TrimSuffix(note, "\n") {
		t.Fatalf("stdin description changed: %q", got)
	}
}

func TestIssueDelegateDescriptionFileContainment(t *testing.T) {
	calls := 0
	delegationTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		_ = json.NewEncoder(w).Encode(map[string]any{"issue_id": delegationChild, "replayed": false})
	})
	external := filepath.Join(t.TempDir(), "external.md")
	if err := os.WriteFile(external, []byte("description"), 0600); err != nil {
		t.Fatal(err)
	}
	args := append(delegationArgs(), "--description-file", external)
	if _, err := executeDelegation(args); err == nil || calls != 0 {
		t.Fatalf("external file accepted without override: calls=%d err=%v", calls, err)
	}
	if _, err := executeDelegation(append(args, "--allow-external-file")); err != nil || calls != 1 {
		t.Fatalf("explicit file override failed: calls=%d err=%v", calls, err)
	}
}

func TestIssueDelegateResolvesParentKey(t *testing.T) {
	for _, id := range []string{delegationParent, "bad"} {
		t.Run(id, func(t *testing.T) {
			var calls []string
			delegationTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				calls = append(calls, r.Method+" "+r.URL.Path)
				if r.Method == http.MethodGet {
					_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "identifier": "ABC-42"})
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"issue_id": delegationChild, "replayed": false})
			})
			args := delegationArgs()
			args[0] = "ABC-42"
			_, err := executeDelegation(args)
			want := []string{"GET /api/issues/ABC-42"}
			if id == delegationParent {
				want = append(want, "POST /api/issues/"+id+"/hierarchy-delegations")
			}
			if !reflect.DeepEqual(calls, want) || (err == nil) != (id == delegationParent) {
				t.Fatalf("calls=%v error=%v", calls, err)
			}
		})
	}
}
