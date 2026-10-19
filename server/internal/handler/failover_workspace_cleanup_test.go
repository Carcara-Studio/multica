package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestWorkspaceDeletionCleansFailoverStateAtomicallyAndPreservesNeighbour(t *testing.T) {
	if testPool == nil || testHandler == nil {
		t.Skip("database not available")
	}
	ctx := t.Context()
	fx := testutil.New(testPool, testWorkspaceID, testUserID)
	victim := fx.Workspace(t, "Failover cleanup", "failover-cleanup-"+uuid.NewString())
	fx.Member(t, victim, testUserID, "owner")
	victimAgent := fx.Agent(t, "Failover cleanup victim", "", testutil.Cols{"workspace_id": victim})
	neighbourAgent := fx.Agent(t, "Failover cleanup neighbour", "")
	for _, entry := range []struct{ agent, workspace string }{{victimAgent, victim}, {neighbourAgent, testWorkspaceID}} {
		fx.InsertNoID(t, "agent_failover_state", testutil.Cols{
			"agent_id": entry.agent, "workspace_id": entry.workspace, "primary_generation": uuid.NewString(),
			"state": "primary_probing", "probe_token": uuid.NewString(), "probe_runtime_id": uuid.NewString(),
			"last_probe_at": testutil.Raw("now()"), "probe_lease_until": testutil.Raw("now()+interval '2 minutes'"),
		}, "agent_id=$1", entry.agent)

		fx.InsertNoID(t, "agent_fallback_config", testutil.Cols{
			"agent_id": entry.agent, "workspace_id": entry.workspace,
			"runtime_id": uuid.NewString(), "generation": uuid.NewString(),
		}, "agent_id=$1", entry.agent)
		fx.InsertNoID(t, "agent_task_execution_route", testutil.Cols{
			"task_id": uuid.NewString(), "agent_id": entry.agent, "workspace_id": entry.workspace,
			"runtime_id": uuid.NewString(), "route": "fallback", "primary_generation": uuid.NewString(),
			"state_version": 1, "fallback_generation": uuid.NewString(),
		}, "agent_id=$1", entry.agent)
	}
	bundleExists := func(t *testing.T, agent string) bool {
		t.Helper()
		var exists bool
		if err := testPool.QueryRow(ctx, `SELECT (SELECT count(*) FROM agent_failover_state WHERE agent_id=$1) + (SELECT count(*) FROM agent_fallback_config WHERE agent_id=$1) + (SELECT count(*) FROM agent_task_execution_route WHERE agent_id=$1) = 3`, agent).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		return exists
	}
	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	qtx := testHandler.Queries.WithTx(tx)
	if _, err := qtx.LockWorkspaceForDelete(ctx, parseUUID(victim)); err != nil {
		t.Fatal(err)
	}
	if err := qtx.DeleteWorkspace(ctx, parseUUID(victim)); err != nil {
		t.Fatal(err)
	}
	var leftovers int
	if err := tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM agent_failover_state WHERE agent_id=$1) + (SELECT count(*) FROM agent_fallback_config WHERE agent_id=$1) + (SELECT count(*) FROM agent_task_execution_route WHERE agent_id=$1)`, victimAgent).Scan(&leftovers); err != nil {
		t.Fatal(err)
	}
	if leftovers != 0 {
		t.Fatal("failover state survived inside workspace delete transaction")
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if !bundleExists(t, victimAgent) || !bundleExists(t, neighbourAgent) {
		t.Fatal("workspace rollback did not preserve failover state")
	}

	req := withURLParam(newRequest(http.MethodDelete, "/api/workspaces/"+victim, nil), "id", victim)
	req.Header.Set("X-Workspace-ID", victim)
	testutil.Call(t, testHandler.DeleteWorkspace, req).Want(http.StatusNoContent)
	var leftoversAfter int
	if err := testPool.QueryRow(ctx, `SELECT (SELECT count(*) FROM agent_failover_state WHERE agent_id=$1) + (SELECT count(*) FROM agent_fallback_config WHERE agent_id=$1) + (SELECT count(*) FROM agent_task_execution_route WHERE agent_id=$1)`, victimAgent).Scan(&leftoversAfter); err != nil {
		t.Fatal(err)
	}
	if leftoversAfter != 0 {
		t.Fatal("workspace delete left a live probe claim orphaned")
	}
	if !bundleExists(t, neighbourAgent) {
		t.Fatal("workspace delete removed a neighbour's failover state")
	}
}
