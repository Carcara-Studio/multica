package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/projectmemory"
)

const continuityTestID = "11111111-1111-4111-8111-111111111111"

func continuityCLISetup(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	t.Chdir(t.TempDir())
	for _, key := range []string{"MULTICA_AGENT_ID", "MULTICA_TASK_ID", "MULTICA_DAEMON_PORT", "MULTICA_TASK_CONFIG_ROOT"} {
		t.Setenv(key, "")
	}
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	setCLITestServerEnv(t, srv.URL)
}
func TestProjectContinuityCLIReadPublishAndClear(t *testing.T) {
	for _, action := range []string{"get", "set", "clear"} {
		t.Run(action, func(t *testing.T) {
			calls := 0
			continuityCLISetup(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != "/api/projects/"+continuityTestID+"/continuity" || r.Header.Get("X-Workspace-ID") != "ws-1" {
					t.Error("wrong scope")
				}
				methods := map[string]string{"get": "GET", "set": "PUT", "clear": "DELETE"}
				if r.Method != methods[action] {
					t.Error("wrong method")
				}
				if action != "get" {
					var request map[string]any
					if json.NewDecoder(r.Body).Decode(&request) != nil || request["expected_revision"] != continuityTestID {
						t.Error("revision changed")
					}
				}
				content := "Decision: scoped context"
				if action == "clear" {
					content = ""
				}
				_ = json.NewEncoder(w).Encode(projectmemory.Checkpoint{ProjectID: continuityTestID, Revision: continuityTestID, Content: content, ReviewedBy: continuityTestID, ReviewedAt: "2026-10-29T12:00:00Z"})
			})
			cmd := newProjectContinuityCmd()
			var out bytes.Buffer
			cmd.SetOut(&out)
			args := []string{action, continuityTestID}
			if action != "get" {
				body := map[string]any{"expected_revision": continuityTestID}
				if action == "set" {
					body["content"] = "Decision: scoped context"
				}
				raw, _ := json.Marshal(body)
				if action == "set" {
					file := filepath.Join(t.TempDir(), "checkpoint.json")
					if err := os.WriteFile(file, raw, 0600); err != nil {
						t.Fatal(err)
					}
					args = append(args, "--file", file)
				} else {
					cmd.SetIn(bytes.NewReader(raw))
					args = append(args, "--file", "-")
				}
			}
			cmd.SetArgs(args)
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			if calls != 1 || !strings.Contains(out.String(), `"revision"`) {
				t.Fatalf("unexpected outcome: %d %s", calls, out.String())
			}
		})
	}
}

func TestProjectContinuityCLIFailsWithoutReplayOrBodyLeak(t *testing.T) {
	for _, status := range []int{200, 409} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			continuityCLISetup(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"error":"do-not-print-secret"}`))
			})
			cmd := newProjectContinuityCmd()
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetArgs([]string{"set", continuityTestID, "--file", "-"})
			cmd.SetIn(strings.NewReader(`{"expected_revision":"","content":"note"}`))
			err := cmd.Execute()
			if err == nil || calls != 1 || strings.Contains(out.String()+err.Error(), "do-not-print-secret") {
				t.Fatalf("unsafe failure: %v calls=%d", err, calls)
			}
		})
	}
}

func TestProjectContinuityCLIBlocksAgentContext(t *testing.T) {
	calls := 0
	continuityCLISetup(t, func(w http.ResponseWriter, r *http.Request) { calls++ })
	t.Setenv("MULTICA_AGENT_ID", continuityTestID)
	cmd := newProjectContinuityCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"get", continuityTestID})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "daemon-managed task") || calls != 0 {
		t.Fatalf("agent gate: %v calls=%d", err, calls)
	}
}
