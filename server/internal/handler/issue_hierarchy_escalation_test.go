package handler

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestHierarchyEscalationNativeAuthorization(t *testing.T) {
	for _, root := range []bool{false, true} {
		name := "manager"
		if root {
			name = "root"
		}
		t.Run(name, func(t *testing.T) {
			f := newHierarchyHTTPFixture(t)
			manager := f.source
			if !root {
				f.source = f.target
				dbfx.Exec(t, "UPDATE issue SET assignee_id=$1 WHERE id=$2", f.source, f.parent)
				dbfx.Exec(t, "UPDATE agent_task_queue SET agent_id=$1 WHERE id=$2", f.source, f.task)
				f.token = f.newToken(t, f.task)
			}
			dbfx.Cleanup(t, "DELETE FROM project_hierarchy_escalation WHERE source_issue_id=$1", f.parent)
			dbfx.Cleanup(t, "DELETE FROM inbox_item WHERE issue_id IN (SELECT decision_issue_id FROM project_hierarchy_escalation WHERE source_issue_id=$1)", f.parent)
			dbfx.Cleanup(t, "DELETE FROM agent_task_queue WHERE issue_id IN (SELECT decision_issue_id FROM project_hierarchy_escalation WHERE source_issue_id=$1)", f.parent)
			hash := auth.HashToken(f.token)
			token, err := testHandler.Queries.GetTaskTokenByHash(context.Background(), hash)
			if err != nil {
				t.Fatal(err)
			}
			var project string
			dbfx.QueryRow(t, "SELECT project_id::text FROM issue WHERE id=$1", f.parent).Scan(&project)
			in := service.HierarchyEscalationCreate{WorkspaceID: parseUUID(testWorkspaceID), ProjectID: parseUUID(project), IssueID: parseUUID(f.parent), SourceAgentID: parseUUID(f.source), SourceTaskID: parseUUID(f.task), RequestID: parseUUID(uuid.NewString()), Reason: "Decision required", Authorize: testHandler.hierarchyEscalationAuthorization(token, hash, parseUUID(f.parent))}
			first, err := testHandler.IssueService.Escalate(context.Background(), in, service.IssueCreateOpts{})
			if err != nil {
				t.Fatal(err)
			}
			if root {
				if first.Issue.AssigneeID.Valid {
					t.Fatal("root invoked an Agent")
				}
			} else if first.Issue.AssigneeID != parseUUID(manager) {
				t.Fatal("manager not derived from hierarchy")
			}
			// A new turn can reuse the request; its fresh MAT must be authorized.
			dbfx.Exec(t, "UPDATE agent_task_queue SET status='completed' WHERE id=$1", f.task)
			if _, err = testHandler.IssueService.Escalate(context.Background(), in, service.IssueCreateOpts{}); err == nil {
				t.Fatal("stopped source task admitted")
			}
			next := dbfx.Task(t, f.source, testutil.Cols{"runtime_id": f.runtime, "issue_id": f.parent, "status": "running", "originator_user_id": testUserID, "accountable_user_id": testUserID})
			fresh := f.newToken(t, next)
			freshHash := auth.HashToken(fresh)
			freshToken, err := testHandler.Queries.GetTaskTokenByHash(context.Background(), freshHash)
			if err != nil {
				t.Fatal(err)
			}
			in.SourceTaskID = parseUUID(next)
			in.Authorize = testHandler.hierarchyEscalationAuthorization(freshToken, freshHash, in.IssueID)
			replay, err := testHandler.IssueService.Escalate(context.Background(), in, service.IssueCreateOpts{})
			if err != nil || !replay.EscalationReplayed || replay.Issue.ID != first.Issue.ID {
				t.Fatalf("fresh MAT replay: %+v / %v", replay, err)
			}
			dbfx.Exec(t, "UPDATE task_token SET expires_at=$1 WHERE token_hash=$2", time.Now().Add(-time.Hour), freshHash)
			if _, err = testHandler.IssueService.Escalate(context.Background(), in, service.IssueCreateOpts{}); err == nil {
				t.Fatal("expired MAT admitted on replay")
			}
			dbfx.Exec(t, "UPDATE task_token SET expires_at=$1 WHERE token_hash=$2", time.Now().Add(time.Hour), freshHash)
			if !root {
				other := dbfx.User(t, "other escalation originator", uuid.NewString()+"@multica.test")
				dbfx.Member(t, testWorkspaceID, other, "member")
				dbfx.Exec(t, "UPDATE agent_task_queue SET originator_user_id=$1,accountable_user_id=$1 WHERE id=$2", other, next)
				if _, err = testHandler.IssueService.Escalate(context.Background(), in, service.IssueCreateOpts{}); err == nil {
					t.Fatal("token-user ownership bypassed actual originator permission")
				}
				dbfx.Exec(t, "UPDATE agent SET permission_mode='public_to' WHERE id=$1", manager)
				dbfx.InsertNoID(t, "agent_invocation_target", testutil.Cols{"agent_id": manager, "target_type": "member", "target_id": other}, "agent_id=$1", manager)
				if _, err = testHandler.IssueService.Escalate(context.Background(), in, service.IssueCreateOpts{}); err != nil {
					t.Fatalf("native member allow-list refused originator: %v", err)
				}
				dbfx.Exec(t, "UPDATE agent SET runtime_id=NULL WHERE id=$1", manager)
				if _, err = testHandler.IssueService.Escalate(context.Background(), in, service.IssueCreateOpts{}); err == nil {
					t.Fatal("unavailable manager bypassed on replay")
				}
			}
			dbfx.Exec(t, "DELETE FROM task_token WHERE token_hash=$1", freshHash)
			if _, err = testHandler.IssueService.Escalate(context.Background(), in, service.IssueCreateOpts{}); err == nil {
				t.Fatal("revoked MAT admitted")
			}
		})
	}
}
