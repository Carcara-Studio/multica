package agent

// NativeQuotaObservation preserves a native terminal usage-exhaustion signal.
// It is not account identity, a no-side-effects assertion, or permission to
// retry/route work. Callers must still bind the result to their authenticated
// run and current target/configuration fences (ADR-038).
type NativeQuotaObservation struct {
	Provider  string
	Code      string
	SessionID string
	TurnID    string
}

func (c *codexClient) observeNativeQuotaCompletion(params map[string]any) {
	if extractNestedString(params, "turn", "status") != "failed" ||
		extractNestedString(params, "turn", "error", "codexErrorInfo") != "usageLimitExceeded" {
		return
	}
	threadID, _ := params["threadId"].(string)
	turnID := extractNestedString(params, "turn", "id")
	if !boundedCodexObservationID(threadID) || !boundedCodexObservationID(turnID) ||
		threadID != c.getThreadID() || turnID != c.activeTurnID() {
		return
	}
	c.turnErrorMu.Lock()
	defer c.turnErrorMu.Unlock()
	c.nativeQuota = &NativeQuotaObservation{Provider: "codex", Code: "usageLimitExceeded", SessionID: threadID, TurnID: turnID}
}

func (c *codexClient) nativeQuotaObservation(finalStatus, sessionID string) *NativeQuotaObservation {
	c.turnErrorMu.Lock()
	defer c.turnErrorMu.Unlock()
	if finalStatus != "failed" || c.nativeQuota == nil || sessionID != c.nativeQuota.SessionID {
		return nil
	}
	copy := *c.nativeQuota
	return &copy
}

// Decline unexpected ID shapes rather than retaining arbitrary provider payloads.
func boundedCodexObservationID(id string) bool {
	if len(id) == 0 || len(id) > 128 {
		return false
	}
	for _, ch := range id {
		if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') ||
			(ch >= '0' && ch <= '9') || ch == '-' || ch == '_') {
			return false
		}
	}
	return true
}
