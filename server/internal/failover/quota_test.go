package failover

import (
	"testing"

	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

func TestReliableQuotaExhaustion(t *testing.T) {
	tests := []struct {
		name, diagnostic string
		want             bool
	}{
		{"balance code", `{"code":"insufficient_balance"}`, true},
		{"billing quota code", `{"code":"insufficient_quota"}`, true},
		{"low balance", "Your balance is too low", true},
		{"monthly budget", "Monthly usage limit reached", true},
		{"daily budget", "Account exceeded the daily usage limit", true},
		{"subscription window", "API Error: 403 You've reached your 5-hour usage limit", true},
		{"subscription auth prefix", "Failed to refresh access token. API Error: 403 You've reached your 5-hour usage limit", true},
		{"zero credits", "Your account has 0 credits remaining", true},
		{"credits", "Provider credits are exhausted", true},
		{"unclassified credit balance", "Credit balance depleted", false},
		{"no credits", "Out of credits", true},
		{"payment", "HTTP 402 Payment Required", true},
		{"generic quota", "quota exceeded", false},
		{"generic credits", "credits unavailable", false},
		{"numeric only", "HTTP 402", false},
		{"minute quota", "quota exceeded per minute", false},
		{"rate limit quota", "rate limit: quota exceeded", false},
		{"credit rate", "credits exhausted; rate_limit exceeded", false},
		{"budget plus transient", "daily usage limit; rate limit exceeded", false},
		{"plural credit rate", "credits exhausted; rate limits exceeded", false},
		{"HTTP rate", "HTTP 429 credits exhausted", false},
		{"HTTP capacity", "HTTP 529 credits exhausted", false},
		{"request rate", "quota: requests per minute exceeded", false},
		{"token rate", "quota: tokens per second exceeded", false},
		{"mixed quota code", "insufficient_quota: too many requests", false},
		{"capacity", "credits exhausted; provider capacity", false},
		{"overload", "credits exhausted; overloaded", false},
		{"auth precedence", "unauthorized: credits exhausted", false},
		{"context precedence", "context_length_exceeded; credits exhausted", false},
		{"network", "connection reset by peer", false},
		{"server", "HTTP 503", false},
		{"timeout", "request timed out", false},
		{"empty", "", false},
		{"unknown model", "model not found", false},
		{"substring balance", "not_insufficient_balance_marker", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ReliableQuotaExhaustion(taskfailure.ReasonAgentProviderQuotaLimit, tt.diagnostic)
			if got != tt.want {
				t.Fatalf("ReliableQuotaExhaustion(%q) = %v, want %v", tt.diagnostic, got, tt.want)
			}
		})
	}
}

func TestReliableQuotaExhaustionRequiresCanonicalQuota(t *testing.T) {
	for _, reason := range taskfailure.AllReasons() {
		if reason == taskfailure.ReasonAgentProviderQuotaLimit {
			continue
		}
		if ReliableQuotaExhaustion(reason, "insufficient_balance") {
			t.Errorf("non-quota reason %q authorized failover", reason)
		}
	}
	for _, reason := range []taskfailure.Reason{"", "agent_error", "unknown"} {
		if ReliableQuotaExhaustion(reason, "insufficient_balance") {
			t.Errorf("unrefined reason %q authorized failover", reason)
		}
	}
}

func TestHistoryQuotaLabelAloneDoesNotAuthorizeRouting(t *testing.T) {
	for _, diagnostic := range []string{"quota exceeded per minute", "rate limit: quota exceeded", "credits unavailable"} {
		reason := taskfailure.Classify(diagnostic)
		if reason != taskfailure.ReasonAgentProviderQuotaLimit {
			t.Fatalf("upstream changed classification of %q to %q; re-evaluate the evidence gate", diagnostic, reason)
		}
		if ReliableQuotaExhaustion(reason, diagnostic) {
			t.Fatalf("ambiguous quota label authorized routing: %q", diagnostic)
		}
	}
}
