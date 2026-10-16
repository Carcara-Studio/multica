package handler

import (
	"context"
	"testing"
)

// TestClaimTaskByRuntime_DoesNotFollowAgentRuntimeRebind pins the execution
// ownership invariant required by fork failover work: a queued task belongs to
// the runtime stamped on the task row at enqueue time. Moving the agent to a
// different runtime affects future work only; it must not make already-queued
// work claimable by the new runtime.
func TestClaimTaskByRuntime_DoesNotFollowAgentRuntimeRebind(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	originalRuntimeID := createClaimReclaimRuntime(t, ctx, "Failover original runtime")
	newRuntimeID := createClaimReclaimRuntime(t, ctx, "Failover new runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, originalRuntimeID, "Failover runtime ownership agent")

	var taskID string
	dbfx.QueryRow(t, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
		VALUES ($1, $2, $3, 'queued', 0)
		RETURNING id
	`, agentID, originalRuntimeID, issueID).Scan(&taskID)
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})

	// Simulate a configuration/failover rebind after the task already exists.
	dbfx.Exec(t, `UPDATE agent SET runtime_id = $2, updated_at = now() WHERE id = $1`, agentID, newRuntimeID)

	claimed, body := claimTaskByRuntimeForTest(t, newRuntimeID)
	if claimed != nil {
		t.Fatalf("new runtime claimed pre-rebind task %s as %s: %s", taskID, claimed.ID, body)
	}

	var persistedRuntimeID, status string
	dbfx.QueryRow(t, `
		SELECT runtime_id::text, status
		FROM agent_task_queue
		WHERE id = $1
	`, taskID).Scan(&persistedRuntimeID, &status)
	if persistedRuntimeID != originalRuntimeID {
		t.Fatalf("task runtime_id = %s, want original runtime %s", persistedRuntimeID, originalRuntimeID)
	}
	if status != "queued" {
		t.Fatalf("task status = %s, want queued after rejected claim", status)
	}
}
