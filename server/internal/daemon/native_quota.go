package daemon

import (
	"github.com/multica-ai/multica/server/pkg/agent"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

// This is only a no-replay guard, not authority to change an execution target.
// The adapter already bound the native event to its current thread and turn.
func nativeQuotaFailureReason(provider string, result agent.Result) string {
	q := result.NativeQuota
	if provider != "codex" || result.Status != "failed" || q == nil ||
		!q.HasSupportedShape() || q.SessionID != result.SessionID {
		return ""
	}
	return taskfailure.ReasonAgentProviderQuotaLimit.String()
}

func taskNativeQuota(result TaskResult) agent.NativeQuotaObservation {
	q := result.NativeQuota
	if (result.Status == "blocked" || result.Status == "failed") && q.HasSupportedShape() &&
		(q.SessionID == result.SessionID || (result.SessionID == "" && result.SessionRolloutMissing)) {
		return q
	}
	return agent.NativeQuotaObservation{}
}
