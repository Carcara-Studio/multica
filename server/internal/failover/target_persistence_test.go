package failover

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func newTargetFixture(t *testing.T) (*stateFixture, db.AgentFallbackConfig) {
	t.Helper()
	f := newStateFixture(t)
	fallback := testUUID(f.fx.Runtime(t, "Fallback"))
	c, err := f.q.InitializeAgentFallbackConfig(t.Context(), db.InitializeAgentFallbackConfigParams{
		AgentID: f.row.AgentID, WorkspaceID: f.row.WorkspaceID, RuntimeID: fallback,
		Generation: newGeneration(), Model: "fallback-model", ThinkingLevel: "", ServiceTier: "",
	})
	if err != nil {
		t.Fatal(err)
	}
	f.fx.Cleanup(t, `DELETE FROM agent_fallback_config WHERE agent_id=$1`, f.row.AgentID)
	f.fx.Cleanup(t, `DELETE FROM agent_task_execution_route WHERE agent_id=$1`, f.row.AgentID)
	return f, c
}

func targetTask(t *testing.T, f *stateFixture, runtime pgtype.UUID) pgtype.UUID {
	t.Helper()
	return testUUID(f.fx.Task(t, uuidString(f.row.AgentID), testutil.Cols{"runtime_id": uuidString(runtime), "status": "queued"}))
}

func uuidString(v pgtype.UUID) string { return uuid.UUID(v.Bytes).String() }

func recordRoute(ctx context.Context, q *db.Queries, f *stateFixture, c db.AgentFallbackConfig, task pgtype.UUID, route string) (db.AgentTaskExecutionRoute, error) {
	return q.RecordAgentTaskExecutionRoute(ctx, db.RecordAgentTaskExecutionRouteParams{
		AgentID: f.row.AgentID, WorkspaceID: f.row.WorkspaceID, FallbackGeneration: c.Generation,
		PrimaryGeneration: f.row.PrimaryGeneration, StateVersion: f.row.Version, TaskID: task, Route: route,
	})
}

func TestFallbackConfigOwnershipAndConcurrentCAS(t *testing.T) {
	f, c := newTargetFixture(t)
	ctx := t.Context()
	_, err := f.q.GetAgentFallbackConfig(ctx, db.GetAgentFallbackConfigParams{AgentID: f.row.AgentID, WorkspaceID: newGeneration()})
	wantNoTransition(t, err)
	otherUser := f.fx.User(t, "Foreign runtime owner", "foreign-runtime-owner@example.test")
	foreignRuntime := testUUID(f.fx.Runtime(t, "Private foreign runtime", testutil.Cols{"owner_id": otherUser, "visibility": "private"}))
	p := db.UpdateAgentFallbackConfigParams{AgentID: f.row.AgentID, WorkspaceID: f.row.WorkspaceID, ExpectedGeneration: c.Generation, ExpectedPrimaryGeneration: f.row.PrimaryGeneration, ExpectedVersion: f.row.Version, NewPrimaryGeneration: newGeneration(), NewGeneration: newGeneration(), RuntimeID: foreignRuntime, Model: "model"}
	_, err = f.q.UpdateAgentFallbackConfig(ctx, p)
	wantNoTransition(t, err)
	foreignWs := f.fx.Workspace(t, "Foreign target workspace", "foreign-target-"+uuidString(newGeneration()))
	p.RuntimeID = testUUID(f.fx.Runtime(t, "Foreign workspace runtime", testutil.Cols{"workspace_id": foreignWs, "visibility": "public"}))
	_, err = f.q.UpdateAgentFallbackConfig(ctx, p)
	wantNoTransition(t, err)
	p.RuntimeID = c.RuntimeID
	p.NewGeneration = c.Generation
	_, err = f.q.UpdateAgentFallbackConfig(ctx, p)
	wantNoTransition(t, err)
	var winners atomic.Int32
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			args := p
			args.NewGeneration = newGeneration()
			_, err := f.q.UpdateAgentFallbackConfig(ctx, args)
			if err == nil {
				winners.Add(1)
			} else if !errors.Is(err, pgx.ErrNoRows) {
				t.Errorf("CAS: %v", err)
			}
		})
	}
	wg.Wait()
	if winners.Load() != 1 {
		t.Fatalf("CAS winners=%d", winners.Load())
	}
	n, err := f.q.DeleteAgentFallbackConfig(ctx, db.DeleteAgentFallbackConfigParams{AgentID: f.row.AgentID, WorkspaceID: f.row.WorkspaceID, ExpectedGeneration: c.Generation, ExpectedPrimaryGeneration: f.row.PrimaryGeneration, ExpectedVersion: f.row.Version, NewPrimaryGeneration: newGeneration()})
	if err != nil || n != 0 {
		t.Fatalf("stale delete=%d %v", n, err)
	}
}

func TestExecutionRouteProofsAreImmutableAndStateBound(t *testing.T) {
	f, c := newTargetFixture(t)
	ctx := t.Context()
	primaryTask := targetTask(t, f, f.runtime)
	p, err := recordRoute(ctx, f.q, f, c, primaryTask, "primary")
	if err != nil {
		t.Fatal(err)
	}
	_, err = recordRoute(ctx, f.q, f, c, primaryTask, "primary")
	wantNoTransition(t, err)
	_, err = recordRoute(ctx, f.q, f, c, targetTask(t, f, c.RuntimeID), "fallback")
	wantNoTransition(t, err)
	_, err = recordRoute(ctx, f.q, f, c, targetTask(t, f, c.RuntimeID), "primary")
	wantNoTransition(t, err)
	_, err = recordRoute(ctx, f.q, f, c, targetTask(t, f, f.runtime), "unknown")
	wantNoTransition(t, err)
	stale := c
	stale.Generation = newGeneration()
	_, err = recordRoute(ctx, f.q, f, stale, targetTask(t, f, f.runtime), "primary")
	wantNoTransition(t, err)
	state, err := f.activate(ctx, f.q)
	if err != nil {
		t.Fatal(err)
	}
	fallbackTask := targetTask(t, f, c.RuntimeID)
	// Enqueue proof selected before transition cannot be written afterward.
	_, err = recordRoute(ctx, f.q, f, c, fallbackTask, "fallback")
	wantNoTransition(t, err)
	f.row = state
	route, err := recordRoute(ctx, f.q, f, c, fallbackTask, "fallback")
	if err != nil {
		t.Fatal(err)
	}
	_, err = recordRoute(ctx, f.q, f, c, targetTask(t, f, f.runtime), "primary")
	wantNoTransition(t, err)
	f.due(t)
	probe, err := f.claim(ctx, f.q)
	if err != nil {
		t.Fatal(err)
	}
	f.row = probe
	if _, err := recordRoute(ctx, f.q, f, c, targetTask(t, f, c.RuntimeID), "fallback"); err != nil {
		t.Fatal(err)
	}
	probe, err = f.dispatch(ctx, f.q, probe)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.settle(ctx, f.q, probe, "success"); err != nil {
		t.Fatal(err)
	}
	saved, err := f.q.GetAgentTaskExecutionRoute(ctx, db.GetAgentTaskExecutionRouteParams{TaskID: fallbackTask, WorkspaceID: f.row.WorkspaceID})
	if err != nil {
		t.Fatal(err)
	}
	if saved != route {
		t.Fatal("recovery rewrote queued fallback proof")
	}
	saved, err = f.q.GetAgentTaskExecutionRoute(ctx, db.GetAgentTaskExecutionRouteParams{TaskID: primaryTask, WorkspaceID: f.row.WorkspaceID})
	if err != nil {
		t.Fatal(err)
	}
	if saved != p {
		t.Fatal("transition rewrote primary proof")
	}
	_, err = f.q.GetAgentTaskExecutionRoute(ctx, db.GetAgentTaskExecutionRouteParams{TaskID: primaryTask, WorkspaceID: newGeneration()})
	wantNoTransition(t, err)
	// Stored state version must not be substituted with the recovered version.
	_, err = f.q.ActivateAgentFallbackState(ctx, db.ActivateAgentFallbackStateParams{AgentID: p.AgentID, WorkspaceID: p.WorkspaceID, PrimaryGeneration: p.PrimaryGeneration, ExpectedVersion: p.StateVersion, FailureTaskID: f.task})
	wantNoTransition(t, err)
}

func TestExecutionRouteAndTaskRollbackAndRetryInheritance(t *testing.T) {
	f, c := newTargetFixture(t)
	ctx := t.Context()
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	var task pgtype.UUID
	if err := tx.QueryRow(ctx, `INSERT INTO agent_task_queue(agent_id,runtime_id,status) VALUES($1,$2,'queued') RETURNING id`, f.row.AgentID, f.runtime).Scan(&task); err != nil {
		t.Fatal(err)
	}
	if _, err := recordRoute(ctx, f.q.WithTx(tx), f, c, task, "primary"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM agent_task_queue WHERE id=$1)+(SELECT count(*) FROM agent_task_execution_route WHERE task_id=$1)`, task).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rollback count=%d err=%v", count, err)
	}
	parent := targetTask(t, f, f.runtime)
	proof, err := recordRoute(ctx, f.q, f, c, parent, "primary")
	if err != nil {
		t.Fatal(err)
	}
	child := targetTask(t, f, f.runtime)
	f.fx.Exec(t, `UPDATE agent_task_queue SET parent_task_id=$1, retry_of_task_id=$1 WHERE id=$2`, parent, child)
	_, err = f.q.CopyAgentTaskExecutionRouteForRetry(ctx, db.CopyAgentTaskExecutionRouteForRetryParams{ParentTaskID: parent, ChildTaskID: child, WorkspaceID: c.WorkspaceID})
	wantNoTransition(t, err)
	f.fx.Exec(t, `UPDATE agent_task_queue SET status='failed', completed_at=now() WHERE id=$1`, parent)
	if _, err = f.q.DeleteAgentFallbackConfig(ctx, db.DeleteAgentFallbackConfigParams{AgentID: c.AgentID, WorkspaceID: c.WorkspaceID, ExpectedGeneration: c.Generation, ExpectedPrimaryGeneration: f.row.PrimaryGeneration, ExpectedVersion: f.row.Version, NewPrimaryGeneration: newGeneration()}); err != nil {
		t.Fatal(err)
	}
	copied, err := f.q.CopyAgentTaskExecutionRouteForRetry(ctx, db.CopyAgentTaskExecutionRouteForRetryParams{ParentTaskID: parent, ChildTaskID: child, WorkspaceID: c.WorkspaceID})
	if err != nil {
		t.Fatal(err)
	}
	if copied.TaskID != child || copied.RuntimeID != proof.RuntimeID || copied.PrimaryGeneration != proof.PrimaryGeneration || copied.StateVersion != proof.StateVersion || copied.FallbackGeneration != proof.FallbackGeneration || copied.Route != proof.Route {
		t.Fatal("retry changed route proof")
	}
	wrongRuntime := targetTask(t, f, c.RuntimeID)
	f.fx.Exec(t, `UPDATE agent_task_queue SET parent_task_id=$1, retry_of_task_id=$1 WHERE id=$2`, parent, wrongRuntime)
	_, err = f.q.CopyAgentTaskExecutionRouteForRetry(ctx, db.CopyAgentTaskExecutionRouteForRetryParams{ParentTaskID: parent, ChildTaskID: wrongRuntime, WorkspaceID: c.WorkspaceID})
	wantNoTransition(t, err)
}

func TestConcurrentRouteInsertAndSuspension(t *testing.T) {
	f, c := newTargetFixture(t)
	ctx := t.Context()
	task := targetTask(t, f, f.runtime)
	f.fx.Exec(t, `UPDATE agent_task_queue SET status='deferred' WHERE id=$1`, task)
	var winners atomic.Int32
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			_, err := recordRoute(ctx, f.q, f, c, task, "primary")
			if err == nil {
				winners.Add(1)
			} else if !errors.Is(err, pgx.ErrNoRows) {
				t.Errorf("route: %v", err)
			}
		})
	}
	wg.Wait()
	if winners.Load() != 1 {
		t.Fatalf("route winners=%d", winners.Load())
	}
	if _, err := f.q.SuspendAgentFailoverState(ctx, db.SuspendAgentFailoverStateParams{AgentID: f.row.AgentID, WorkspaceID: f.row.WorkspaceID}); err != nil {
		t.Fatal(err)
	}
	_, err := recordRoute(ctx, f.q, f, c, targetTask(t, f, f.runtime), "primary")
	wantNoTransition(t, err)
}

func TestRouteProofWaitsForStateWriteAndRejectsStaleSelection(t *testing.T) {
	f, c := newTargetFixture(t)
	task := targetTask(t, f, f.runtime)
	tx, err := f.pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(t.Context(), `UPDATE agent_failover_state SET version=version+1 WHERE agent_id=$1`, f.row.AgentID); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	_, err = recordRoute(ctx, f.q, f, c, task, "primary")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("route bypassed locked state: %v", err)
	}
	if err := tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	_, err = recordRoute(t.Context(), f.q, f, c, task, "primary")
	wantNoTransition(t, err)
	_, err = f.q.GetAgentTaskExecutionRoute(t.Context(), db.GetAgentTaskExecutionRouteParams{TaskID: task, WorkspaceID: f.row.WorkspaceID})
	wantNoTransition(t, err)
}
