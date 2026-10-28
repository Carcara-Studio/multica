package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/agent"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

func TestNativeQuotaOptionalMetadataDoesNotBreakFailureDecoding(t *testing.T) {
	for _, payload := range []string{`null`, `42`, `"not an observation"`, `[]`, `{"code":42}`, `{"provider":"codex","code":"rateLimitExceeded"}`, `{"padding":"` + strings.Repeat("x", 1025) + `"}`} {
		var req TaskFailRequest
		if err := json.Unmarshal([]byte(`{"error":"original","native_quota":`+payload+`,"nativeQuota":{"daemonID":"forged"}}`), &req); err != nil {
			t.Fatalf("optional metadata prevented ordinary failure decoding: %v", err)
		}
		if _, ok := decodeNativeQuotaReport(req); ok || req.nativeQuota != nil || req.Error != "original" {
			t.Fatal("malformed observation gained authority or changed ordinary failure")
		}
	}
}

func TestNativeQuotaTransportRequiresAuthenticatedExecutor(t *testing.T) {
	runtimeID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	workspaceID := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}
	ws := uuidToString(workspaceID)
	q := agent.NativeQuotaObservation{Provider: "codex", Code: "usageLimitExceeded", SessionID: "thread-1", TurnID: "turn-1"}
	for _, tc := range []struct {
		name, daemon, workspace string
		mutate                  func(*db.AgentTaskQueue, *db.AgentRuntime)
		want                    bool
	}{
		{"authenticated daemon", "daemon-1", ws, nil, true},
		{"workspace user token is not executor identity", "", "", nil, false},
		{"foreign daemon", "daemon-other", ws, nil, false},
		{"foreign workspace", "daemon-1", "other", nil, false},
		{"foreign task runtime", "daemon-1", ws, func(task *db.AgentTaskQueue, _ *db.AgentRuntime) { task.RuntimeID.Bytes[1] = 3 }, false},
		{"foreign runtime workspace", "daemon-1", ws, func(_ *db.AgentTaskQueue, rt *db.AgentRuntime) { rt.WorkspaceID.Bytes[1] = 3 }, false},
		{"other provider", "daemon-1", ws, func(_ *db.AgentTaskQueue, rt *db.AgentRuntime) { rt.Provider = "opencode" }, false},
		{"already terminal", "daemon-1", ws, func(task *db.AgentTaskQueue, _ *db.AgentRuntime) { task.Status = "failed" }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			task := db.AgentTaskQueue{ID: pgtype.UUID{Bytes: [16]byte{3}, Valid: true}, RuntimeID: runtimeID, Status: "running"}
			rt := db.AgentRuntime{ID: runtimeID, WorkspaceID: workspaceID, Provider: "codex", DaemonID: pgtype.Text{String: "daemon-1", Valid: true}}
			if tc.mutate != nil {
				tc.mutate(&task, &rt)
			}
			r := httptest.NewRequest("POST", "/", strings.NewReader(`{"daemon_id":"daemon-1"}`))
			r.Header.Set("X-Daemon-ID", "daemon-1") // body/headers cannot grant identity
			if tc.daemon != "" {
				r = r.WithContext(middleware.WithDaemonContext(context.Background(), tc.workspace, tc.daemon))
			}
			got := correlateNativeQuotaTransport(r, task, rt, ws, q)
			if (got != nil) != tc.want {
				t.Fatalf("correlated=%v, want %v", got != nil, tc.want)
			}
		})
	}
}

func TestNativeQuotaFailureCallbackPreservesTerminalNoReplay(t *testing.T) {
	if testPool == nil || testHandler == nil {
		t.Skip("database not available")
	}
	runtimeID := dbfx.Runtime(t, "Quota transport", testutil.Cols{"daemon_id": "quota-daemon", "provider": "codex"})
	agentID := dbfx.Agent(t, "Quota transport", runtimeID)
	issueID := dbfx.Issue(t, "Quota transport")
	taskID := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "issue_id": issueID, "status": "running", "attempt": 1, "max_attempts": 2})
	q := agent.NativeQuotaObservation{Provider: "codex", Code: "usageLimitExceeded", SessionID: "thread-1", TurnID: "turn-1"}
	req := withURLParam(newRequest(http.MethodPost, "/api/daemon/tasks/"+taskID+"/fail", map[string]any{
		"error": "connection closed mid-response", "failure_reason": "agent_error.provider_network",
		"session_id": "thread-1", "native_quota": q,
	}), "taskId", taskID)
	req = req.WithContext(middleware.WithDaemonContext(req.Context(), testWorkspaceID, "quota-daemon"))
	testutil.Call(t, testHandler.FailTask, req).Want(http.StatusOK)
	task, err := testHandler.Queries.GetAgentTask(t.Context(), parseUUID(taskID))
	if err != nil || task.Status != "failed" || task.FailureReason.String != taskfailure.ReasonAgentProviderQuotaLimit.String() || task.Error.String != "connection closed mid-response" {
		t.Fatalf("terminal failure=%+v, error=%v", task, err)
	}
	var retries, availability int
	dbfx.QueryRow(t, `SELECT count(*) FROM agent_task_queue WHERE parent_task_id=$1`, taskID).Scan(&retries)
	dbfx.QueryRow(t, `SELECT count(*) FROM agent_failover_state WHERE agent_id=$1`, agentID).Scan(&availability)
	if retries != 0 || availability != 0 {
		t.Fatalf("native diagnostic created retries=%d or availability=%d", retries, availability)
	}
}

func TestNativeQuotaReportSessionBinding(t *testing.T) {
	q := agent.NativeQuotaObservation{Provider: "codex", Code: "usageLimitExceeded", SessionID: "thread-1", TurnID: "turn-1"}
	raw, _ := json.Marshal(q)
	for _, tc := range []struct {
		session string
		missing bool
		want    bool
	}{{"thread-1", false, true}, {"", true, true}, {"", false, false}, {"foreign", true, false}} {
		_, ok := decodeNativeQuotaReport(TaskFailRequest{NativeQuota: raw, SessionID: tc.session, SessionRolloutMissing: tc.missing})
		if ok != tc.want {
			t.Fatalf("session=%q, missing=%v: correlated=%v", tc.session, tc.missing, ok)
		}
	}
}
