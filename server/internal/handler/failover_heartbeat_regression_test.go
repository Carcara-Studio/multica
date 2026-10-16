package handler

import (
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// TestDaemonHeartbeat_DoesNotSettleRunningTask pins the control-plane boundary
// needed by model failover. A heartbeat proves runtime liveness and may carry
// runtime actions, but it is not evidence of provider quota exhaustion and must
// not settle an in-flight task as a provider failure.
//
// When persisted failover state is introduced, extend this regression to also
// assert that heartbeat leaves that state unchanged.
func TestDaemonHeartbeat_DoesNotSettleRunningTask(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	runtimeID := createRuntimeLocalSkillTestRuntime(t, testUserID)
	agentID := dbfx.Agent(t, "Failover heartbeat isolation agent", runtimeID)
	issueID := dbfx.Issue(t, "failover heartbeat isolation issue")
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": runtimeID,
		"issue_id":   issueID,
		"status":     "running",
		"started_at": testutil.Raw("now()"),
	})

	req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/heartbeat", map[string]any{
		"runtime_id": runtimeID,
	}, testWorkspaceID, "runtime-local-skills-daemon")
	testutil.Call(t, testHandler.DaemonHeartbeat, req).Want(http.StatusOK)

	var status string
	var failureReasonIsNull bool
	dbfx.QueryRow(t, `
		SELECT status, failure_reason IS NULL
		FROM agent_task_queue
		WHERE id = $1
	`, taskID).Scan(&status, &failureReasonIsNull)
	if status != "running" {
		t.Fatalf("heartbeat changed task status to %q, want running", status)
	}
	if !failureReasonIsNull {
		t.Fatal("heartbeat populated failure_reason on an in-flight task")
	}
}
