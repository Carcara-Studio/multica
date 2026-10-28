package daemon

import (
	"testing"

	"github.com/multica-ai/multica/server/pkg/agent"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

func TestNativeQuotaPreventsFreshSessionReplay(t *testing.T) {
	for _, tc := range []struct {
		diagnostic string
		rejected   bool
	}{
		{"connection closed mid-response", true},
		{"could not resolve authentication method", false},
		{"messages.0 content must be non-empty", false},
	} {
		t.Run(tc.diagnostic, func(t *testing.T) {
			diagnostic := tc.diagnostic
			result := agent.Result{Status: "failed", SessionID: "thread-1", Error: diagnostic, ResumeRejected: tc.rejected}
			if !shouldRetryWithFreshSession(result, "prior-thread", 0, "codex") {
				t.Fatal("fixture must reach the existing fresh-session retry")
			}
			result.NativeQuota = &agent.NativeQuotaObservation{Provider: "codex", Code: "usageLimitExceeded", SessionID: "thread-1", TurnID: "turn-1"}
			if shouldRetryWithFreshSession(result, "prior-thread", 0, "codex") {
				t.Fatal("native quota must veto replay even with a conflicting resume flag")
			}
			if got := nativeQuotaFailureReason("codex", result); got != taskfailure.ReasonAgentProviderQuotaLimit.String() {
				t.Fatalf("failure reason = %q", got)
			}
			if result.Error != diagnostic {
				t.Fatal("display diagnostic changed")
			}
		})
	}
}

func TestNativeQuotaNoReplayGuardRejectsUnboundObservations(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*agent.Result, *string)
	}{
		{"absent", func(r *agent.Result, _ *string) { r.NativeQuota = nil }},
		{"provider", func(_ *agent.Result, p *string) { *p = "opencode" }},
		{"source", func(r *agent.Result, _ *string) { r.NativeQuota.Provider = "opencode" }},
		{"rate limit", func(r *agent.Result, _ *string) { r.NativeQuota.Code = "rateLimitExceeded" }},
		{"foreign session", func(r *agent.Result, _ *string) { r.NativeQuota.SessionID = "other" }},
		{"missing session", func(r *agent.Result, _ *string) { r.SessionID = ""; r.NativeQuota.SessionID = "" }},
		{"missing turn", func(r *agent.Result, _ *string) { r.NativeQuota.TurnID = "" }},
		{"completed", func(r *agent.Result, _ *string) { r.Status = "completed" }},
		{"cancelled", func(r *agent.Result, _ *string) { r.Status = "cancelled" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := "codex"
			result := agent.Result{Status: "failed", SessionID: "thread-1", NativeQuota: &agent.NativeQuotaObservation{
				Provider: "codex", Code: "usageLimitExceeded", SessionID: "thread-1", TurnID: "turn-1",
			}}
			tc.change(&result, &provider)
			if got := nativeQuotaFailureReason(provider, result); got != "" {
				t.Fatalf("unbound observation supplied reason %q", got)
			}
		})
	}
}
