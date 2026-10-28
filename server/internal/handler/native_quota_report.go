package handler

import (
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/pkg/agent"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// This snapshot correlates a diagnostic with an authenticated daemon transport.
// It is deliberately not an availability capability or accepted route evidence.
type nativeQuotaDiagnostic struct {
	observation agent.NativeQuotaObservation
	taskID      pgtype.UUID
	runtimeID   pgtype.UUID
	daemonID    string
}

func decodeNativeQuotaReport(req TaskFailRequest) (agent.NativeQuotaObservation, bool) {
	var q agent.NativeQuotaObservation
	if len(req.NativeQuota) == 0 || len(req.NativeQuota) > 1024 || json.Unmarshal(req.NativeQuota, &q) != nil || !q.HasSupportedShape() {
		return agent.NativeQuotaObservation{}, false
	}
	return q, q.SessionID == req.SessionID || (req.SessionID == "" && req.SessionRolloutMissing)
}

func (h *Handler) nativeQuotaDiagnostic(r *http.Request, task db.AgentTaskQueue, workspaceID string, req TaskFailRequest) *nativeQuotaDiagnostic {
	q, ok := decodeNativeQuotaReport(req)
	if !ok || task.Status != "running" || middleware.DaemonIDFromContext(r.Context()) == "" || middleware.DaemonWorkspaceIDFromContext(r.Context()) != workspaceID {
		return nil
	}
	runtime, err := h.Queries.GetAgentRuntime(r.Context(), task.RuntimeID)
	if err != nil {
		// Optional diagnostic metadata cannot hold the ordinary terminal callback
		// hostage. An unavailable source remains inconclusive, never authoritative.
		return nil
	}
	return correlateNativeQuotaTransport(r, task, runtime, workspaceID, q)
}

func correlateNativeQuotaTransport(r *http.Request, task db.AgentTaskQueue, runtime db.AgentRuntime, workspaceID string, q agent.NativeQuotaObservation) *nativeQuotaDiagnostic {
	daemonID := middleware.DaemonIDFromContext(r.Context())
	if !q.HasSupportedShape() || task.Status != "running" || !task.ID.Valid || !task.RuntimeID.Valid || task.RuntimeID != runtime.ID || !runtime.WorkspaceID.Valid || uuidToString(runtime.WorkspaceID) != workspaceID ||
		runtime.Provider != "codex" || !runtime.DaemonID.Valid || daemonID == "" || runtime.DaemonID.String != daemonID || middleware.DaemonWorkspaceIDFromContext(r.Context()) != workspaceID {
		return nil
	}
	return &nativeQuotaDiagnostic{observation: q, taskID: task.ID, runtimeID: runtime.ID, daemonID: daemonID}
}
