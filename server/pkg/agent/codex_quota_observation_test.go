package agent

import (
	"encoding/json"
	"log/slog"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCodexNativeQuotaRequiresFailedCurrentTurn(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, thread, turn, status string
		info                       any
		want                       bool
	}{
		{"native usage", "thr-current", "turn-current", "failed", "usageLimitExceeded", true},
		{"rate limit", "thr-current", "turn-current", "failed", "rateLimitExceeded", false},
		{"message only", "thr-current", "turn-current", "failed", nil, false},
		{"unknown info", "thr-current", "turn-current", "failed", "quota", false},
		{"object info", "thr-current", "turn-current", "failed", map[string]any{"usageLimitExceeded": true}, false},
		{"foreign thread", "thr-other", "turn-current", "failed", "usageLimitExceeded", false},
		{"foreign turn", "thr-current", "turn-old", "failed", "usageLimitExceeded", false},
		{"missing thread", "", "turn-current", "failed", "usageLimitExceeded", false},
		{"missing turn", "thr-current", "", "failed", "usageLimitExceeded", false},
		{"unbounded ID", "thr-current", strings.Repeat("x", 129), "failed", "usageLimitExceeded", false},
		{"arbitrary ID payload", "thr-current", "turn-current\nsecret", "failed", "usageLimitExceeded", false},
		{"successful turn", "thr-current", "turn-current", "completed", "usageLimitExceeded", false},
		{"interrupted turn", "thr-current", "turn-current", "interrupted", "usageLimitExceeded", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _, _ := newTestCodexClient(t)
			c.notificationProtocol = "raw"
			c.setThreadID("thr-current")
			c.setActiveTurnID("turn-current")
			payload, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "turn/completed", "params": map[string]any{
				"threadId": tc.thread, "turn": map[string]any{"id": tc.turn, "status": tc.status,
					"error": map[string]any{"message": "usage limit exceeded; insufficient_quota", "codexErrorInfo": tc.info}},
			}})
			if err != nil {
				t.Fatal(err)
			}
			c.handleLine(string(payload))
			got := c.nativeQuotaObservation("failed", "thr-current")
			if (got != nil) != tc.want {
				t.Fatalf("native quota=%+v, want present=%v", got, tc.want)
			}
			if got != nil {
				if got.Provider != "codex" || got.Code != "usageLimitExceeded" || got.SessionID != "thr-current" || got.TurnID != "turn-current" {
					t.Fatalf("wrong provenance: %+v", got)
				}
				if c.nativeQuotaObservation("completed", "thr-current") != nil || c.nativeQuotaObservation("aborted", "thr-current") != nil || c.nativeQuotaObservation("failed", "thr-other") != nil {
					t.Fatal("observation escaped its final outcome/session")
				}
				got.Code = "mutated"
				if c.nativeQuotaObservation("failed", "thr-current").Code != "usageLimitExceeded" {
					t.Fatal("observation must be returned as a copy")
				}
			}
		})
	}
}

func TestCodexNativeQuotaDoesNotUseAssistantOrUnboundError(t *testing.T) {
	t.Parallel()
	c, _, _ := newTestCodexClient(t)
	c.notificationProtocol = "raw"
	c.setThreadID("thr-current")
	c.setActiveTurnID("turn-current")
	c.handleLine(`{"jsonrpc":"2.0","method":"item/agentMessage/delta","params":{"threadId":"thr-current","turnId":"turn-current","itemId":"msg-1","delta":"usageLimitExceeded insufficient_quota"}}`)
	c.handleLine(`{"jsonrpc":"2.0","method":"error","params":{"threadId":"thr-current","turnId":"turn-current","willRetry":false,"error":{"message":"limit","codexErrorInfo":"usageLimitExceeded"}}}`)
	if c.nativeQuotaObservation("failed", "thr-current") != nil {
		t.Fatal("assistant text or an error without authoritative failed completion is not quota provenance")
	}
	c.setActiveTurnID("")
	c.handleLine(`{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thr-current","turn":{"id":"turn-current","status":"failed","error":{"codexErrorInfo":"usageLimitExceeded"}}}}`)
	if c.nativeQuotaObservation("failed", "thr-current") != nil {
		t.Fatal("completion without a known active turn cannot provide provenance")
	}
}

func TestCodexNativeQuotaPropagatesThroughBackendResult(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("existing fake app-server shell fixture is POSIX-only; protocol cases run on Windows")
	}
	t.Parallel()
	fake := writeFakeCodexAppServer(t, `read line
echo '{"jsonrpc":"2.0","id":1,"result":{}}'
read line
read line
echo '{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"thr-quota"}}}'
read line
echo '{"jsonrpc":"2.0","id":3,"result":{}}'
echo '{"jsonrpc":"2.0","method":"turn/started","params":{"threadId":"thr-quota","turn":{"id":"turn-quota"}}}'
echo '{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thr-quota","turn":{"id":"turn-quota","status":"failed","error":{"message":"native usage exhausted","codexErrorInfo":"usageLimitExceeded"}}}}'
while read line; do :; done
`)
	result, _ := executeFakeCodexCollectingMessagesWithConfig(t, fake,
		Config{Logger: slog.Default(), Env: map[string]string{"CODEX_HOME": t.TempDir()}}, ExecOptions{}, 10*time.Second)
	if result.Status != "failed" || result.Error != "native usage exhausted" || result.NativeQuota == nil {
		t.Fatalf("native quota must survive the failed backend result without replacing display error: %+v", result)
	}
	if result.NativeQuota.SessionID != result.SessionID || result.NativeQuota.TurnID != "turn-quota" {
		t.Fatalf("result provenance mismatched: %+v", result.NativeQuota)
	}
}

func TestCodexRetryErrorAndLateQuotaCannotCreateObservation(t *testing.T) {
	t.Parallel()
	c, _, _ := newTestCodexClient(t)
	c.notificationProtocol = "raw"
	c.setThreadID("thr-current")
	c.setActiveTurnID("turn-current")
	c.handleLine(`{"jsonrpc":"2.0","method":"error","params":{"threadId":"thr-current","turnId":"turn-current","willRetry":true,"error":{"message":"limit","codexErrorInfo":"usageLimitExceeded"}}}`)
	if c.nativeQuotaObservation("failed", "thr-current") != nil {
		t.Fatal("retry notification is not a terminal quota observation")
	}
	c.handleLine(`{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thr-current","turn":{"id":"turn-current","status":"completed"}}}`)
	c.handleLine(`{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thr-current","turn":{"id":"turn-current","status":"failed","error":{"codexErrorInfo":"usageLimitExceeded"}}}}`)
	if c.nativeQuotaObservation("failed", "thr-current") != nil {
		t.Fatal("late duplicate cannot overwrite successful completion")
	}
}
