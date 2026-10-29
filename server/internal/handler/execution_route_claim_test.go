package handler

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

func TestUnresolvedRouteDeliverySettlesWithoutCredentialsOrReplacement(t *testing.T) {
	runtimeID := dbfx.Runtime(t, "Route gate runtime", testutil.Cols{"provider": "codex"})
	agentID := dbfx.Agent(t, "Route gate agent", runtimeID, testutil.Cols{"kind": "user"})
	id := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "status": "dispatched", "dispatched_at": testutil.Raw("clock_timestamp()")})
	dbfx.Exec(t, `INSERT INTO agent_task_execution_route(task_id,agent_id,workspace_id,runtime_id,route,primary_generation,state_version,fallback_generation) VALUES($1,$2,$3,$4,'primary',$5,1,$6)`, id, agentID, testWorkspaceID, runtimeID, uuid.NewString(), uuid.NewString())
	dbfx.Cleanup(t, `DELETE FROM agent_task_execution_route WHERE task_id=$1`, id)
	dbfx.Cleanup(t, `DELETE FROM task_token WHERE task_id=$1`, id)
	task, err := testHandler.Queries.GetAgentTask(t.Context(), parseUUID(id))
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := testHandler.Queries.GetAgentRuntime(t.Context(), parseUUID(runtimeID))
	if err != nil {
		t.Fatal(err)
	}
	token := db.CreateTaskTokenParams{TokenHash: uuid.NewString(), TaskID: task.ID, AgentID: task.AgentID, WorkspaceID: parseUUID(testWorkspaceID), UserID: parseUUID(testUserID), ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true}}
	_, failure, err := testHandler.finalizeClaimDelivery(t.Context(), &task, runtime, runtimeID, testWorkspaceID, nil, token, nil, false, nil)
	if err != nil || failure == nil || !failure.settled || failure.status != http.StatusConflict || failure.outcome != "error_execution_route_unavailable" {
		t.Fatalf("wrong delivery disposition: %+v %v", failure, err)
	}
	current, err := testHandler.Queries.GetAgentTask(t.Context(), task.ID)
	if err != nil || current.Status != "failed" || current.FailureReason.String != taskfailure.ReasonInvalidTaskIdentity.String() {
		t.Fatalf("wrong settlement: %+v %v", current, err)
	}
	if dbfx.Count(t, `SELECT count(*) FROM task_token WHERE task_id=$1`, id) != 0 {
		t.Fatal("credentials issued")
	}
	if dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE agent_id=$1`, agentID) != 1 {
		t.Fatal("replacement task created")
	}
}
