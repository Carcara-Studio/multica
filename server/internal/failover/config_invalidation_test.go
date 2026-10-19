package failover

import (
	"context"
	"reflect"
	"testing"
	"time"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func configEdit(c db.AgentFallbackConfig, s db.AgentFailoverState) db.UpdateAgentFallbackConfigParams {
	return db.UpdateAgentFallbackConfigParams{
		AgentID: c.AgentID, WorkspaceID: c.WorkspaceID, RuntimeID: c.RuntimeID,
		ExpectedGeneration: c.Generation, NewGeneration: newGeneration(),
		ExpectedPrimaryGeneration: s.PrimaryGeneration, ExpectedVersion: s.Version, NewPrimaryGeneration: newGeneration(),
		Model: "changed-model", ThinkingLevel: c.ThinkingLevel, ServiceTier: c.ServiceTier,
	}
}
func configRemoval(c db.AgentFallbackConfig, s db.AgentFailoverState) db.DeleteAgentFallbackConfigParams {
	return db.DeleteAgentFallbackConfigParams{AgentID: c.AgentID, WorkspaceID: c.WorkspaceID, ExpectedGeneration: c.Generation,
		ExpectedPrimaryGeneration: s.PrimaryGeneration, ExpectedVersion: s.Version, NewPrimaryGeneration: newGeneration()}
}
func storedState(t *testing.T, q *db.Queries, f *stateFixture) db.AgentFailoverState {
	t.Helper()
	s, err := q.GetAgentFailoverState(t.Context(), db.GetAgentFailoverStateParams{AgentID: f.row.AgentID, WorkspaceID: f.row.WorkspaceID})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func storedConfig(t *testing.T, q *db.Queries, c db.AgentFallbackConfig) db.AgentFallbackConfig {
	t.Helper()
	saved, err := q.GetAgentFallbackConfig(t.Context(), db.GetAgentFallbackConfigParams{AgentID: c.AgentID, WorkspaceID: c.WorkspaceID})
	if err != nil {
		t.Fatal(err)
	}
	return saved
}

func TestConfigEditAtomicallyInvalidatesDispatchedProbeAndQueuedProof(t *testing.T) {
	f, c := newTargetFixture(t)
	ctx := t.Context()
	task := targetTask(t, f, f.runtime)
	proof, err := recordRoute(ctx, f.q, f, c, task, "primary")
	if err != nil {
		t.Fatal(err)
	}
	p := f.probe(t)
	p, err = f.dispatch(ctx, f.q, p)
	if err != nil {
		t.Fatal(err)
	}
	args := configEdit(c, p)
	updated, err := f.q.UpdateAgentFallbackConfig(ctx, args)
	if err != nil {
		t.Fatal(err)
	}
	s := storedState(t, f.q, f)
	if updated.Generation != args.NewGeneration || updated.RuntimeID != c.RuntimeID || s.PrimaryGeneration != args.NewPrimaryGeneration || s.Version != p.Version+1 {
		t.Fatal("config/state epochs did not rotate together")
	}
	if s.State != "fallback_active" || s.SuspendedAt.Valid || s.ProbeToken.Valid || s.ProbeLeaseUntil.Valid || s.ProbeDispatchedAt.Valid || s.ProbeRuntimeID.Valid {
		t.Fatal("edit preserved claim or recovered primary")
	}
	if !s.NextProbeAt.Time.Equal(s.UpdatedAt.Time.Add(30*time.Minute)) || s.PrimaryFailureTaskID != p.PrimaryFailureTaskID || s.RecoveredAt != p.RecoveredAt {
		t.Fatal("edit lost availability history or schedule")
	}
	_, err = f.settle(ctx, f.q, p, "success")
	wantNoTransition(t, err)
	saved, err := f.q.GetAgentTaskExecutionRoute(ctx, db.GetAgentTaskExecutionRouteParams{TaskID: task, WorkspaceID: c.WorkspaceID})
	if err != nil {
		t.Fatal(err)
	}
	if saved != proof || saved.FallbackGeneration == updated.Generation || saved.PrimaryGeneration == s.PrimaryGeneration {
		t.Fatal("edit rewrote queued proof instead of invalidating its epochs")
	}
	_, err = f.q.UpdateAgentFallbackConfig(ctx, args)
	wantNoTransition(t, err)
	if !reflect.DeepEqual(storedState(t, f.q, f), s) || !reflect.DeepEqual(storedConfig(t, f.q, c), updated) {
		t.Fatal("stale edit mutated config/state")
	}
}

func TestConfigMutationRollbackAndRemovalDoNotRecoverPrimary(t *testing.T) {
	f, c := newTargetFixture(t)
	ctx := t.Context()
	p := f.probe(t)
	var err error
	p, err = f.dispatch(ctx, f.q, p)
	if err != nil {
		t.Fatal(err)
	}
	for _, remove := range []bool{false, true} {
		tx, err := f.pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
		q := f.q.WithTx(tx)
		if remove {
			n, err := q.DeleteAgentFallbackConfig(ctx, configRemoval(c, p))
			if err != nil || n != 1 {
				t.Fatalf("remove=%d err=%v", n, err)
			}
			_, err = q.GetAgentFallbackConfig(ctx, db.GetAgentFallbackConfigParams{AgentID: c.AgentID, WorkspaceID: c.WorkspaceID})
			wantNoTransition(t, err)
		} else {
			if _, err := q.UpdateAgentFallbackConfig(ctx, configEdit(c, p)); err != nil {
				t.Fatal(err)
			}
		}
		inside := storedState(t, q, f)
		if inside.ProbeToken.Valid || inside.PrimaryGeneration == p.PrimaryGeneration || inside.State != "fallback_active" || inside.SuspendedAt.Valid != remove {
			t.Fatal("mutation not visible atomically inside transaction")
		}
		if err := tx.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(storedState(t, f.q, f), p) || !reflect.DeepEqual(storedConfig(t, f.q, c), c) {
			t.Fatal("rollback lost config or probe authority")
		}
	}
	args := configRemoval(c, p)
	n, err := f.q.DeleteAgentFallbackConfig(ctx, args)
	if err != nil || n != 1 {
		t.Fatalf("delete=%d err=%v", n, err)
	}
	s := storedState(t, f.q, f)
	if s.State != "fallback_active" || !s.SuspendedAt.Valid || s.ProbeToken.Valid || s.PrimaryGeneration != args.NewPrimaryGeneration || s.Version != p.Version+1 {
		t.Fatal("removal did not suspend/fence without recovery")
	}
	_, err = f.settle(ctx, f.q, p, "success")
	wantNoTransition(t, err)
	n, err = f.q.DeleteAgentFallbackConfig(ctx, args)
	if err != nil || n != 0 {
		t.Fatalf("duplicate delete=%d err=%v", n, err)
	}
	if !reflect.DeepEqual(storedState(t, f.q, f), s) {
		t.Fatal("duplicate removal changed availability")
	}
}

func TestConfigMutationsRequireMatchingStateAndPreserveSuspension(t *testing.T) {
	f, c := newTargetFixture(t)
	ctx := t.Context()
	args := configEdit(c, f.row)
	args.ExpectedVersion++
	_, err := f.q.UpdateAgentFallbackConfig(ctx, args)
	wantNoTransition(t, err)
	remove := configRemoval(c, f.row)
	remove.ExpectedPrimaryGeneration = newGeneration()
	n, err := f.q.DeleteAgentFallbackConfig(ctx, remove)
	if err != nil || n != 0 {
		t.Fatalf("stale state delete=%d err=%v", n, err)
	}
	if !reflect.DeepEqual(storedState(t, f.q, f), f.row) || !reflect.DeepEqual(storedConfig(t, f.q, c), c) {
		t.Fatal("mismatched state changed config")
	}
	s, err := f.q.SuspendAgentFailoverState(ctx, db.SuspendAgentFailoverStateParams{AgentID: c.AgentID, WorkspaceID: c.WorkspaceID})
	if err != nil {
		t.Fatal(err)
	}
	c, err = f.q.UpdateAgentFallbackConfig(ctx, configEdit(c, s))
	if err != nil {
		t.Fatal(err)
	}
	after := storedState(t, f.q, f)
	if after.SuspendedAt != s.SuspendedAt || after.State != "primary_active" || after.NextProbeAt.Valid {
		t.Fatal("edit resumed suspension or changed availability")
	}
	f.fx.Exec(t, `DELETE FROM agent_failover_state WHERE agent_id=$1`, c.AgentID)
	_, err = f.q.UpdateAgentFallbackConfig(ctx, configEdit(c, after))
	wantNoTransition(t, err)
	n, err = f.q.DeleteAgentFallbackConfig(ctx, configRemoval(c, after))
	if err != nil || n != 0 {
		t.Fatalf("orphan remove=%d err=%v", n, err)
	}
	if !reflect.DeepEqual(storedConfig(t, f.q, c), c) {
		t.Fatal("missing state authorized config mutation")
	}
}
