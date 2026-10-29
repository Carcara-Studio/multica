package service

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type routedClaimFixture struct {
	fx    *testutil.Fixture
	svc   *TaskService
	task  db.AgentTaskQueue
	token db.CreateTaskTokenParams
}

func newRoutedClaimFixture(t *testing.T, route, status string) routedClaimFixture {
	t.Helper()
	pool := sharedTestPool(t)
	fx := testutil.New(pool, "", "")
	fx.UserID = fx.User(t, "Route gate owner", uuid.NewString()+"@example.test")
	fx.WorkspaceID = fx.Workspace(t, "Route gate", "route-gate-"+uuid.NewString())
	fx.Member(t, fx.WorkspaceID, fx.UserID, "owner")
	runtimeID := fx.Runtime(t, "Route gate runtime", testutil.Cols{"provider": "codex"})
	agentID := fx.Agent(t, "Route gate agent", runtimeID, testutil.Cols{"kind": "user"})
	id := fx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "status": status, "dispatched_at": testutil.Raw("clock_timestamp()")})
	q := db.New(pool)
	task, err := q.GetAgentTask(t.Context(), util.MustParseUUID(id))
	if err != nil {
		t.Fatal(err)
	}
	if route != "" {
		// Deliberately orphaned metadata: intent/config absence never makes a
		// marked task legacy. No fixture capability authorizes this route.
		fx.Exec(t, `INSERT INTO agent_task_execution_route(task_id,agent_id,workspace_id,runtime_id,route,primary_generation,state_version,fallback_generation) VALUES($1,$2,$3,$4,$5,$6,1,$7)`, id, agentID, fx.WorkspaceID, runtimeID, route, uuid.NewString(), uuid.NewString())
		fx.Cleanup(t, `DELETE FROM agent_task_execution_route WHERE task_id=$1`, id)
	}
	fx.Cleanup(t, `DELETE FROM task_token WHERE task_id=$1`, id)
	fx.Cleanup(t, `DELETE FROM task_supplement_capability WHERE task_id=$1`, id)
	return routedClaimFixture{fx: fx, svc: NewTaskService(q, pool, nil, events.New()), task: task, token: db.CreateTaskTokenParams{
		TokenHash: uuid.NewString(), TaskID: task.ID, AgentID: task.AgentID, WorkspaceID: util.MustParseUUID(fx.WorkspaceID), UserID: util.MustParseUUID(fx.UserID), ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
	}}
}

func TestUnresolvedExecutionRouteRejectsClaimCredentials(t *testing.T) {
	for _, route := range []string{"primary", "fallback"} {
		t.Run(route, func(t *testing.T) {
			f := newRoutedClaimFixture(t, route, "dispatched")
			mcp := db.CreateDaemonTokenParams{TokenHash: uuid.NewString(), WorkspaceID: f.token.WorkspaceID, DaemonID: "synthetic-mcp", ExpiresAt: f.token.ExpiresAt}
			_, err := f.svc.FinalizeTaskClaim(t.Context(), f.task, f.token, nil, true, nil, []byte(`{"new":"snapshot"}`), mcp)
			var denied *ClaimDeliveryAuthzError
			if !errors.As(err, &denied) || denied.Reason != "error_execution_route_unavailable" {
				t.Fatalf("wrong rejection: %v", err)
			}
			if got := f.fx.Count(t, `SELECT count(*) FROM task_token WHERE task_id=$1`, f.task.ID); got != 0 {
				t.Fatalf("task token issued: %d", got)
			}
			if got := f.fx.Count(t, `SELECT count(*) FROM daemon_token WHERE token_hash=$1`, mcp.TokenHash); got != 0 {
				t.Fatalf("MCP token issued: %d", got)
			}
			current, err := f.svc.Queries.GetAgentTask(t.Context(), f.task.ID)
			if err != nil || current.Status != "dispatched" || len(current.IssueSnapshot) != 0 || len(current.DeliveredCommentIds) != 0 {
				t.Fatalf("rejected delivery mutated task: %+v err=%v", current, err)
			}
			if got := f.fx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE agent_id=$1`, f.task.AgentID); got != 1 {
				t.Fatalf("created replacement work: %d", got)
			}
		})
	}
}

func TestUnresolvedExecutionRouteRejectsModernAndLegacyStarts(t *testing.T) {
	for _, route := range []string{"primary", "fallback"} {
		for _, status := range []string{"dispatched", "waiting_local_directory", "running"} {
			t.Run(route+"/"+status, func(t *testing.T) {
				f := newRoutedClaimFixture(t, route, status)
				got, err := f.svc.StartTaskForClaim(t.Context(), db.LockAgentTaskStartClaimParams{ID: f.task.ID, RuntimeID: f.task.RuntimeID, DispatchedAt: f.task.DispatchedAt}, true)
				var denied *ClaimDeliveryAuthzError
				if got != nil || !errors.As(err, &denied) {
					t.Fatalf("modern start/replay accepted: %+v %v", got, err)
				}
				got, err = f.svc.StartTask(t.Context(), f.task.ID, true)
				if got != nil || !errors.Is(err, pgx.ErrNoRows) {
					t.Fatalf("legacy start accepted: %+v %v", got, err)
				}
				if _, err := f.svc.Queries.StartAgentTask(t.Context(), f.task.ID); !errors.Is(err, pgx.ErrNoRows) {
					t.Fatalf("direct legacy SQL start accepted: %v", err)
				}
				current, err := f.svc.Queries.GetAgentTask(t.Context(), f.task.ID)
				if err != nil || current.Status != status || current.StartedAt != f.task.StartedAt {
					t.Fatalf("task changed: %+v %v", current, err)
				}
				if n := f.fx.Count(t, `SELECT count(*) FROM task_supplement_capability WHERE task_id=$1`, f.task.ID); n != 0 {
					t.Fatal("supplement capability created")
				}
			})
		}
	}
}

func TestConfigurationIntentWithoutRouteKeepsLegacyExecution(t *testing.T) {
	f := newRoutedClaimFixture(t, "", "dispatched")
	f.fx.Exec(t, `INSERT INTO agent_fallback_config(agent_id,workspace_id,runtime_id,generation,model,native_login_path) VALUES($1,$2,$3,$4,'pending-model',true)`, f.task.AgentID, f.token.WorkspaceID, f.task.RuntimeID, uuid.NewString())
	f.fx.Cleanup(t, `DELETE FROM agent_fallback_config WHERE agent_id=$1`, f.task.AgentID)
	if _, err := f.svc.FinalizeTaskClaim(t.Context(), f.task, f.token, nil, false, nil, nil); err != nil {
		t.Fatal(err)
	}
	claim := db.LockAgentTaskStartClaimParams{ID: f.task.ID, RuntimeID: f.task.RuntimeID, DispatchedAt: f.task.DispatchedAt}
	if got, err := f.svc.StartTaskForClaim(t.Context(), claim); err != nil || got.Status != "running" {
		t.Fatalf("unmarked start failed: %+v %v", got, err)
	}
	if got, err := f.svc.StartTaskForClaim(t.Context(), claim); err != nil || got.Status != "running" {
		t.Fatalf("unmarked replay failed: %+v %v", got, err)
	}
}
