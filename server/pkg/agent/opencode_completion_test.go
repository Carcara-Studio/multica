package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const completionProofFixture = `{"info":{"id":"ses_current"},"messages":[{"id":"msg_user","type":"user","time":{"created":1}},{"id":"msg_current","type":"assistant","finish":"stop","time":{"created":2,"completed":3},"content":[{"type":"text","text":"[redacted]"}]},{"id":"msg_idle","type":"idle","outcome":"succeeded","time":{"created":4}}]}`

func TestOpencodeCompletionProofRejectsUnprovenState(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, raw string
		want      bool
	}{
		{"native successful idle", completionProofFixture, true},
		{"wrong session", strings.Replace(completionProofFixture, "ses_current", "ses_old", 1), false},
		{"stale assistant", strings.Replace(completionProofFixture, "msg_current", "msg_old", 1), false},
		{"failed idle", strings.Replace(completionProofFixture, "succeeded", "failed", 1), false},
		{"interrupted idle", strings.Replace(completionProofFixture, "succeeded", "interrupted", 1), false},
		{"missing completion", strings.Replace(completionProofFixture, `,"completed":3`, "", 1), false},
		{"continuation", strings.Replace(completionProofFixture, `"finish":"stop"`, `"finish":"tool-calls"`, 1), false},
		{"error", strings.Replace(completionProofFixture, `"finish":"stop"`, `"finish":"stop","error":{}`, 1), false},
		{"old idle timestamp", strings.Replace(completionProofFixture, `"created":4`, `"created":1`, 1), false},
		{"later prompt", strings.Replace(completionProofFixture, `"type":"idle"`, `"type":"user"`, 1), false},
		{"missing prompt", strings.Replace(completionProofFixture, `"type":"user"`, `"type":"system"`, 1), false},
		{"pending tool", strings.Replace(completionProofFixture, `"type":"text","text":"[redacted]"`, `"type":"tool","state":{"status":"running"}`, 1), false},
		{"malformed", "{", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := opencodeCompletionProven([]byte(tc.raw), "ses_current", "msg_current"); got != tc.want {
				t.Fatalf("proof = %v, want %v", got, tc.want)
			}
		})
	}
}

func runFakeOpencodeCompletionHelper() {
	if os.Args[1] == "session" {
		if err := os.WriteFile(os.Getenv("COMPLETION_CAPTURE"), []byte(strings.Join(os.Args[1:], "\n")), 0600); err != nil {
			os.Exit(9)
		}
		if os.Getenv("COMPLETION_MODE") == "export-fails" {
			os.Exit(1)
		}
		if os.Getenv("COMPLETION_MODE") == "export-stalls" {
			time.Sleep(time.Minute)
			return
		}
		if os.Getenv("COMPLETION_MODE") == "oversized" {
			fmt.Print(strings.Repeat("x", opencodeProofMaxBytes+1))
			return
		}
		fmt.Print(os.Getenv("COMPLETION_PROOF"))
		return
	}
	_, _ = io.Copy(io.Discard, os.Stdin)
	if os.Getenv("COMPLETION_MODE") == "missing-id" {
		fmt.Println(`{"type":"step_start","sessionID":"ses_current","part":{}}`)
	} else {
		fmt.Println(`{"type":"step_start","sessionID":"ses_current","part":{"messageID":"msg_current"}}`)
	}
	fmt.Println(`{"type":"text","sessionID":"ses_current","part":{"messageID":"msg_current","text":"answer"}}`)
	switch os.Getenv("COMPLETION_MODE") {
	case "mixed-session":
		fmt.Println(`{"type":"text","sessionID":"ses_other","part":{"messageID":"msg_other","text":"other"}}`)
	case "exit-fails":
		os.Exit(1)
	case "explicit-error":
		fmt.Println(`{"type":"error","error":{"name":"UnknownError","data":{"message":"fixture failure"}}}`)
	case "closed-step":
		fmt.Println(`{"type":"step_finish","part":{"reason":"stop"}}`)
	case "cancel":
		time.Sleep(time.Minute)
	}
}

func TestOpencodeV2CompletionProof(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		mode, version, want string
		query               bool
	}{
		{"missing-finish", "2.0.28", "completed", true},
		{"export-fails", "2.0.28", "failed", true},
		{"export-stalls", "2.0.28", "timeout", true},
		{"oversized", "2.0.28", "failed", true},
		{"exit-fails", "2.0.28", "failed", false},
		{"explicit-error", "2.0.28", "failed", false},
		{"closed-step", "2.0.28", "completed", false},
		{"missing-finish", "1.18.31", "failed", false},
		{"managed", "2.0.28", "failed", false},
		{"missing-id", "2.0.28", "failed", false},
		{"mixed-session", "2.0.28", "failed", false},
		{"cancel", "2.0.28", "aborted", false},
	} {
		t.Run(tc.mode+tc.version, func(t *testing.T) {
			dir := t.TempDir()
			capture := filepath.Join(dir, "export-args")
			backend, err := New("opencode", Config{ExecutablePath: os.Args[0], BuiltinRuntime: true, CLIVersion: tc.version, Logger: slog.Default(), Env: map[string]string{
				opencodeStdinHelperEnv: "1", "MULTICA_OPENCODE_COMPLETION_FIXTURE": "1", "COMPLETION_MODE": tc.mode,
				"COMPLETION_CAPTURE": capture, "COMPLETION_PROOF": completionProofFixture, "MULTICA_TOKEN": "mat_fake",
			}})
			if err != nil {
				t.Fatal(err)
			}
			if tc.mode == "managed" {
				delete(backend.(*opencodeBackend).cfg.Env, "MULTICA_TOKEN")
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			session, err := backend.Execute(ctx, "fixture", ExecOptions{Cwd: dir, Timeout: 5 * time.Second})
			if err != nil {
				t.Fatal(err)
			}
			if tc.mode == "cancel" {
				<-session.Messages
				cancel()
			}
			for range session.Messages {
			}
			result := <-session.Result
			if result.Status != tc.want {
				t.Fatalf("status %q error %q, want %q", result.Status, result.Error, tc.want)
			}
			args, err := os.ReadFile(capture)
			if tc.query {
				if err != nil {
					t.Fatal(err)
				}
				if string(args) != "session\nexport\n--sanitize\nses_current\n--standalone" {
					t.Fatalf("wrong export connection: %q", args)
				}
			} else if !os.IsNotExist(err) {
				t.Fatalf("unexpected completion query: %s (%v)", args, err)
			}
		})
	}
}

func TestOpencodeCompletionProofBufferBound(t *testing.T) {
	var b opencodeProofBuffer
	data := []byte(strings.Repeat("x", opencodeProofMaxBytes+10))
	n, err := io.Copy(&b, strings.NewReader(string(data)))
	if err != nil || n != int64(len(data)) || !b.overflow || b.Len() != opencodeProofMaxBytes {
		t.Fatal("unbounded proof output")
	}
	if !json.Valid([]byte(completionProofFixture)) {
		t.Fatal("invalid fixture")
	}
}
