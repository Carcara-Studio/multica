package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestHierarchyDelegationChildCompletionResumesManagerAndReplays(t *testing.T) {
	f := newHierarchyHTTPFixture(t)
	t.Cleanup(func() { cleanupChildDoneIssue(f.parent) })
	dbfx.Exec(t, "UPDATE issue SET status = 'in_progress' WHERE id = $1", f.parent)
	key := uuid.NewString()
	child := hierarchyReceipt(t, f.call(t, testHandler, f.parent, key, nil, nil), http.StatusCreated)
	t.Cleanup(func() { cleanupChildDoneIssue(child.IssueID) })
	if child.Replayed {
		t.Fatal("new delegation was marked as replayed")
	}
	replay := hierarchyReceipt(t, f.call(t, testHandler, f.parent, key, nil, nil), http.StatusOK)
	if !replay.Replayed || replay.IssueID != child.IssueID {
		t.Fatalf("initial retry changed the child: %+v", replay)
	}
	complete := func(taskID, output string) {
		t.Helper()
		req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/tasks/"+taskID+"/complete",
			map[string]any{"output": output}, testWorkspaceID, "hierarchy-lifecycle-fixture")
		req = withURLParam(req, "taskId", taskID)
		var terminal AgentTaskResponse
		testutil.Call(t, testHandler.CompleteTask, req).Want(http.StatusOK).JSON(&terminal)
		if terminal.ID != taskID || terminal.Status != "completed" {
			t.Fatalf("task did not complete: id=%s status=%s", terminal.ID, terminal.Status)
		}
		if n := dbfx.Count(t, "SELECT count(*) FROM task_token WHERE task_id = $1", taskID); n != 0 {
			t.Fatalf("completed task retained %d task tokens", n)
		}
	}
	claim := func(issueID, agentID string) AgentTaskResponse {
		t.Helper()
		id, _, leader, raw := claimAgentInstructionsForTest(t, f.runtime)
		var envelope struct {
			Task *AgentTaskResponse `json:"task"`
		}
		if err := json.Unmarshal([]byte(raw), &envelope); err != nil || envelope.Task == nil {
			t.Fatalf("missing native claim: %v", err)
		}
		task := *envelope.Task
		if id == "" || task.ID != id || task.IssueID != issueID || task.AgentID != agentID ||
			task.RuntimeID != f.runtime || task.WorkspaceID != testWorkspaceID || leader || task.SquadID != "" {
			t.Fatalf("claim changed ownership or introduced Squad routing: task=%s agent=%s issue=%s runtime=%s leader=%v squad=%s",
				task.ID, task.AgentID, task.IssueID, task.RuntimeID, leader, task.SquadID)
		}
		if !strings.HasPrefix(task.AuthToken, "mat_") {
			t.Fatal("native claim omitted its task token")
		}
		if !task.StartClaimSupported || task.DispatchedAt == nil || task.Status != "dispatched" {
			t.Fatal("native claim omitted its start generation")
		}
		// A claim reserves the run; the daemon must acknowledge this exact
		// generation before its task is running or its MAT can delegate.
		req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/tasks/"+task.ID+"/start",
			map[string]string{"runtime_id": task.RuntimeID, "dispatched_at": *task.DispatchedAt},
			testWorkspaceID, "hierarchy-lifecycle-fixture")
		req = withURLParam(req, "taskId", task.ID)
		var started AgentTaskResponse
		testutil.Call(t, testHandler.StartTask, req).Want(http.StatusOK).JSON(&started)
		if started.ID != task.ID || started.Status != "running" {
			t.Fatalf("claimed task did not start: id=%s status=%s", started.ID, started.Status)
		}
		return task
	}
	// The manager yields normally. Completion revokes its old MAT; the next
	// authorized turn must use the token minted by its own native claim.
	complete(f.task, "Waiting for the delegated child.")
	if w := f.call(t, testHandler, f.parent, key, nil, nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("completed manager MAT remained usable: status=%d", w.Code)
	}
	childTask := claim(child.IssueID, f.target)
	// Exercise the child's real task credential through the normal Issue
	// status route, retaining the native child_done receipt and wakeup path.
	router := chi.NewRouter()
	router.Use(middleware.Auth(testHandler.Queries, nil, nil, nil))
	router.Use(middleware.RequireWorkspaceMember(testHandler.Queries))
	router.Put("/api/issues/{id}", testHandler.UpdateIssue)
	req := newRequest(http.MethodPut, "/api/issues/"+child.IssueID, map[string]any{"status": "done"})
	req.Header.Set("Authorization", "Bearer "+childTask.AuthToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("child status completion: %d %s", w.Code, w.Body.String())
	}
	complete(childTask.ID, "Delegated work completed.")
	runWakeupTick(t)
	runs := childDoneRuns(t, f.parent)
	if len(runs) != 1 || runs[0].AgentID != f.source || runs[0].Leader || runs[0].SquadID != "" || runs[0].WakeupID == "" {
		t.Fatalf("completion did not produce one native manager wakeup: %+v", runs)
	}
	for _, fact := range []string{service.ChildDoneDefaultInstruction, "condition.met", `"all":true`} {
		if !strings.Contains(runs[0].Note, fact) {
			t.Fatalf("manager wakeup omitted %q", fact)
		}
	}
	managerTask := claim(f.parent, f.source)
	if managerTask.ID != runs[0].ID || managerTask.ID == f.task {
		t.Fatal("manager did not resume through the native child_done run")
	}
	later := f
	later.task, later.token = managerTask.ID, managerTask.AuthToken
	replay = hierarchyReceipt(t, later.call(t, testHandler, f.parent, key, nil, nil), http.StatusOK)
	if !replay.Replayed || replay.IssueID != child.IssueID {
		t.Fatalf("later-turn retry recreated or changed completed child: %+v", replay)
	}
	updateChildStatus(t, child.IssueID, "done")
	runWakeupTick(t)
	if n := dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id = $1", child.IssueID); n != 1 {
		t.Fatalf("delegation retries scheduled %d child tasks", n)
	}
	if n := dbfx.Count(t, "SELECT count(*) FROM issue WHERE parent_issue_id = $1", f.parent); n != 1 {
		t.Fatalf("delegation retries created %d children", n)
	}
	if runs := childDoneRuns(t, f.parent); len(runs) != 1 {
		t.Fatalf("same completion produced duplicate manager wakeups: %+v", runs)
	}
	entries := childDoneEntries(t, f.parent)
	if len(entries) != 1 || entries[0].Outcome != "woke" || entries[0].TargetID != f.source || entries[0].TaskID != managerTask.ID {
		t.Fatalf("native completion timeline lost its manager/run lineage: %+v", entries)
	}
	var parentStatus string
	dbfx.QueryRow(t, "SELECT status FROM issue WHERE id = $1", f.parent).Scan(&parentStatus)
	if parentStatus != "in_progress" {
		t.Fatalf("child completion changed parent status to %s", parentStatus)
	}
}
