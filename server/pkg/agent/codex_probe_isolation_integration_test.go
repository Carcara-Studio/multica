//go:build agentintegration

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// This captures a request to a fake provider, not a real inference or an approved
// recovery adapter. Keep the opt-in gate ahead of installed executable lookup.
func TestCodexProbeIsolationCapture(t *testing.T) {
	if os.Getenv("MULTICA_RUN_REAL_AGENT_SMOKE") != "1" {
		t.Skip("set MULTICA_RUN_REAL_AGENT_SMOKE=1 for the native CLI/local fake-provider capture")
	}
	cli := os.Getenv("MULTICA_CODEX_PROBE_CLI")
	if cli == "" || !filepath.IsAbs(cli) {
		t.Fatal("MULTICA_CODEX_PROBE_CLI must explicitly name an absolute native CLI path")
	}
	for _, isolated := range []bool{false, true} {
		t.Run(fmt.Sprintf("isolated=%v", isolated), func(t *testing.T) {
			root := t.TempDir()
			home, cwd := filepath.Join(root, "home"), filepath.Join(root, "workspace")
			for _, dir := range []string{home, cwd} {
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
			}
			write := func(path, content string) {
				t.Helper()
				if err := os.WriteFile(path, []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			write(filepath.Join(cwd, "AGENTS.md"), "PROBE_PROJECT_CANARY_8F32")
			catalog := filepath.Join(root, "catalog.json")
			write(catalog, codexProbeCaptureCatalog)
			instructions := filepath.Join(root, "instructions.txt")
			write(instructions, "Reply OK.")
			requests := make(chan []byte, 8)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
				if err != nil || r.Method != http.MethodPost || r.URL.Path != "/v1/responses" {
					t.Errorf("unexpected local request: %s %s (%v)", r.Method, r.URL.Path, err)
				}
				select {
				case requests <- body:
				default:
					t.Error("unexpected request retries")
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, `{"error":{"type":"invalid_request_error","message":"MOCK_CAPTURE_ONLY"}}`)
			}))
			defer server.Close()
			config := fmt.Sprintf(codexProbeCaptureConfig, filepath.ToSlash(catalog), filepath.ToSlash(instructions), server.URL)
			if isolated {
				config += "\ngoals = false\n\n[skills]\ninclude_instructions = false\n[skills.bundled]\nenabled = false\n"
			}
			write(filepath.Join(home, "config.toml"), config)
			// No inherited tokens, proxies, provider overrides or real CODEX_HOME.
			env := []string{"CODEX_HOME=" + home, "HOME=" + home, "USERPROFILE=" + home, "RUST_LOG=error"}
			for _, name := range []string{"SYSTEMROOT", "WINDIR", "PATH", "TEMP", "TMP", "COMSPEC"} {
				if value := os.Getenv(name); value != "" {
					env = append(env, name+"="+value)
				}
			}
			ctx, cancel := context.WithTimeout(t.Context(), 35*time.Second)
			defer cancel()
			version := exec.CommandContext(ctx, cli, "--version")
			version.Env = env
			out, err := version.Output()
			if err != nil || strings.TrimSpace(string(out)) != "codex-cli 0.156.1" {
				t.Fatalf("capture contract is pinned to Codex0.156.1: %q (%v)", out, err)
			}
			cmd := exec.CommandContext(ctx, cli, "exec", "--ephemeral", "--skip-git-repo-check", "--strict-config", "-C", cwd, "Reply OK.")
			cmd.Env = env
			out, err = cmd.CombinedOutput()
			if ctx.Err() != nil || err == nil || !strings.Contains(string(out), "MOCK_CAPTURE_ONLY") {
				t.Fatalf("expected deliberate local provider rejection, got %v: %s", err, out)
			}
			if len(requests) != 1 {
				t.Fatalf("captured %d requests, want exactly one", len(requests))
			}
			raw := <-requests
			var request struct {
				Model        string            `json:"model"`
				Instructions string            `json:"instructions"`
				Tools        []json.RawMessage `json:"tools"`
				Input        []struct {
					Role    string `json:"role"`
					Content []struct {
						Text string `json:"text"`
					} `json:"content"`
				} `json:"input"`
			}
			if err := json.Unmarshal(raw, &request); err != nil {
				t.Fatal(err)
			}
			if request.Model != "probe-isolation-fixture" || request.Instructions != "Reply OK." || strings.Contains(string(raw), "PROBE_PROJECT_CANARY_8F32") {
				t.Fatal("unexpected model/instructions or leaked project canary")
			}
			if isolated {
				if len(request.Tools) != 0 || len(request.Input) != 1 || request.Input[0].Role != "user" || len(request.Input[0].Content) != 1 || request.Input[0].Content[0].Text != "Reply OK." {
					t.Fatalf("isolation violated: %d tools, %d input messages", len(request.Tools), len(request.Input))
				}
			} else if len(request.Tools) == 0 || !strings.Contains(string(raw), "skills_instructions") {
				t.Fatal("negative control did not expose native goal tools and skill instructions")
			}
		})
	}
}

// Synthetic metadata exercises the pinned CLI's tool registry; it is not a
// claim that this fixture model exists or is eligible for production recovery.
const codexProbeCaptureCatalog = `{"models":[{
 "slug":"probe-isolation-fixture","display_name":"Probe fixture","description":"Local capture only","base_instructions":"Reply OK.",
 "supported_reasoning_levels":[],"shell_type":"disabled","visibility":"list","supported_in_api":true,"priority":0,
 "availability_nux":null,"upgrade":null,"default_reasoning_summary":"none","support_verbosity":false,
 "default_verbosity":null,"apply_patch_tool_type":null,"truncation_policy":{"mode":"tokens","limit":10000},
 "context_window":32000,"experimental_supported_tools":[],"input_modalities":["text"],
 "supports_search_tool":false,"include_skills_usage_instructions":false,
 "include_plugin_usage_instructions":false,"include_apps_usage_instructions":false,
 "supports_experimental_context":false,"node_repl_disabled":true
}]}`

const codexProbeCaptureConfig = `model = "probe-isolation-fixture"
model_provider = "capture"
model_catalog_json = "%s"
model_instructions_file = "%s"
project_doc_max_bytes = 0
approval_policy = "never"
sandbox_mode = "read-only"
include_environment_context = false
include_permissions_instructions = false
include_collaboration_mode_instructions = false
include_apps_instructions = false
web_search = "disabled"
notify = []
[windows]
sandbox = "mxc"
[model_providers.capture]
name = "Local synthetic capture"
base_url = "%s/v1"
wire_api = "responses"
requires_openai_auth = false
request_max_retries = 0
stream_max_retries = 0
[tools.update_plan]
enabled = false
[tools.experimental_request_user_input]
enabled = false
[features]
shell_tool = false
unified_exec = false
shell_snapshot = false
powershell_shell_version = false
apply_patch_freeform = false
view_image = false
apps = false
plugins = false
remote_plugin = false
hooks = false
plugin_hooks = false
multi_agent = false
multi_agent_v2 = false
code_mode = false
code_mode_only = false
js_repl = false
image_generation = false
browser_use = false
computer_use = false
standalone_web_search = false
tool_search = false
search_tool = false
tool_suggest = false
sleep_tool = false
current_time_reminder = false
request_permissions_tool = false
send_message_to_user_async = false
token_budget = false
deferred_executor = false
memories = false
skip_host_skill_discovery = true
remote_models = false
`
