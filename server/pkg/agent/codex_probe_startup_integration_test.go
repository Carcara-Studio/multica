//go:build agentintegration

package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Introspection in a synthetic home is not admission of a real authenticated
// recovery probe, nor a proof about every process or network startup effect.
func TestCodexProbeStartupIntrospection(t *testing.T) {
	if os.Getenv("MULTICA_RUN_REAL_AGENT_SMOKE") != "1" {
		t.Skip("set MULTICA_RUN_REAL_AGENT_SMOKE=1 for native synthetic startup introspection")
	}
	cli := os.Getenv("MULTICA_CODEX_PROBE_CLI")
	if !filepath.IsAbs(cli) {
		t.Fatal("MULTICA_CODEX_PROBE_CLI must name an explicit absolute CLI path")
	}
	root := t.TempDir()
	home, cwd := filepath.Join(root, "home"), filepath.Join(root, "workspace")
	for _, dir := range []string{home, cwd} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path, data string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var contacts atomic.Int64
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		contacts.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer local.Close()
	catalog, instructions := filepath.Join(root, "catalog.json"), filepath.Join(root, "instructions.txt")
	write(catalog, codexProbeCaptureCatalog)
	write(instructions, "Reply OK.")
	config := fmt.Sprintf(codexProbeCaptureConfig, filepath.ToSlash(catalog), filepath.ToSlash(instructions), local.URL)
	config += "\ngoals = false\n[skills]\ninclude_instructions = false\n[skills.bundled]\nenabled = false\n"
	config += fmt.Sprintf("\n[mcp_servers.startup_canary]\nurl = %q\n", local.URL+"/mcp")
	configPath := filepath.Join(home, "config.toml")
	write(configPath, config)
	env := []string{"CODEX_HOME=" + home, "HOME=" + home, "USERPROFILE=" + home, "RUST_LOG=error"}
	for _, key := range []string{"SYSTEMROOT", "WINDIR", "PATH", "TEMP", "TMP", "COMSPEC"} {
		if value := os.Getenv(key); value != "" {
			env = append(env, key+"="+value)
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 35*time.Second)
	defer cancel()
	version := exec.CommandContext(ctx, cli, "--version")
	version.Env, version.Dir = env, cwd
	out, err := version.Output()
	if err != nil || strings.TrimSpace(string(out)) != "codex-cli 0.156.1" {
		t.Fatal("startup contract requires native Codex0.156.1")
	}

	t.Run("effective_features", func(t *testing.T) {
		cmd := exec.CommandContext(ctx, cli, "app-server", "--strict-config")
		cmd.Env, cmd.Dir = env, cwd
		stdin, err := cmd.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		defer func() { _ = stdin.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait() }()
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 4096), 2<<20)
		id := 0
		rpc := func(method string, params any, result any) {
			t.Helper()
			id++
			if err := json.NewEncoder(stdin).Encode(map[string]any{"id": id, "method": method, "params": params}); err != nil {
				t.Fatal(err)
			}
			for scanner.Scan() {
				var msg struct {
					ID     int             `json:"id"`
					Result json.RawMessage `json:"result"`
					Error  json.RawMessage `json:"error"`
				}
				if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
					t.Fatal("invalid native JSON")
				}
				if msg.ID != id {
					continue
				}
				if len(msg.Error) > 0 {
					t.Fatalf("%s returned native error", method)
				}
				if result != nil {
					if err := json.Unmarshal(msg.Result, result); err != nil {
						t.Fatal(err)
					}
				}
				return
			}
			t.Fatalf("native stream ended awaiting %s: %v", method, scanner.Err())
		}
		rpc("initialize", map[string]any{"clientInfo": map[string]string{"name": "multica_probe_fixture", "version": "0.1.0"}, "capabilities": map[string]bool{"experimentalApi": true}}, nil)
		if err := json.NewEncoder(stdin).Encode(map[string]string{"method": "initialized"}); err != nil {
			t.Fatal(err)
		}
		var raw struct {
			Config struct {
				Features map[string]bool `json:"features"`
			} `json:"config"`
		}
		rpc("config/read", map[string]any{"includeLayers": false, "cwd": cwd}, &raw)
		for _, name := range []string{"unified_exec", "shell_tool"} {
			if value, present := raw.Config.Features[name]; !present || value {
				t.Fatal("fixture raw flag missing or unexpectedly enabled")
			}
		}
		var requirements struct {
			Requirements json.RawMessage `json:"requirements"`
		}
		rpc("configRequirements/read", map[string]any{}, &requirements)
		if len(requirements.Requirements) == 0 {
			t.Fatal("requirements response omitted the requirements field")
		}
		// Requirements can be supplied by the host. Never print their payload or
		// certify an unknown managed policy merely because this RPC succeeded.
		features := map[string]bool{}
		cursor := ""
		for page := 0; ; page++ {
			if page >= 64 {
				t.Fatal("feature pagination did not terminate")
			}
			params := map[string]any{"limit": 7}
			if cursor != "" {
				params["cursor"] = cursor
			}
			var reply struct {
				Data []struct {
					Name    string `json:"name"`
					Enabled bool   `json:"enabled"`
				} `json:"data"`
				NextCursor *string `json:"nextCursor"`
			}
			rpc("experimentalFeature/list", params, &reply)
			if len(reply.Data) > 7 {
				t.Fatal("native feature page exceeded requested limit")
			}
			for _, f := range reply.Data {
				if _, ok := features[f.Name]; ok {
					t.Fatal("duplicate paginated feature")
				}
				features[f.Name] = f.Enabled
			}
			if reply.NextCursor == nil {
				break
			}
			if *reply.NextCursor == cursor {
				t.Fatal("feature cursor did not advance")
			}
			cursor = *reply.NextCursor
		}
		for name, want := range map[string]bool{"unified_exec": true, "shell_tool": false, "hooks": false, "plugins": false} {
			got, ok := features[name]
			if !ok || got != want {
				t.Fatalf("effective feature %s: present=%t enabled=%t want=%t", name, ok, got, want)
			}
		}
		if len(features) <= 7 {
			t.Fatal("fixture did not exercise multiple feature pages")
		}
		if contacts.Load() != 0 {
			t.Fatal("synthetic provider/MCP contacted during introspection")
		}
	})
	if t.Failed() {
		return
	}
	t.Run("strict_invalid_config", func(t *testing.T) {
		write(configPath, "unknown_probe_fixture_key = true\n"+config)
		cmd := exec.CommandContext(ctx, cli, "app-server", "--strict-config")
		cmd.Env, cmd.Dir = env, cwd
		output, err := cmd.CombinedOutput()
		if ctx.Err() != nil || err == nil || !strings.Contains(string(output), "unknown_probe_fixture_key") {
			t.Fatal("strict startup did not reject the unknown fixture field")
		}
		if contacts.Load() != 0 {
			t.Fatal("synthetic endpoint contacted after invalid config")
		}
	})
}
