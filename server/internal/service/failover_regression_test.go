package service

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

// TestFailTaskProviderQuotaIsTerminalAndDoesNotRetry pins the failover boundary:
// quota exhaustion may drive future execution routing, but settling the current
// task must remain terminal and must not implicitly replay it.
func TestFailTaskProviderQuotaIsTerminalAndDoesNotRetry(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, _ := seedAttributionFixture(t, pool)

	var runtimeID string
	if err := pool.QueryRow(ctx, `SELECT runtime_id::text FROM agent WHERE id = $1`, agentID).Scan(&runtimeID); err != nil {
		t.Fatalf("load agent runtime: %v", err)
	}

	var chatSessionID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id)
		VALUES ($1, $2, $3) RETURNING id
	`, workspaceID, agentID, userID).Scan(&chatSessionID); err != nil {
		t.Fatalf("seed chat session: %v", err)
	}

	var taskID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue
			(agent_id, runtime_id, chat_session_id, status, priority, attempt, max_attempts)
		VALUES ($1, $2, $3, 'running', 0, 1, 2)
		RETURNING id
	`, agentID, runtimeID, chatSessionID).Scan(&taskID); err != nil {
		t.Fatalf("seed running task: %v", err)
	}
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM agent_task_queue WHERE chat_session_id = $1`, chatSessionID)
		_, _ = pool.Exec(c, `DELETE FROM chat_message WHERE chat_session_id = $1`, chatSessionID)
		_, _ = pool.Exec(c, `DELETE FROM chat_session WHERE id = $1`, chatSessionID)
	})

	bus := events.New()
	var got events.Event
	bus.Subscribe(protocol.EventTaskFailed, func(e events.Event) { got = e })
	svc := &TaskService{Queries: q, TxStarter: pool, Bus: bus}
	quotaReason := taskfailure.ReasonAgentProviderQuotaLimit.String()

	if _, err := svc.FailTask(
		ctx,
		util.MustParseUUID(taskID),
		"provider reports insufficient_balance",
		"", "", "",
		quotaReason,
		false, "", "",
	); err != nil {
		t.Fatalf("FailTask: %v", err)
	}

	var status, failureReason string
	if err := pool.QueryRow(ctx, `
		SELECT status, failure_reason
		FROM agent_task_queue
		WHERE id = $1
	`, taskID).Scan(&status, &failureReason); err != nil {
		t.Fatalf("load failed task: %v", err)
	}
	if status != "failed" {
		t.Fatalf("task status = %q, want failed", status)
	}
	if failureReason != quotaReason {
		t.Fatalf("failure_reason = %q, want %q", failureReason, quotaReason)
	}

	var retryCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM agent_task_queue
		WHERE retry_of_task_id = $1
	`, taskID).Scan(&retryCount); err != nil {
		t.Fatalf("count retry children: %v", err)
	}
	if retryCount != 0 {
		t.Fatalf("quota failure created %d implicit retry child(ren), want 0", retryCount)
	}

	payload, ok := got.Payload.(map[string]any)
	if !ok {
		t.Fatalf("task failed payload type = %T, want map[string]any", got.Payload)
	}
	if payload["failure_reason"] != quotaReason {
		t.Fatalf("event failure_reason = %v, want %s", payload["failure_reason"], quotaReason)
	}
	if payload["retry_pending"] != false {
		t.Fatalf("event retry_pending = %v, want false", payload["retry_pending"])
	}
}

// TestFailTaskAutomaticRetryPreservesRuntimeOwnership pins the other side of
// the same contract. When an already-authorized retryable failure creates a
// retry child, that child inherits the parent's persisted runtime ownership.
// A later PRIMARY/FALLBACK route marker must follow the same inheritance rule.
func TestFailTaskAutomaticRetryPreservesRuntimeOwnership(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, _ := seedAttributionFixture(t, pool)

	var runtimeID string
	if err := pool.QueryRow(ctx, `SELECT runtime_id::text FROM agent WHERE id = $1`, agentID).Scan(&runtimeID); err != nil {
		t.Fatalf("load agent runtime: %v", err)
	}

	var chatSessionID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id)
		VALUES ($1, $2, $3) RETURNING id
	`, workspaceID, agentID, userID).Scan(&chatSessionID); err != nil {
		t.Fatalf("seed chat session: %v", err)
	}

	var taskID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue
			(agent_id, runtime_id, chat_session_id, status, priority, attempt, max_attempts)
		VALUES ($1, $2, $3, 'running', 0, 1, 2)
		RETURNING id
	`, agentID, runtimeID, chatSessionID).Scan(&taskID); err != nil {
		t.Fatalf("seed running task: %v", err)
	}
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM agent_task_queue WHERE chat_session_id = $1`, chatSessionID)
		_, _ = pool.Exec(c, `DELETE FROM chat_message WHERE chat_session_id = $1`, chatSessionID)
		_, _ = pool.Exec(c, `DELETE FROM chat_session WHERE id = $1`, chatSessionID)
	})

	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}
	if _, err := svc.FailTask(
		ctx,
		util.MustParseUUID(taskID),
		"provider stream disconnected",
		"", "", "",
		taskfailure.ReasonAgentProviderNetwork.String(),
		false, "", "",
	); err != nil {
		t.Fatalf("FailTask: %v", err)
	}

	var childRuntimeID, retryOfTaskID string
	if err := pool.QueryRow(ctx, `
		SELECT runtime_id::text, retry_of_task_id::text
		FROM agent_task_queue
		WHERE retry_of_task_id = $1
	`, taskID).Scan(&childRuntimeID, &retryOfTaskID); err != nil {
		t.Fatalf("load automatic retry child: %v", err)
	}
	if retryOfTaskID != taskID {
		t.Fatalf("retry_of_task_id = %s, want parent %s", retryOfTaskID, taskID)
	}
	if childRuntimeID != runtimeID {
		t.Fatalf("retry runtime_id = %s, want parent runtime %s", childRuntimeID, runtimeID)
	}
}
