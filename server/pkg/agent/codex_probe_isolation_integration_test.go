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
	"runtime"
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
	for _, tc := range []struct {
		name                                                                 string
		isolated, capabilityRich, shellControl, modelTools, ceiling, success bool
		wantTools                                                            []string
		hookSource                                                           string
		hookControl                                                          bool
	}{
		{name: "goal_skill_control"},
		{name: "isolated", isolated: true},
		{name: "shell_capable", isolated: true, capabilityRich: true},
		{name: "shell_control", isolated: true, capabilityRich: true, shellControl: true, wantTools: []string{"exec_command", "write_stdin"}},
		{name: "model_tools_control", isolated: true, capabilityRich: true, modelTools: true, wantTools: []string{"apply_patch", "request_user_input_async"}},
		{name: "catalog_ceiling", isolated: true, capabilityRich: true, modelTools: true, ceiling: true, success: true},
		{name: "user_hook_control", isolated: true, hookSource: "user", hookControl: true},
		{name: "user_hook_blocked", isolated: true, hookSource: "user"},
		{name: "project_hook_control", isolated: true, hookSource: "project", hookControl: true},
		{name: "project_hook_blocked", isolated: true, hookSource: "project"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.hookSource != "" && runtime.GOOS != "windows" {
				t.Skip("command hook fixture explicitly exercises the Windows shell")
			}
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
			hookMarker := filepath.Join(root, "hook-ran.txt")
			if tc.hookSource != "" {
				hookDir := home
				if tc.hookSource == "project" {
					hookDir = filepath.Join(cwd, ".codex")
					for _, dir := range []string{hookDir, filepath.Join(cwd, ".git", "objects"), filepath.Join(cwd, ".git", "refs")} {
						if err := os.MkdirAll(dir, 0700); err != nil {
							t.Fatal(err)
						}
					}
					write(filepath.Join(cwd, ".git", "HEAD"), "ref: refs/heads/main\n")
					write(filepath.Join(cwd, ".git", "config"), "[core]\nrepositoryformatversion = 0\nbare = false\n")
					write(filepath.Join(hookDir, "config.toml"), "[features]\nhooks = true\n")
				}
				command := fmt.Sprintf(`[System.IO.File]::WriteAllText('%s', 'PROBE_HOOK_RAN'); Write-Output 'PROBE_HOOK_CONTEXT_6B91'`, strings.ReplaceAll(hookMarker, "'", "''"))
				hooks, err := json.Marshal(map[string]any{"hooks": map[string]any{"SessionStart": []any{map[string]any{"hooks": []any{map[string]any{"type": "command", "command": command, "commandWindows": command, "timeout": 5}}}}}})
				if err != nil {
					t.Fatal(err)
				}
				write(filepath.Join(hookDir, "hooks.json"), string(hooks))
			}
			catalog := filepath.Join(root, "catalog.json")
			modelCatalog := codexProbeCaptureCatalog
			if tc.capabilityRich {
				modelCatalog = strings.NewReplacer(
					`"shell_type":"disabled"`, `"shell_type":"unified_exec"`,
					`"supports_search_tool":false`, `"supports_search_tool":true`,
					`"node_repl_disabled":true`, `"node_repl_disabled":false`,
					`"include_skills_usage_instructions":false`, `"include_skills_usage_instructions":true`,
					`"include_plugin_usage_instructions":false`, `"include_plugin_usage_instructions":true`,
					`"include_apps_usage_instructions":false`, `"include_apps_usage_instructions":true`,
				).Replace(modelCatalog)
			}
			if tc.modelTools {
				modelCatalog = strings.NewReplacer(
					`"apply_patch_tool_type":null`, `"apply_patch_tool_type":"freeform"`,
					`"experimental_supported_tools":[]`, `"experimental_supported_tools":["clock","request_user_input_async","send_message_to_user_async"]`,
				).Replace(modelCatalog)
			}
			if tc.ceiling {
				modelCatalog = strings.NewReplacer(
					`"apply_patch_tool_type":"freeform"`, `"apply_patch_tool_type":null`,
					`"experimental_supported_tools":["clock","request_user_input_async","send_message_to_user_async"]`, `"experimental_supported_tools":[]`,
				).Replace(modelCatalog)
			}
			write(catalog, modelCatalog)
			instructions := filepath.Join(root, "instructions.txt")
			write(instructions, "Reply OK.")
			requests := make(chan []byte, 8)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.hookSource != "" {
					marker, err := os.ReadFile(hookMarker)
					if tc.hookControl {
						if err != nil || strings.TrimSpace(string(marker)) != "PROBE_HOOK_RAN" {
							t.Error("positive hook marker missing before provider request")
						}
					} else if !os.IsNotExist(err) {
						t.Error("hook marker exists or cannot be checked before provider request")
					}
				}
				body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
				if err != nil || r.Method != http.MethodPost || r.URL.Path != "/v1/responses" {
					t.Errorf("unexpected local request: %s %s (%v)", r.Method, r.URL.Path, err)
				}
				select {
				case requests <- body:
				default:
					t.Error("unexpected request retries")
				}
				if tc.success {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, codexProbeCaptureSuccess)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, `{"error":{"type":"invalid_request_error","message":"MOCK_CAPTURE_ONLY"}}`)
			}))
			defer server.Close()
			config := fmt.Sprintf(codexProbeCaptureConfig, filepath.ToSlash(catalog), filepath.ToSlash(instructions), server.URL)
			if tc.isolated {
				config += "\ngoals = false\n\n[skills]\ninclude_instructions = false\n[skills.bundled]\nenabled = false\n"
			}
			if tc.shellControl {
				config = strings.Replace(config, "shell_tool = false", "shell_tool = true", 1)
			}
			if tc.hookSource != "" {
				config = strings.Replace(config, "hooks = false\n", "hooks = true\n", 1)
				if tc.hookSource == "project" {
					// The pinned Windows trust lookup uses the native path spelling.
					config += fmt.Sprintf("\n[projects.%q]\ntrust_level = \"trusted\"\n", cwd)
				}
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
			version.WaitDelay = 2 * time.Second
			version.Env = env
			version.Dir = cwd
			out, err := version.Output()
			if err != nil || strings.TrimSpace(string(out)) != "codex-cli 0.156.1" {
				t.Fatalf("capture contract is pinned to Codex0.156.1: %q (%v)", out, err)
			}
			args := []string{"exec", "--json", "--ephemeral", "--skip-git-repo-check", "--strict-config", "-C", cwd}
			if tc.hookSource != "" {
				// Trust only test-created synthetic hooks for this invocation. Both
				// controls use the bypass so missing trust cannot explain suppression.
				args = append(args, "--dangerously-bypass-hook-trust")
				if !tc.hookControl {
					args = append(args, "-c", "features.hooks=false")
				}
			}
			args = append(args, "Reply OK.")
			cmd := exec.CommandContext(ctx, cli, args...)
			cmd.WaitDelay = 2 * time.Second
			cmd.Env = env
			cmd.Dir = cwd
			out, err = cmd.Output()
			if tc.success {
				if ctx.Err() != nil || err != nil {
					t.Fatalf("native CLI did not complete the synthetic response: %v", err)
				}
				assertCodexProbeCaptureCompletion(t, out)
			} else if ctx.Err() != nil || err == nil || !strings.Contains(string(out), "MOCK_CAPTURE_ONLY") {
				t.Fatalf("expected deliberate local provider rejection, got %v: %s", err, out)
			}
			if len(requests) != 1 {
				t.Fatalf("captured %d requests, want exactly one", len(requests))
			}
			raw := <-requests
			if tc.hookSource != "" {
				_, markerErr := os.Stat(hookMarker)
				if tc.hookControl {
					if markerErr != nil || !strings.Contains(string(raw), "PROBE_HOOK_CONTEXT_6B91") {
						t.Fatal("positive hook control did not run and contribute context")
					}
				} else if !os.IsNotExist(markerErr) || strings.Contains(string(raw), "PROBE_HOOK_CONTEXT_6B91") {
					t.Fatal("disabled hook ran or contributed context")
				}
			}
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
			if tc.hookControl {
				if len(request.Tools) != 0 || len(request.Input) < 2 {
					t.Fatal("hook control did not isolate other tools or add hook input")
				}
			} else if tc.isolated {
				if (len(tc.wantTools) == 0 && len(request.Tools) != 0) || len(request.Input) != 1 || request.Input[0].Role != "user" || len(request.Input[0].Content) != 1 || request.Input[0].Content[0].Text != "Reply OK." {
					t.Fatalf("isolation violated: %d tools, %d input messages", len(request.Tools), len(request.Input))
				}
				if len(tc.wantTools) > 0 {
					toolJSON, _ := json.Marshal(request.Tools)
					for _, name := range tc.wantTools {
						if !strings.Contains(string(toolJSON), `"name":"`+name+`"`) {
							t.Fatalf("negative control did not expose %s", name)
						}
					}
				}
			} else if len(request.Tools) == 0 || !strings.Contains(string(raw), "skills_instructions") {
				t.Fatal("negative control did not expose native goal tools and skill instructions")
			}
		})
	}
}

func assertCodexProbeCaptureCompletion(t *testing.T, output []byte) {
	t.Helper()
	var started, completed, answer bool
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		var event struct {
			Type string `json:"type"`
			Item struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"item"`
		}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatal("invalid native JSON event", err)
		}
		started = started || event.Type == "turn.started"
		completed = completed || event.Type == "turn.completed"
		answer = answer || (event.Type == "item.completed" && event.Item.Type == "agent_message" && event.Item.Text == "OK")
		if event.Type == "turn.failed" || event.Type == "error" {
			t.Fatal("native CLI reported failure for synthetic success")
		}
	}
	if !started || !completed || !answer {
		t.Fatal("exit status alone did not prove native completion and final answer")
	}
}

// Stream shape follows the pinned upstream core/tests/common/responses.rs.
// The local producer is synthetic, not an actual successful model inference.
const codexProbeCaptureSuccess = `event: response.created
data: {"type":"response.created","response":{"id":"resp_probe_fixture"}}

event: response.output_item.done
data: {"type":"response.output_item.done","item":{"type":"message","role":"assistant","id":"msg_probe_fixture","content":[{"type":"output_text","text":"OK"}]}}

event: response.completed
data: {"type":"response.completed","response":{"id":"resp_probe_fixture","usage":{"input_tokens":0,"input_tokens_details":null,"output_tokens":0,"output_tokens_details":null,"total_tokens":0}}}

`

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
