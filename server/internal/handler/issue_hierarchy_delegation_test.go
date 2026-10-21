package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
)

type hierarchyHTTPFixture struct {
	source, target, parent, task, runtime, token string
}

func newHierarchyHTTPFixture(t *testing.T) hierarchyHTTPFixture {
	t.Helper()
	project := hierarchyAPIProject(t)
	runtime := dbfx.Runtime(t, "hierarchy HTTP "+uuid.NewString())
	source := dbfx.Agent(t, "hierarchy HTTP source", runtime)
	target := dbfx.Agent(t, "hierarchy HTTP target", runtime)
	root := createHierarchyMembershipViaAPI(t, project, source, nil, "Lead")
	createHierarchyMembershipViaAPI(t, project, target, &root.ID, "Worker")
	parent := dbfx.Issue(t, "hierarchy HTTP parent", testutil.Cols{
		"number": nextWorkspaceIssueNumber(t), "project_id": project,
		"assignee_type": "agent", "assignee_id": source,
	})
	task := dbfx.Task(t, source, testutil.Cols{
		"runtime_id": runtime, "issue_id": parent, "status": "running",
		"originator_user_id": testUserID, "accountable_user_id": testUserID,
	})
	f := hierarchyHTTPFixture{source: source, target: target, parent: parent, task: task, runtime: runtime}
	f.token = f.newToken(t, task)
	dbfx.Cleanup(t, "DELETE FROM issue WHERE parent_issue_id = $1", parent)
	dbfx.Cleanup(t, "DELETE FROM project_hierarchy_delegation WHERE parent_issue_id = $1", parent)
	return f
}

func (f hierarchyHTTPFixture) newToken(t *testing.T, task string) string {
	t.Helper()
	token := "mat_"+uuid.NewString()
	dbfx.Insert(t, "task_token", testutil.Cols{
		"token_hash": auth.HashToken(token), "task_id": task, "agent_id": f.source,
		"workspace_id": testWorkspaceID, "user_id": testUserID,
		"expires_at": time.Now().Add(time.Hour),
	})
	return token
}

func hierarchyHTTPRouter(h *Handler) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Auth(h.Queries, nil, nil, nil))
	r.Use(middleware.RequireWorkspaceMember(h.Queries))
	r.Post("/api/issues/{id}/hierarchy-delegations", h.CreateHierarchyDelegation)
	return r
}

func (f hierarchyHTTPFixture) call(t *testing.T, h *Handler, parent, key string, extra map[string]any, mutate func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	body := map[string]any{"request_id": key, "target_agent_id": f.target, "title": "delegated HTTP work"}
	for k, v := range extra {
		body[k] = v
	}
	req := newRequest(http.MethodPost, "/api/issues/"+parent+"/hierarchy-delegations", body)
	req.Header.Set("Authorization", "Bearer "+f.token)
	if mutate != nil {
		mutate(req)
	}
	w := httptest.NewRecorder()
	hierarchyHTTPRouter(h).ServeHTTP(w, req)
	return w
}

func hierarchyReceipt(t *testing.T, w *httptest.ResponseRecorder, status int) HierarchyDelegationResponse {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status=%d want=%d body=%s", w.Code, status, w.Body.String())
	}
	var receipt HierarchyDelegationResponse
	if err := json.Unmarshal(w.Body.Bytes(), &receipt); err != nil || receipt.IssueID == "" {
		t.Fatalf("invalid receipt: %+v, %v", receipt, err)
	}
	return receipt
}

func TestHierarchyDelegationHTTPCreateReplayAndNativeOriginator(t *testing.T) {
	f := newHierarchyHTTPFixture(t)
	key := uuid.NewString()
	var number int32
	dbfx.QueryRow(t, "SELECT number FROM issue WHERE id = $1", f.parent).Scan(&number)
	identifier := fmt.Sprintf("%s-%d", testHandler.getIssuePrefix(context.Background(), parseUUID(testWorkspaceID)), number)
	first := hierarchyReceipt(t, f.call(t, testHandler, identifier, key, nil, func(r *http.Request) {
		// Auth must overwrite all forged actor/workspace selectors.
		r.Header.Set("X-User-ID", uuid.NewString())
		r.Header.Set("X-Agent-ID", uuid.NewString())
		r.Header.Set("X-Task-ID", uuid.NewString())
		r.Header.Set("X-Actor-Source", "member")
		r.Header.Set("X-Workspace-ID", uuid.NewString())
	}), http.StatusCreated)
	if first.Replayed {
		t.Fatal("new work marked replay")
	}
	var origin, assignee, parent, creator, human string
	dbfx.QueryRow(t, "SELECT origin_id::text, assignee_id::text, parent_issue_id::text, creator_id::text FROM issue WHERE id = $1", first.IssueID).Scan(&origin, &assignee, &parent, &creator)
	dbfx.QueryRow(t, "SELECT originator_user_id::text FROM agent_task_queue WHERE issue_id = $1", first.IssueID).Scan(&human)
	if origin != f.task || assignee != f.target || parent != f.parent || creator != f.source || human != testUserID {
		t.Fatalf("wrong native lineage: %s/%s/%s/%s/%s", origin, assignee, parent, creator, human)
	}
	dbfx.Exec(t, "UPDATE agent_task_queue SET status = 'completed' WHERE issue_id = $1", first.IssueID)
	replay := hierarchyReceipt(t, f.call(t, testHandler, f.parent, key, nil, nil), http.StatusOK)
	if !replay.Replayed || replay.IssueID != first.IssueID {
		t.Fatalf("retry receipt = %+v", replay)
	}
	if n := dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id = $1", first.IssueID); n != 1 {
		t.Fatalf("replay scheduled %d tasks", n)
	}
	if w := f.call(t, testHandler, f.parent, key, map[string]any{"title": "different intent"}, nil); w.Code != http.StatusConflict {
		t.Fatalf("intent conflict = %d %s", w.Code, w.Body.String())
	}
	followup := hierarchyReceipt(t, f.call(t, testHandler, f.parent, uuid.NewString(), nil, nil), http.StatusCreated)
	if followup.IssueID == first.IssueID {
		t.Fatal("new identity did not create follow-up")
	}
}

func TestHierarchyDelegationHTTPRejectsMemberAndUntrustedInputs(t *testing.T) {
	f := newHierarchyHTTPFixture(t)
	for _, field := range []string{"delegator_agent_id", "source_task_id", "project_id", "originator_user_id", "status", "allow_duplicate"} {
		t.Run(field, func(t *testing.T) {
			w := f.call(t, testHandler, f.parent, uuid.NewString(), map[string]any{field: uuid.NewString()}, nil)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("untrusted field = %d %s", w.Code, w.Body.String())
			}
		})
	}
	for _, field := range []string{"request_id", "target_agent_id"} {
		w := f.call(t, testHandler, f.parent, uuid.NewString(), map[string]any{field: "bad"}, nil)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("bad UUID %s = %d", field, w.Code)
		}
	}
	pat := "mul_"+uuid.NewString()
	dbfx.Insert(t, "personal_access_token", testutil.Cols{
		"user_id": testUserID, "name": "hierarchy member test", "token_hash": auth.HashToken(pat), "token_prefix": "mul_test",
	})
	w := f.call(t, testHandler, f.parent, uuid.NewString(), nil, func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+pat)
		r.Header.Set("X-Actor-Source", "task_token")
		r.Header.Set("X-Agent-ID", f.source)
		r.Header.Set("X-Task-ID", f.task)
	})
	if w.Code != http.StatusForbidden {
		t.Fatalf("forged member = %d %s", w.Code, w.Body.String())
	}
	if n := dbfx.Count(t, "SELECT count(*) FROM project_hierarchy_delegation WHERE parent_issue_id = $1", f.parent); n != 0 {
		t.Fatalf("denial left %d identities", n)
	}
}

type hierarchyAdmissionTxStarter struct {
	pool *pgxpool.Pool
	before func()
}

func (s hierarchyAdmissionTxStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	s.before()
	return s.pool.Begin(ctx)
}

func TestHierarchyDelegationHTTPRechecksAdmissionInsideCreate(t *testing.T) {
	for _, name := range []string{"revoked token", "expired token", "completed task", "rebound source", "unbound target", "runtime unusable", "private target", "different originator", "squad task"} {
		t.Run(name, func(t *testing.T) {
			f := newHierarchyHTTPFixture(t)
			// Revoke/change after Auth, actor resolution and parent loading,
			// immediately before native Create begins its final transaction.
			h := *testHandler
			svc := *testHandler.IssueService
			h.IssueService = &svc
			svc.TxStarter = hierarchyAdmissionTxStarter{pool: testPool, before: func() {
				switch name {
				case "revoked token":
					dbfx.Exec(t, "DELETE FROM task_token WHERE token_hash = $1", auth.HashToken(f.token))
				case "expired token":
					dbfx.Exec(t, "UPDATE task_token SET expires_at = now() - interval '1 second' WHERE token_hash = $1", auth.HashToken(f.token))
				case "completed task":
					dbfx.Exec(t, "UPDATE agent_task_queue SET status = 'completed' WHERE id = $1", f.task)
				case "rebound source":
					dbfx.Exec(t, "UPDATE agent SET runtime_id = NULL WHERE id = $1", f.source)
				case "unbound target":
					dbfx.Exec(t, "UPDATE agent SET runtime_id = NULL WHERE id = $1", f.target)
				case "runtime unusable":
					dbfx.Exec(t, `UPDATE agent_runtime SET status = 'offline', metadata = '{"offline_reason":{"code":"not_executable"}}' WHERE id = $1`, f.runtime)
				case "private target":
					other := dbfx.User(t, "other hierarchy owner", uuid.NewString()+"@multica.test")
					dbfx.Exec(t, "UPDATE agent SET owner_id = $1 WHERE id = $2", other, f.target)
				case "different originator":
					other := dbfx.User(t, "different originator", uuid.NewString()+"@multica.test")
					dbfx.Exec(t, "UPDATE agent_task_queue SET originator_user_id = $1, accountable_user_id = $1 WHERE id = $2", other, f.task)
				case "squad task":
					squad := dbfx.Squad(t, "hierarchy exclusion", f.source)
					dbfx.Exec(t, "UPDATE agent_task_queue SET squad_id = $1 WHERE id = $2", squad, f.task)
				}
			}}
			w := f.call(t, &h, f.parent, uuid.NewString(), nil, nil)
			if w.Code != http.StatusForbidden {
				t.Fatalf("changed admission = %d %s", w.Code, w.Body.String())
			}
			if strings.Contains(w.Body.String(), "private") || strings.Contains(w.Body.String(), "SELECT") {
				t.Fatalf("denial leaked configuration: %s", w.Body.String())
			}
			if n := dbfx.Count(t, "SELECT count(*) FROM issue WHERE parent_issue_id = $1", f.parent); n != 0 {
				t.Fatalf("denial created %d children", n)
			}
		})
	}
}

func TestHierarchyDelegationHTTPReplayStillRequiresCurrentPermission(t *testing.T) {
	f := newHierarchyHTTPFixture(t)
	key := uuid.NewString()
	hierarchyReceipt(t, f.call(t, testHandler, f.parent, key, nil, nil), http.StatusCreated)
	other := dbfx.User(t, "hierarchy replay other", uuid.NewString()+"@multica.test")
	dbfx.Exec(t, "UPDATE agent SET owner_id = $1 WHERE id = $2", other, f.target)
	w := f.call(t, testHandler, f.parent, key, nil, nil)
	if w.Code != http.StatusForbidden {
		t.Fatalf("replay bypassed invocation = %d %s", w.Code, w.Body.String())
	}
}


func TestHierarchyDelegationHTTPUsesNativeInvocationTargets(t *testing.T) {
	for _, mode := range []string{"member originator", "workspace unattributed"} {
		t.Run(mode, func(t *testing.T) {
			f := newHierarchyHTTPFixture(t)
			dbfx.Exec(t, "UPDATE agent SET permission_mode = 'public_to' WHERE id = $1", f.target)
			targetType, targetID := "workspace", testWorkspaceID
			if mode == "member originator" {
				human := dbfx.User(t, "top-of-chain human", uuid.NewString()+"@multica.test")
				dbfx.Member(t, testWorkspaceID, human, "member")
				dbfx.Exec(t, "UPDATE agent_task_queue SET originator_user_id = $1, accountable_user_id = $1 WHERE id = $2", human, f.task)
				targetType, targetID = "member", human
				// The token user owns the target; only the actual originator
				// matters, so absence of their grant must still deny.
				w := f.call(t, testHandler, f.parent, uuid.NewString(), nil, nil)
				if w.Code != http.StatusForbidden {
					t.Fatalf("borrowed token-user ownership: %d %s", w.Code, w.Body.String())
				}
			} else {
				dbfx.Exec(t, "UPDATE agent_task_queue SET originator_user_id = NULL WHERE id = $1", f.task)
			}
			dbfx.InsertNoID(t, "agent_invocation_target", testutil.Cols{
				"agent_id": f.target, "target_type": targetType, "target_id": targetID,
			}, "agent_id = $1", f.target)
			receipt := hierarchyReceipt(t, f.call(t, testHandler, f.parent, uuid.NewString(), nil, nil), http.StatusCreated)
			if receipt.Replayed {
				t.Fatal("first native allow-list invocation replayed")
			}
		})
	}
}

func TestHierarchyDelegationHTTPAllowsNativeWaitableRuntime(t *testing.T) {
	f := newHierarchyHTTPFixture(t)
	dbfx.Exec(t, "UPDATE agent_runtime SET status = 'offline' WHERE id = $1", f.runtime)
	hierarchyReceipt(t, f.call(t, testHandler, f.parent, uuid.NewString(), nil, nil), http.StatusCreated)
}
