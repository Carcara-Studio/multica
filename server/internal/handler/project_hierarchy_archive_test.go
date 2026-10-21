package handler

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func archiveHierarchyAgent(t *testing.T, id string, status int) *httptest.ResponseRecorder {
	t.Helper()
	req := withURLParam(newRequest(http.MethodPost, "/api/agents/"+id+"/archive", nil), "id", id)
	w := httptest.NewRecorder()
	testHandler.ArchiveAgent(w, req)
	if w.Code != status {
		t.Fatalf("archive status=%d want=%d: %s", w.Code, status, w.Body.String())
	}
	return w
}

func TestHierarchyAgentArchiveStructuralConflictRollsBack(t *testing.T) {
	for _, role := range []string{"root", "manager", "mixed projects"} {
		t.Run(role, func(t *testing.T) {
			p := hierarchyAPIProject(t)
			rootAgent := hierarchyAPIAgent(t, "root")
			root := createHierarchyMembershipViaAPI(t, p, rootAgent, nil, "Root")
			victim := rootAgent
			blocker := root
			if role != "root" {
				victim = hierarchyAPIAgent(t, "manager")
				blocker = createHierarchyMembershipViaAPI(t, p, victim, &root.ID, "Manager")
				createHierarchyMembershipViaAPI(t, p, hierarchyAPIAgent(t, "worker"), &blocker.ID, "Worker")
			}
			if role == "mixed projects" {
				other := hierarchyAPIProject(t)
				r := createHierarchyMembershipViaAPI(t, other, hierarchyAPIAgent(t, "other root"), nil, "Root")
				createHierarchyMembershipViaAPI(t, other, victim, &r.ID, "Leaf elsewhere")
			}
			task := dbfx.Task(t, victim)
			w := archiveHierarchyAgent(t, victim, http.StatusConflict)
			var response struct {
				Code     string `json:"code"`
				Blockers []struct {
					ProjectID    string `json:"project_id"`
					MembershipID string `json:"membership_id"`
				} `json:"blockers"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if response.Code != "hierarchy_archive_blocked" || len(response.Blockers) != 1 || response.Blockers[0].ProjectID != p || response.Blockers[0].MembershipID != blocker.ID {
				t.Fatalf("conflict=%s", w.Body.String())
			}
			if n := dbfx.Count(t, `SELECT count(*) FROM agent WHERE id=$1 AND archived_at IS NULL`, victim); n != 1 {
				t.Fatal("blocked Agent archived")
			}
			if n := dbfx.Count(t, `SELECT count(*) FROM project_agent_membership WHERE agent_id=$1 AND archived_at IS NOT NULL`, victim); n != 0 {
				t.Fatal("blocked archive deactivated memberships")
			}
			if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE id=$1 AND status='cancelled'`, task); n != 0 {
				t.Fatal("blocked archive cancelled task")
			}
		})
	}
}

func TestHierarchyAgentArchiveLeavesAndRestore(t *testing.T) {
	victim := hierarchyAPIAgent(t, "leaf")
	var memberships []string
	for range 2 {
		p := hierarchyAPIProject(t)
		root := createHierarchyMembershipViaAPI(t, p, hierarchyAPIAgent(t, "root"), nil, "Root")
		m := createHierarchyMembershipViaAPI(t, p, victim, &root.ID, "Leaf")
		memberships = append(memberships, m.ID)
	}
	archiveHierarchyAgent(t, victim, http.StatusOK)
	if n := dbfx.Count(t, `SELECT count(*) FROM project_agent_membership WHERE agent_id=$1 AND archived_at IS NOT NULL`, victim); n != 2 {
		t.Fatalf("archived memberships=%d", n)
	}
	req := withURLParam(newRequest(http.MethodPost, "/api/agents/"+victim+"/restore", nil), "id", victim)
	testutil.Call(t, testHandler.RestoreAgent, req).Want(http.StatusOK)
	if n := dbfx.Count(t, `SELECT count(*) FROM project_agent_membership WHERE agent_id=$1 AND archived_at IS NULL`, victim); n != 0 {
		t.Fatal("restore reactivated memberships")
	}
	// Re-entry remains explicit, using the existing membership identity.
	var p, manager string
	dbfx.QueryRow(t, `SELECT project_id::text,manager_membership_id::text FROM project_agent_membership WHERE id=$1`, memberships[0]).Scan(&p, &manager)
	m := createHierarchyMembershipViaAPI(t, p, victim, &manager, "Returned")
	if m.ID != memberships[0] {
		t.Fatal("explicit re-entry replaced identity")
	}
}

func TestHierarchyAgentArchiveMemberRemovalIsAtomic(t *testing.T) {
	for _, leave := range []bool{false, true} {
		for _, blocked := range []bool{false, true} {
			name := uuid.NewString()
			t.Run(name, func(t *testing.T) {
				f := setupRevocationFixture(t, "hier-"+name, "hier-daemon-"+name)
				// This Workspace is independent of the default handler fixture.
				p := dbfx.Project(t, "Revocation hierarchy", testutil.Cols{"workspace_id": f.WorkspaceID})
				dbfx.Cleanup(t, `DELETE FROM project_agent_membership WHERE workspace_id=$1`, f.WorkspaceID)
				rootAgent := dbfx.Agent(t, "Surviving root", "", testutil.Cols{"workspace_id": f.WorkspaceID})
				var root, leaf string
				dbfx.QueryRow(t, `INSERT INTO project_agent_membership(workspace_id,project_id,agent_id) VALUES($1,$2,$3) RETURNING id::text`, f.WorkspaceID, p, rootAgent).Scan(&root)
				dbfx.QueryRow(t, `INSERT INTO project_agent_membership(workspace_id,project_id,agent_id,manager_membership_id) VALUES($1,$2,$3,$4) RETURNING id::text`, f.WorkspaceID, p, f.AgentID, root).Scan(&leaf)
				if blocked {
					other := dbfx.Agent(t, "Blocking manager", f.RuntimeID, testutil.Cols{"workspace_id": f.WorkspaceID, "owner_id": f.TargetUserID})
					next := dbfx.Project(t, "Blocking hierarchy", testutil.Cols{"workspace_id": f.WorkspaceID})
					dbfx.Exec(t, `INSERT INTO project_agent_membership(workspace_id,project_id,agent_id) VALUES($1,$2,$3)`, f.WorkspaceID, next, other)
				}
				req := newRequest(http.MethodDelete, "/api/workspaces/"+f.WorkspaceID+"/members/"+f.MemberID, nil)
				req.Header.Set("X-Workspace-ID", f.WorkspaceID)
				req = withURLParams(req, "id", f.WorkspaceID, "memberId", f.MemberID)
				handler := testHandler.DeleteMember
				if leave {
					req.Header.Set("X-User-ID", f.TargetUserID)
					handler = testHandler.LeaveWorkspace
				}
				status := http.StatusNoContent
				if blocked {
					status = http.StatusConflict
				}
				testutil.Call(t, handler, req).Want(status)
				if !blocked {
					assertRevoked(t, f)
					if n := dbfx.Count(t, `SELECT count(*) FROM project_agent_membership WHERE id=$1 AND archived_at IS NOT NULL`, leaf); n != 1 {
						t.Fatal("revocation did not deactivate leaf")
					}
				} else {
					if n := dbfx.Count(t, `SELECT count(*) FROM member WHERE id=$1`, f.MemberID); n != 1 {
						t.Fatal("blocked member removed")
					}
					if n := dbfx.Count(t, `SELECT count(*) FROM agent WHERE runtime_id=$1 AND archived_at IS NOT NULL`, f.RuntimeID); n != 0 {
						t.Fatal("blocked batch partially archived")
					}
					if n := dbfx.Count(t, `SELECT count(*) FROM project_agent_membership WHERE id=$1 AND archived_at IS NULL`, leaf); n != 1 {
						t.Fatal("blocked batch deactivated leaf")
					}
					if n := dbfx.Count(t, `SELECT count(*) FROM daemon_token WHERE token_hash=$1`, f.TokenHash); n != 1 {
						t.Fatal("blocked batch revoked token")
					}
					if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE id=$1 AND status='cancelled'`, f.TaskID); n != 0 {
						t.Fatal("blocked batch cancelled task")
					}
				}
			})
		}
	}
}

func TestHierarchyAgentArchiveLeafRollbackOnInvalidationFailure(t *testing.T) {
	fx, victim, _, _ := probingAgentForMutation(t, nil)
	p := hierarchyAPIProject(t)
	root := createHierarchyMembershipViaAPI(t, p, hierarchyAPIAgent(t, "root"), nil, "Root")
	leaf := createHierarchyMembershipViaAPI(t, p, victim, &root.ID, "Leaf")
	fx.Exec(t, `UPDATE agent_failover_state SET version=$1 WHERE agent_id=$2`, int64(math.MaxInt64), victim)
	archiveHierarchyAgent(t, victim, http.StatusInternalServerError)
	if n := fx.Count(t, `SELECT count(*) FROM agent WHERE id=$1 AND archived_at IS NULL`, victim); n != 1 {
		t.Fatal("failed archive committed Agent change")
	}
	if n := fx.Count(t, `SELECT count(*) FROM project_agent_membership WHERE id=$1 AND archived_at IS NULL`, leaf.ID); n != 1 {
		t.Fatal("failed archive committed leaf deactivation")
	}
}
