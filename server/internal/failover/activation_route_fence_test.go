package failover

import (
	"context"
	"reflect"
	"testing"
	"time"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestQuotaActivationRejectsUnboundOrChangedPrimaryRoute(t *testing.T) {
	for _, mode := range []string{
		"missing proof", "fallback route", "primary generation", "fallback generation",
		"route workspace", "route runtime", "caller refreshed version", "primary rebind",
		"private primary owner", "private fallback owner", "owner membership removed",
		"archived agent", "missing config", "missing fallback runtime", "launch before route", "failure before launch",
	} {
		t.Run(mode, func(t *testing.T) {
			f, c := newTargetFixture(t)
			switch mode {
			case "missing proof":
				f.fx.Exec(t, `DELETE FROM agent_task_execution_route WHERE task_id=$1`, f.task)
			case "fallback route":
				f.fx.Exec(t, `UPDATE agent_task_execution_route SET route='fallback' WHERE task_id=$1`, f.task)
			case "primary generation":
				f.fx.Exec(t, `UPDATE agent_task_execution_route SET primary_generation=$2 WHERE task_id=$1`, f.task, newGeneration())
			case "fallback generation":
				f.fx.Exec(t, `UPDATE agent_fallback_config SET generation=$2 WHERE agent_id=$1`, f.row.AgentID, newGeneration())
			case "route workspace":
				f.fx.Exec(t, `UPDATE agent_task_execution_route SET workspace_id=$2 WHERE task_id=$1`, f.task, newGeneration())
			case "route runtime":
				f.fx.Exec(t, `UPDATE agent_task_execution_route SET runtime_id=$2 WHERE task_id=$1`, f.task, c.RuntimeID)
			case "caller refreshed version":
				f.fx.Exec(t, `UPDATE agent_failover_state SET version=version+1 WHERE agent_id=$1`, f.row.AgentID)
				f.row.Version++
			case "primary rebind":
				f.fx.Exec(t, `UPDATE agent SET runtime_id=$2 WHERE id=$1`, f.row.AgentID, c.RuntimeID)
				f.fx.Cleanup(t, `UPDATE agent SET runtime_id=$2 WHERE id=$1`, f.row.AgentID, f.runtime)
			case "private primary owner":
				other := f.fx.User(t, "Other primary owner", uuidString(newGeneration())+"@example.test")
				f.fx.Exec(t, `UPDATE agent_runtime SET owner_id=$2 WHERE id=$1`, f.runtime, other)
				f.fx.Cleanup(t, `UPDATE agent_runtime SET owner_id=$2 WHERE id=$1`, f.runtime, f.fx.UserID)
			case "private fallback owner":
				other := f.fx.User(t, "Other fallback owner", uuidString(newGeneration())+"@example.test")
				f.fx.Exec(t, `UPDATE agent_runtime SET owner_id=$2 WHERE id=$1`, c.RuntimeID, other)
				f.fx.Cleanup(t, `UPDATE agent_runtime SET owner_id=$2 WHERE id=$1`, c.RuntimeID, f.fx.UserID)
			case "owner membership removed":
				f.fx.Exec(t, `DELETE FROM member WHERE workspace_id=$1 AND user_id=$2`, f.row.WorkspaceID, f.fx.UserID)
			case "archived agent":
				f.fx.Exec(t, `UPDATE agent SET archived_at=clock_timestamp() WHERE id=$1`, f.row.AgentID)
			case "missing config":
				f.fx.Exec(t, `DELETE FROM agent_fallback_config WHERE agent_id=$1`, f.row.AgentID)
			case "missing fallback runtime":
				f.fx.Exec(t, `DELETE FROM agent_runtime WHERE id=$1`, c.RuntimeID)
			case "launch before route":
				f.fx.Exec(t, `UPDATE agent_task_execution_route SET created_at=(SELECT started_at+interval '1 second' FROM agent_task_queue WHERE id=$1) WHERE task_id=$1`, f.task)
			case "failure before launch":
				f.fx.Exec(t, `UPDATE agent_task_queue SET completed_at=started_at-interval '1 second' WHERE id=$1`, f.task)
			}
			before, err := f.q.GetAgentFailoverState(t.Context(), db.GetAgentFailoverStateParams{AgentID: f.row.AgentID, WorkspaceID: f.row.WorkspaceID})
			if err != nil {
				t.Fatal(err)
			}
			_, err = f.activate(t.Context(), f.q)
			wantNoTransition(t, err)
			after, err := f.q.GetAgentFailoverState(t.Context(), db.GetAgentFailoverStateParams{AgentID: f.row.AgentID, WorkspaceID: f.row.WorkspaceID})
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatalf("rejected quota changed availability: %v", err)
			}
		})
	}
}

func TestRecoveredPrimaryRejectsOldQuotaEvenWithCurrentCASVersion(t *testing.T) {
	f, c := newTargetFixture(t)
	originalTask := f.task
	p := f.probe(t)
	if _, err := f.dispatch(t.Context(), f.q, p); err != nil {
		t.Fatal(err)
	}
	recovered, err := f.settle(t.Context(), f.q, p, "success") // fixture outcome, no real inference
	if err != nil {
		t.Fatal(err)
	}
	f.row = recovered // adversarial caller substitutes the latest CAS version
	_, err = f.activate(t.Context(), f.q)
	wantNoTransition(t, err)
	current, err := f.q.GetAgentFailoverState(t.Context(), db.GetAgentFailoverStateParams{AgentID: recovered.AgentID, WorkspaceID: recovered.WorkspaceID})
	if err != nil || !reflect.DeepEqual(current, recovered) {
		t.Fatalf("old quota reactivated fallback: %v", err)
	}

	// A genuinely new routed primary run may fail after recovery. Do not rewrite
	// the old proof or replay the old task to manufacture that evidence.
	f.task = targetTask(t, f, f.runtime)
	if _, err := recordRoute(t.Context(), f.q, f, c, f.task, "primary"); err != nil {
		t.Fatal(err)
	}
	f.fx.Exec(t, `UPDATE agent_task_queue SET status='running',started_at=clock_timestamp() WHERE id=$1`, f.task)
	f.fx.Exec(t, `UPDATE agent_task_queue SET status='failed',completed_at=clock_timestamp(),failure_reason='agent_error.provider_quota_limit' WHERE id=$1`, f.task)
	activated, err := f.activate(t.Context(), f.q)
	if err != nil || activated.State != "fallback_active" || activated.PrimaryFailureTaskID != f.task {
		t.Fatalf("fresh routed failure rejected: %v", err)
	}
	old, err := f.q.GetAgentTaskExecutionRoute(t.Context(), db.GetAgentTaskExecutionRouteParams{TaskID: originalTask, WorkspaceID: f.row.WorkspaceID})
	if err != nil || old.StateVersion >= recovered.Version {
		t.Fatal("old route proof was rewritten", err)
	}
}

func TestQuotaActivationDoesNotSubstituteExhaustedPrimaryForOfflineFallback(t *testing.T) {
	f, c := newTargetFixture(t)
	f.fx.Exec(t, `UPDATE agent_runtime SET status='offline' WHERE id=$1`, c.RuntimeID)
	row, err := f.activate(t.Context(), f.q)
	if err != nil || row.State != "fallback_active" {
		t.Fatalf("offline fallback changed quota policy: %v", err)
	}
}

func TestQuotaActivationRechecksConfigAfterLockWait(t *testing.T) {
	f, _ := newTargetFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	blocker, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(context.Background())
	if _, err := blocker.Exec(ctx, `SELECT agent_id FROM agent_fallback_config WHERE agent_id=$1 FOR UPDATE`, f.row.AgentID); err != nil {
		t.Fatal(err)
	}
	conn, err := f.pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	result, done := make(chan error, 1), make(chan struct{})
	defer func() { cancel(); <-done; conn.Release() }()
	go func() {
		defer close(done)
		_, err := f.activate(ctx, db.New(conn))
		result <- err
	}()
	for {
		var waiting bool
		if err := f.pool.QueryRow(ctx, `SELECT COALESCE(wait_event_type='Lock',false) FROM pg_stat_activity WHERE pid=$1`, int32(conn.Conn().PgConn().PID())).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case err := <-result:
			t.Fatalf("activation did not wait for configuration lock: %v", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(5 * time.Millisecond):
		}
	}
	if _, err := blocker.Exec(ctx, `UPDATE agent_fallback_config SET generation=$2 WHERE agent_id=$1`, f.row.AgentID, newGeneration()); err != nil {
		t.Fatal(err)
	}
	if err := blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	wantNoTransition(t, <-result)
	state, err := f.q.GetAgentFailoverState(ctx, db.GetAgentFailoverStateParams{AgentID: f.row.AgentID, WorkspaceID: f.row.WorkspaceID})
	if err != nil || !reflect.DeepEqual(state, f.row) {
		t.Fatal("configuration race changed availability", err)
	}
}
