package handler

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/projectmemory"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestProjectContinuityDraftSourceIsolationAndNoPublication(t *testing.T) {
	project := dbfx.Project(t, "Draft source")
	other := dbfx.Project(t, "Other draft source")
	runtime := dbfx.Runtime(t, "Draft runtime")
	agent := dbfx.Agent(t, "Draft agent", runtime)
	issue := dbfx.Issue(t, "Draft issue", testutil.Cols{"project_id": project})
	task := dbfx.Task(t, agent, testutil.Cols{"runtime_id": runtime, "issue_id": issue, "status": "completed", "completed_at": time.Now(), "result": `{"output":"Preserve opaque IDs","session_path":"must-not-leak"}`})
	get := func() projectmemory.Draft {
		t.Helper()
		var draft projectmemory.Draft
		testutil.Call(t, testHandler.GetProjectContinuityDraft, continuityRequest(project, "GET", nil)).Want(200).JSON(&draft)
		if !draft.Valid() {
			t.Fatalf("invalid draft: %+v", draft)
		}
		return draft
	}
	if get().Available {
		t.Fatal("legacy task without captured scope accepted")
	}
	setScope := func(p, s string) {
		raw, _ := json.Marshal(map[string]any{"v": 1, "continuity_scope": map[string]string{"project_id": p, "squad_id": s}})
		dbfx.Exec(t, `UPDATE agent_task_queue SET issue_snapshot=$2::jsonb WHERE id=$1`, task, string(raw))
	}
	setScope(other, "")
	if get().Available {
		t.Fatal("moved project report accepted")
	}
	setScope(project, uuid.NewString())
	if get().Available {
		t.Fatal("squad report promoted to project")
	}
	setScope(project, "")
	draft := get()
	if !draft.Available || draft.Source.TaskID != task || draft.Current.Content != "" || draft.Current.Revision != "" {
		t.Fatalf("wrong source: %+v", draft)
	}
	var checkpoint projectmemory.Checkpoint
	testutil.Call(t, testHandler.GetProjectContinuity, continuityRequest(project, "GET", nil)).Want(200).JSON(&checkpoint)
	if checkpoint.Content != "" || checkpoint.Revision != "" {
		t.Fatal("read published draft")
	}
	dbfx.Exec(t, `UPDATE agent_task_queue SET status='failed' WHERE id=$1`, task)
	if get().Available {
		t.Fatal("failed task accepted")
	}
}

func TestProjectContinuityDraftClaimCapturesResolvedScope(t *testing.T) {
	project := dbfx.Project(t, "Captured continuity scope")
	var agent, runtime string
	dbfx.QueryRow(t, `SELECT id,runtime_id FROM agent WHERE workspace_id=$1 AND runtime_id IS NOT NULL LIMIT 1`, testWorkspaceID).Scan(&agent, &runtime)
	issue := dbfx.Issue(t, "Capture scope on claim", testutil.Cols{"project_id": project})
	task := dbfx.Task(t, agent, testutil.Cols{"runtime_id": runtime, "issue_id": issue})
	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtime+"/claim", nil, testWorkspaceID, "continuity-draft-capture")
	req = withURLParam(req, "runtimeId", runtime)
	testutil.Call(t, testHandler.ClaimTaskByRuntime, req).Want(200)
	var raw []byte
	dbfx.QueryRow(t, `SELECT issue_snapshot FROM agent_task_queue WHERE id=$1`, task).Scan(&raw)
	snapshot, ok := decodeIssueStateSnapshot(raw)
	if !ok || snapshot.ContinuityScope == nil || snapshot.ContinuityScope.ProjectID != project || snapshot.ContinuityScope.SquadID != "" {
		t.Fatalf("claim did not persist scope: %s", raw)
	}
	old := snapshot
	old.ContinuityScope = nil
	if len(snapshot.changedFieldsSince(old)) != 0 {
		t.Fatal("scope changed issue body comparison")
	}
	dbfx.Exec(t, `UPDATE agent_task_queue SET status='completed',completed_at=now(),result='{"output":"captured final report"}'::jsonb WHERE id=$1`, task)
	var draft projectmemory.Draft
	testutil.Call(t, testHandler.GetProjectContinuityDraft, continuityRequest(project, "GET", nil)).Want(200).JSON(&draft)
	if !draft.Valid() || !draft.Available || draft.Source.TaskID != task {
		t.Fatalf("claimed report unavailable: %+v", draft)
	}
}
