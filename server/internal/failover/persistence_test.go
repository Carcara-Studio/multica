package failover

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type stateFixture struct {
	pool          *pgxpool.Pool
	q             *db.Queries
	fx            *testutil.Fixture
	row           db.AgentFailoverState
	runtime, task pgtype.UUID
}

func newStateFixture(t *testing.T, taskStatus string) *stateFixture {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL required; use the managed test environment")
	}
	pool, err := pgxpool.New(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(t.Context()); err != nil {
		t.Fatal(err)
	}
	fx := testutil.New(pool, "", "")
	fx.UserID = fx.User(t, "Failover Test", "failover-"+uuid.NewString()+"@example.test")
	fx.WorkspaceID = fx.Workspace(t, "Failover Test", "failover-"+uuid.NewString())
	fx.Member(t, fx.WorkspaceID, fx.UserID, "owner")
	runtime := fx.Runtime(t, "Primary")
	agent := fx.Agent(t, "Failover", runtime)
	taskCols := testutil.Cols{"runtime_id": runtime, "status": taskStatus}
	if taskStatus == "failed" {
		taskCols["completed_at"] = testutil.Raw("now()")
		taskCols["failure_reason"] = "agent_error.provider_quota_limit"
	}
	task := fx.Task(t, agent, taskCols)
	fx.Cleanup(t, `DELETE FROM agent_failover_state WHERE agent_id=$1`, agent)
	q := db.New(pool)
	row, err := q.InitializeAgentFailoverState(t.Context(), db.InitializeAgentFailoverStateParams{
		AgentID: testUUID(agent), WorkspaceID: testUUID(fx.WorkspaceID), PrimaryGeneration: newGeneration(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return &stateFixture{pool: pool, q: q, fx: fx, row: row, runtime: testUUID(runtime), task: testUUID(task)}
}

func testUUID(s string) pgtype.UUID {
	return pgtype.UUID{Bytes: [16]byte(uuid.MustParse(s)), Valid: true}
}
func newGeneration() pgtype.UUID { return pgtype.UUID{Bytes: [16]byte(uuid.New()), Valid: true} }

func (f *stateFixture) activate(ctx context.Context, q *db.Queries) (db.AgentFailoverState, error) {
	return q.ActivateAgentFallbackState(ctx, db.ActivateAgentFallbackStateParams{
		AgentID: f.row.AgentID, WorkspaceID: f.row.WorkspaceID,
		PrimaryGeneration: f.row.PrimaryGeneration, ExpectedVersion: f.row.Version, FailureTaskID: f.task,
	})
}

func (f *stateFixture) due(t *testing.T) {
	t.Helper()
	f.fx.Exec(t, `UPDATE agent_failover_state SET next_probe_at=clock_timestamp()-interval '1 second' WHERE agent_id=$1`, f.row.AgentID)
}

func (f *stateFixture) claim(ctx context.Context, q *db.Queries) (db.AgentFailoverState, error) {
	return q.ClaimDueAgentPrimaryProbe(ctx, db.ClaimDueAgentPrimaryProbeParams{WorkspaceID: f.row.WorkspaceID, PrimaryRuntimeID: f.runtime})
}

func (f *stateFixture) dispatch(ctx context.Context, q *db.Queries, p db.AgentFailoverState) (db.AgentFailoverState, error) {
	return q.MarkAgentPrimaryProbeDispatched(ctx, db.MarkAgentPrimaryProbeDispatchedParams{
		AgentID: p.AgentID, WorkspaceID: p.WorkspaceID, PrimaryGeneration: p.PrimaryGeneration,
		ProbeToken: p.ProbeToken, PrimaryRuntimeID: f.runtime,
	})
}

func (f *stateFixture) settle(ctx context.Context, q *db.Queries, p db.AgentFailoverState, outcome string) (db.AgentFailoverState, error) {
	return q.SettleAgentPrimaryProbe(ctx, db.SettleAgentPrimaryProbeParams{
		AgentID: p.AgentID, WorkspaceID: p.WorkspaceID, PrimaryGeneration: p.PrimaryGeneration,
		ProbeToken: p.ProbeToken, PrimaryRuntimeID: f.runtime, Outcome: outcome,
	})
}

func (f *stateFixture) probe(t *testing.T) db.AgentFailoverState {
	t.Helper()
	if _, err := f.activate(t.Context(), f.q); err != nil {
		t.Fatal(err)
	}
	f.due(t)
	p, err := f.claim(t.Context(), f.q)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func wantNoTransition(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("transition err=%v, want no rows", err)
	}
}

func TestFailoverInitializationAndQuotaTaskFences(t *testing.T) {
	f, _ := newTargetFixture(t)
	ctx := t.Context()
	_, err := f.q.InitializeAgentFailoverState(ctx, db.InitializeAgentFailoverStateParams{
		AgentID: f.row.AgentID, WorkspaceID: f.row.WorkspaceID, PrimaryGeneration: newGeneration(),
	})
	wantNoTransition(t, err)
	_, err = f.q.GetAgentFailoverState(ctx, db.GetAgentFailoverStateParams{AgentID: f.row.AgentID, WorkspaceID: newGeneration()})
	wantNoTransition(t, err)
	unconfigured := f.fx.Agent(t, "Unconfigured", "")
	_, err = f.q.GetAgentFailoverState(ctx, db.GetAgentFailoverStateParams{AgentID: testUUID(unconfigured), WorkspaceID: f.row.WorkspaceID})
	wantNoTransition(t, err)
	_, err = f.q.InitializeAgentFailoverState(ctx, db.InitializeAgentFailoverStateParams{
		AgentID: testUUID(unconfigured), WorkspaceID: newGeneration(), PrimaryGeneration: newGeneration(),
	})
	wantNoTransition(t, err)
	otherTask := f.fx.Task(t, unconfigured, testutil.Cols{"status": "failed", "completed_at": testutil.Raw("now()"), "failure_reason": "agent_error.provider_quota_limit"})
	savedTask := f.task
	f.task = testUUID(otherTask)
	_, err = f.activate(ctx, f.q)
	wantNoTransition(t, err)
	f.task = savedTask
	// A quota label on another task, or a nonterminal task, is not evidence for
	// this Agent. Each failed CAS must leave initialization and schedule intact.
	for _, override := range []string{
		`UPDATE agent_task_queue SET failure_reason='agent_error.provider_network' WHERE id=$1`,
		`UPDATE agent_task_queue SET failure_reason='agent_error.provider_quota_limit',status='running' WHERE id=$1`,
	} {
		f.fx.Exec(t, override, f.task)
		_, err = f.activate(ctx, f.q)
		wantNoTransition(t, err)
	}
	row, err := f.q.GetAgentFailoverState(ctx, db.GetAgentFailoverStateParams{AgentID: f.row.AgentID, WorkspaceID: f.row.WorkspaceID})
	if err != nil || row.PrimaryGeneration != f.row.PrimaryGeneration || row.Version != 1 || row.NextProbeAt.Valid {
		t.Fatalf("rejected initialization/activation changed state: %+v err=%v", row, err)
	}
}

func TestFallbackActivationCASAndAtomicRollback(t *testing.T) {
	f, _ := newTargetFixture(t)
	ctx := t.Context()
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err := f.activate(ctx, f.q.WithTx(tx)); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	// A new Queries object reads durable state, not process memory.
	row, err := db.New(f.pool).GetAgentFailoverState(ctx, db.GetAgentFailoverStateParams{AgentID: f.row.AgentID, WorkspaceID: f.row.WorkspaceID})
	if err != nil || row.State != "primary_active" || row.Version != 1 {
		t.Fatalf("rollback state=%+v err=%v", row, err)
	}

	var wg sync.WaitGroup
	results := make(chan error, 8)
	for range 8 {
		wg.Go(func() { _, err := f.activate(ctx, db.New(f.pool)); results <- err })
	}
	wg.Wait()
	close(results)
	winners := 0
	for err := range results {
		if err == nil {
			winners++
		} else {
			wantNoTransition(t, err)
		}
	}
	if winners != 1 {
		t.Fatalf("quota transitions=%d, want 1", winners)
	}
	row, err = f.q.GetAgentFailoverState(ctx, db.GetAgentFailoverStateParams{AgentID: f.row.AgentID, WorkspaceID: f.row.WorkspaceID})
	if err != nil {
		t.Fatal(err)
	}
	if row.NextProbeAt.Time.Sub(row.PrimaryFailedAt.Time) != 30*time.Minute || row.Version != 2 {
		t.Fatalf("first schedule/version changed: %+v", row)
	}
	_, err = f.claim(ctx, f.q)
	wantNoTransition(t, err)
	var count int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE agent_id=$1`, row.AgentID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("activation created replay/tasks: count=%d", count)
	}
}

func TestProbeClaimAndDispatchHaveSingleWinner(t *testing.T) {
	f, _ := newTargetFixture(t)
	if _, err := f.activate(t.Context(), f.q); err != nil {
		t.Fatal(err)
	}
	f.due(t)
	var wg sync.WaitGroup
	type result struct {
		row db.AgentFailoverState
		err error
	}
	results := make(chan result, 8)
	for range 8 {
		wg.Go(func() { p, e := f.claim(t.Context(), db.New(f.pool)); results <- result{p, e} })
	}
	wg.Wait()
	close(results)
	var probe db.AgentFailoverState
	winners := 0
	for r := range results {
		if r.err == nil {
			winners++
			probe = r.row
		} else {
			wantNoTransition(t, r.err)
		}
	}
	if winners != 1 || probe.State != "primary_probing" || !probe.ProbeToken.Valid {
		t.Fatalf("claim winners=%d probe=%+v", winners, probe)
	}
	if probe.ProbeLeaseUntil.Time.Sub(probe.LastProbeAt.Time) != 2*time.Minute {
		t.Fatal("unexpected probe lease")
	}
	_, err := f.settle(t.Context(), f.q, probe, "success")
	wantNoTransition(t, err)
	errs := make(chan error, 8)
	for range 8 {
		wg.Go(func() { _, e := f.dispatch(t.Context(), db.New(f.pool), probe); errs <- e })
	}
	wg.Wait()
	close(errs)
	winners = 0
	for err := range errs {
		if err == nil {
			winners++
		} else {
			wantNoTransition(t, err)
		}
	}
	if winners != 1 {
		t.Fatalf("dispatch winners=%d", winners)
	}
	stale := probe
	stale.ProbeToken = newGeneration()
	_, err = f.settle(t.Context(), f.q, stale, "success")
	wantNoTransition(t, err)
	row, err := f.settle(t.Context(), f.q, probe, "success")
	if err != nil || row.State != "primary_active" || row.NextProbeAt.Valid || !row.RecoveredAt.Valid || row.ProbeToken.Valid {
		t.Fatalf("recovery=%+v err=%v", row, err)
	}
	_, err = f.settle(t.Context(), f.q, probe, "success")
	wantNoTransition(t, err)
	_, err = f.activate(t.Context(), f.q)
	wantNoTransition(t, err)
}

func TestUnsuccessfulProbeRetainsFallback(t *testing.T) {
	for _, outcome := range []string{"agent_error.provider_quota_limit", "agent_error.provider_capacity_or_rate_limit", "agent_error.provider_network", "agent_error.provider_auth_or_access", "agent_error.provider_server_error", "agent_error.context_overflow", "probe_timeout", "unknown"} {
		t.Run(outcome, func(t *testing.T) {
			f, _ := newTargetFixture(t)
			probe := f.probe(t)
			if _, err := f.dispatch(t.Context(), f.q, probe); err != nil {
				t.Fatal(err)
			}
			row, err := f.settle(t.Context(), f.q, probe, outcome)
			if err != nil {
				t.Fatal(err)
			}
			if row.State != "fallback_active" || row.ProbeToken.Valid || row.NextProbeAt.Time.Sub(row.UpdatedAt.Time) != 30*time.Minute {
				t.Fatalf("failed probe state=%+v", row)
			}
		})
	}
}

func TestProbeLeaseExpiryAndStaleGeneration(t *testing.T) {
	f, _ := newTargetFixture(t)
	probe := f.probe(t)
	f.fx.Exec(t, `UPDATE agent_failover_state SET last_probe_at=clock_timestamp()-interval '3 minutes', probe_lease_until=clock_timestamp()-interval '1 minute' WHERE agent_id=$1`, probe.AgentID)
	_, err := f.dispatch(t.Context(), f.q, probe)
	wantNoTransition(t, err)
	rows, err := db.New(f.pool).ExpireAgentPrimaryProbeLeases(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range rows {
		if row.AgentID == probe.AgentID {
			found = true
			if row.State != "fallback_active" || row.ProbeToken.Valid || row.NextProbeAt.Time.Sub(row.UpdatedAt.Time) != 30*time.Minute {
				t.Fatalf("expiry=%+v", row)
			}
		}
	}
	if !found {
		t.Fatal("expired claim not recovered")
	}
	_, err = f.settle(t.Context(), f.q, probe, "success")
	wantNoTransition(t, err)
	f.due(t)
	newProbe, err := f.claim(t.Context(), f.q)
	if err != nil {
		t.Fatal(err)
	}
	if probe.ProbeToken == newProbe.ProbeToken {
		t.Fatal("probe token reused")
	}
	newGen := newGeneration()
	row, err := f.q.RotateAgentFailoverGeneration(t.Context(), db.RotateAgentFailoverGenerationParams{AgentID: probe.AgentID, WorkspaceID: probe.WorkspaceID, ExpectedGeneration: probe.PrimaryGeneration, NewGeneration: newGen})
	if err != nil || row.State != "fallback_active" || row.PrimaryGeneration != newGen || row.ProbeToken.Valid {
		t.Fatalf("rotation=%+v err=%v", row, err)
	}
	_, err = f.dispatch(t.Context(), f.q, newProbe)
	wantNoTransition(t, err)
	_, err = f.settle(t.Context(), f.q, newProbe, "success")
	wantNoTransition(t, err)
}

func TestProbeSuspensionArchiveAndWorkspaceFences(t *testing.T) {
	for _, mode := range []string{"suspend", "archive", "delete", "runtime_rebind", "workspace"} {
		t.Run(mode, func(t *testing.T) {
			f, _ := newTargetFixture(t)
			p := f.probe(t)
			if _, err := f.dispatch(t.Context(), f.q, p); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "suspend":
				if _, err := f.q.SuspendAgentFailoverState(t.Context(), db.SuspendAgentFailoverStateParams{AgentID: p.AgentID, WorkspaceID: p.WorkspaceID}); err != nil {
					t.Fatal(err)
				}
			case "archive":
				f.fx.Exec(t, `UPDATE agent SET archived_at=clock_timestamp() WHERE id=$1`, p.AgentID)
			case "delete":
				f.fx.Exec(t, `DELETE FROM agent WHERE id=$1`, p.AgentID)
			case "runtime_rebind":
				newRuntime := f.fx.Runtime(t, "Replacement primary")
				f.fx.Cleanup(t, `UPDATE agent SET runtime_id=NULL WHERE id=$1`, p.AgentID)
				f.fx.Exec(t, `UPDATE agent SET runtime_id=$2 WHERE id=$1`, p.AgentID, newRuntime)
				f.runtime = testUUID(newRuntime)
			case "workspace":
				p.WorkspaceID = newGeneration()
			}
			_, err := f.settle(t.Context(), f.q, p, "success")
			wantNoTransition(t, err)
			_, err = f.claim(t.Context(), f.q)
			wantNoTransition(t, err)
		})
	}
}

func TestProbeSettlementRechecksLeaseAfterLockWait(t *testing.T) {
	f, _ := newTargetFixture(t)
	p := f.probe(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if _, err := f.dispatch(ctx, f.q, p); err != nil {
		t.Fatal(err)
	}
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(ctx, `SELECT agent_id FROM agent_failover_state WHERE agent_id=$1 FOR UPDATE`, p.AgentID); err != nil {
		t.Fatal(err)
	}
	conn, err := f.pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	done := make(chan struct{})
	defer func() { cancel(); <-done; conn.Release() }()
	go func() { defer close(done); _, e := f.settle(ctx, db.New(conn), p, "success"); result <- e }()
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
			t.Fatalf("settlement did not wait for lock: %v", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(5 * time.Millisecond):
		}
	}
	// Expire the lease while the competing statement is blocked. A transaction
	// timestamp or a clock sampled before the row lock would accept this result.
	if _, err := tx.Exec(ctx, `UPDATE agent_failover_state SET probe_lease_until=clock_timestamp() WHERE agent_id=$1`, p.AgentID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	wantNoTransition(t, <-result)
}
