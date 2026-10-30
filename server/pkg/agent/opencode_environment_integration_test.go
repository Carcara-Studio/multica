//go:build agentintegration

package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOpencodeNativeSessionEnvironmentSmoke(t *testing.T) {
	requireRealAgentSmoke(t)
	executable := os.Getenv("MULTICA_OPENCODE_PATH")
	if executable == "" {
		t.Skip("set MULTICA_OPENCODE_PATH to the explicit OpenCode executable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	version, err := exec.CommandContext(ctx, executable, "--version").Output()
	if err != nil {
		t.Fatal(err)
	}
	if !opencodeForwardsSessionEnvironment(Config{CLIVersion: string(version), BuiltinRuntime: true}) {
		t.Skip("requires the verified native per-session environment contract")
	}
	root := t.TempDir()
	configDir := filepath.Join(root, ".opencode")
	if err := os.Mkdir(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	model := "opencode/deepseek-v4.1-flash-free"
	agents := map[string]any{}
	for _, name := range []string{"build", "plan", "general", "explore", "title", "summary", "compaction"} {
		agents[name] = map[string]string{"model": model}
	}
	config, _ := json.Marshal(map[string]any{
		"model": model, "agents": agents,
		"policies": []map[string]string{
			{"action": "provider.use", "resource": "*", "effect": "deny"},
			{"action": "provider.use", "resource": "opencode", "effect": "allow"},
			{"action": "integration.use", "resource": "skill:*", "effect": "deny"},
			{"action": "tool.use", "resource": "subagent:*", "effect": "deny"},
		},
	})
	if err := os.WriteFile(filepath.Join(configDir, "opencode.json"), config, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"native-environment-a", "native-environment-b"} {
		backend, err := New("opencode", Config{
			ExecutablePath: executable, CLIVersion: string(version), BuiltinRuntime: true, Logger: slog.Default(),
			Env: map[string]string{
				"MULTICA_TOKEN": "mat_fake_native_environment_smoke", "MULTICA_NATIVE_ENV_PROBE": marker,
				"OPENAI_API_KEY": "", "NVIDIA_API_KEY": "", "OPENROUTER_API_KEY": "", "ANTHROPIC_API_KEY": "", "OPENCODE_API_KEY": "",
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		session, err := backend.Execute(ctx, "Use exactly one shell tool to print only the environment variable MULTICA_NATIVE_ENV_PROBE. Do not read any other variable or file, change files, call Multica, or use subagents. Return the output.", ExecOptions{Cwd: root, Model: model, Timeout: time.Minute})
		if err != nil {
			t.Fatal(err)
		}
		seen := false
		for message := range session.Messages {
			if message.Type == MessageToolResult && strings.Contains(message.Output, marker) {
				seen = true
			}
		}
		result := <-session.Result
		if result.Status != "completed" || !seen {
			t.Fatalf("native environment not observed through backend tool output: status=%s seen=%v error=%s", result.Status, seen, result.Error)
		}
	}
}
