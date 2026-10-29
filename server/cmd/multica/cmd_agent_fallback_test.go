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
)

const fallbackTestID = "11111111-1111-4111-8111-111111111111"
const fallbackTestRevision = `{"generation":"","primary_generation":"","state_version":0}`
const fallbackTestRequest = `{"mode":"native_login_path","runtime_id":"` + fallbackTestID + `","model":"model-test","expected_revision":` + fallbackTestRevision + `,"overrides":{"version":1,"custom_env":{"KEY":"input-secret"},"custom_args":[],"runtime_config":{},"mcp_config":null}}`
const fallbackTestReceipt = `{"configured":true,"execution_enabled":false,"revision":{"generation":"` + fallbackTestID + `","primary_generation":"` + fallbackTestID + `","state_version":1},"configuration":{"agent_id":"` + fallbackTestID + `","runtime_id":"` + fallbackTestID + `","mode":"native_login_path","model":"model-test","custom_env":{"KEY":"response-secret"}},"secret":"response-secret"}`

func fallbackCLISetup(t *testing.T, handler http.HandlerFunc) string {
	t.Helper()
	t.Chdir(t.TempDir())
	for _, key := range []string{"MULTICA_AGENT_ID", "MULTICA_TASK_ID", "MULTICA_DAEMON_PORT", "MULTICA_TASK_CONFIG_ROOT"} {
		t.Setenv(key, "")
	}
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	setCLITestServerEnv(t, srv.URL)
	return srv.URL
}

func TestAgentFallbackCLIRequestsAndRedactedReceipts(t *testing.T) {
	for _, action := range []string{"get", "set", "remove"} {
		t.Run(action, func(t *testing.T) {
			calls := 0
			fallbackCLISetup(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				methods := map[string]string{"get": "GET", "set": "PUT", "remove": "DELETE"}
				if r.Method != methods[action] || r.URL.EscapedPath() != "/api/agents/agent%2Fother/fallback" || r.Header.Get("X-Workspace-ID") != "ws-1" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.EscapedPath())
				}
				if action != "get" {
					var body map[string]json.RawMessage
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil || string(body["expected_revision"]) != fallbackTestRevision {
						t.Errorf("revision changed: %s, err=%v", body["expected_revision"], err)
					}
					if action == "set" && !strings.Contains(string(body["overrides"]), "input-secret") {
						t.Error("override not sent")
					}
				}
				if action == "remove" {
					w.WriteHeader(204)
					return
				}
				_, _ = w.Write([]byte(fallbackTestReceipt))
			})
			cmd := newAgentFallbackCmd()
			var out bytes.Buffer
			cmd.SetOut(&out)
			request := fallbackTestRequest
			if action == "remove" {
				request = `{"expected_revision":` + fallbackTestRevision + `}`
			}
			args := []string{action, "agent/other"}
			if action != "get" {
				if action == "set" {
					file := filepath.Join(t.TempDir(), "private-request.json")
					if err := os.WriteFile(file, []byte(request), 0600); err != nil {
						t.Fatal(err)
					}
					args = append(args, "--file", file)
				} else {
					cmd.SetIn(strings.NewReader(request))
					args = append(args, "--file", "-")
				}
			}
			cmd.SetArgs(args)
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			if calls != 1 || strings.Contains(out.String(), "secret") || strings.Contains(out.String(), "custom_env") {
				t.Fatalf("unsafe output or unexpected requests: %d %s", calls, out.String())
			}
			if action != "remove" && !strings.Contains(out.String(), `"execution_enabled": false`) {
				t.Fatal("missing disabled receipt")
			}
		})
	}
}

func TestAgentFallbackCLIFailuresDoNotEchoSecretsOrRetry(t *testing.T) {
	for _, tc := range []struct {
		name, action, response string
		status                 int
	}{
		{"conflict", "set", "input-secret response-secret", 409},
		{"denied", "remove", "input-secret response-secret", 403},
		{"malformed", "get", `{"configured":"response-secret"}`, 200},
		{"absent-receipt", "set", `{}`, 200},
		{"enabled", "get", strings.Replace(fallbackTestReceipt, `"execution_enabled":false`, `"execution_enabled":true`, 1), 200},
		{"wrong-mode", "set", strings.Replace(fallbackTestReceipt, "native_login_path", "strict", 1), 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			fallbackCLISetup(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.response))
			})
			cmd := newAgentFallbackCmd()
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			args := []string{tc.action, fallbackTestID}
			request := fallbackTestRequest
			if tc.action == "remove" {
				request = `{"expected_revision":` + fallbackTestRevision + `}`
			}
			if tc.action != "get" {
				args = append(args, "--file", "-")
				cmd.SetIn(strings.NewReader(request))
			}
			cmd.SetArgs(args)
			err := cmd.Execute()
			if err == nil || calls != 1 {
				t.Fatalf("err=%v calls=%d", err, calls)
			}
			if strings.Contains(out.String()+err.Error(), "secret") {
				t.Fatal("error leaked secret")
			}
		})
	}
}

func TestAgentFallbackCLIRejectsBadInputAndTaskContextBeforeHTTP(t *testing.T) {
	calls := 0
	fallbackCLISetup(t, func(w http.ResponseWriter, r *http.Request) { calls++ })
	for _, input := range []string{"", "null", `{} {}`, `{"mode":"native_login_path"}`, strings.Repeat("x", (1<<20)+1)} {
		cmd := newAgentFallbackCmd()
		cmd.SetIn(strings.NewReader(input))
		cmd.SetArgs([]string{"set", fallbackTestID, "--file", "-"})
		cmd.SilenceErrors, cmd.SilenceUsage = true, true
		if err := cmd.Execute(); err == nil {
			t.Fatal("accepted invalid input")
		}
	}
	t.Setenv("MULTICA_AGENT_ID", fallbackTestID)
	for _, action := range []string{"get", "set", "remove"} {
		cmd := newAgentFallbackCmd()
		args := []string{action, fallbackTestID}
		if action != "get" {
			args = append(args, "--file", "-")
			cmd.SetIn(strings.NewReader(fallbackTestRequest))
		}
		cmd.SetArgs(args)
		cmd.SilenceErrors, cmd.SilenceUsage = true, true
		if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "daemon-managed task") {
			t.Fatalf("task gate: %v", err)
		}
	}
	if calls != 0 {
		t.Fatalf("invalid requests reached HTTP: %d", calls)
	}
}

func TestAgentFallbackCLIReadsUnconfiguredHistoryAndUnknownMode(t *testing.T) {
	for _, response := range []string{
		`{"configured":false,"execution_enabled":false,"revision":{"generation":"","primary_generation":"` + fallbackTestID + `","state_version":3},"configuration":null}`,
		strings.Replace(fallbackTestReceipt, "native_login_path", "future-mode", 1),
	} {
		t.Run(response, func(t *testing.T) {
			fallbackCLISetup(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(response)) })
			cmd := newAgentFallbackCmd()
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetArgs([]string{"get", fallbackTestID})
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(response, "future-mode") && !strings.Contains(out.String(), `"mode": "unknown"`) {
				t.Fatal("unknown mode not normalized")
			}
			if strings.Contains(out.String(), "response-secret") {
				t.Fatal("unknown field leaked")
			}
		})
	}
}
