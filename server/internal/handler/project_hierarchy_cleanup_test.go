package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func insertHierarchyTestProject(t *testing.T, workspaceID, title string) string {
	t.Helper()
	var projectID string
	if err := testPool.QueryRow(context.Background(), `
INSERT INTO project (workspace_id, title)
VALUES ($1, $2)
RETURNING id
`, workspaceID, title).Scan(&projectID); err != nil {
		t.Fatalf("create hierarchy test project: %v", err)
	}
	return projectID
}

func insertHierarchyTestMembership(t *testing.T, workspaceID, projectID string) string {
	t.Helper()
	var membershipID string
	if err := testPool.QueryRow(context.Background(), `
INSERT INTO project_agent_membership (workspace_id, project_id, agent_id)
VALUES ($1, $2, gen_random_uuid())
RETURNING id
`, workspaceID, projectID).Scan(&membershipID); err != nil {
		t.Fatalf("create hierarchy test membership: %v", err)
	}
	return membershipID
}

func hierarchyMembershipExists(t *testing.T, membershipID string) bool {
	t.Helper()
	var exists bool
	if err := testPool.QueryRow(context.Background(), `
SELECT EXISTS (SELECT 1 FROM project_agent_membership WHERE id = $1)
`, membershipID).Scan(&exists); err != nil {
		t.Fatalf("check hierarchy membership %s: %v", membershipID, err)
	}
	return exists
}

func TestDeleteProjectCleansHierarchyMembershipsAndPreservesOtherProject(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	victimProject := insertHierarchyTestProject(t, testWorkspaceID, "Hierarchy delete victim")
	survivorProject := insertHierarchyTestProject(t, testWorkspaceID, "Hierarchy delete survivor")
	victimMembership := insertHierarchyTestMembership(t, testWorkspaceID, victimProject)
	survivorMembership := insertHierarchyTestMembership(t, testWorkspaceID, survivorProject)

	t.Cleanup(func() {
		bg := context.Background()
		_, _ = testPool.Exec(bg, `DELETE FROM project_agent_membership WHERE id = ANY($1::uuid[])`, []string{victimMembership, survivorMembership})
		_, _ = testPool.Exec(bg, `DELETE FROM project WHERE id = ANY($1::uuid[])`, []string{victimProject, survivorProject})
	})

	w := httptest.NewRecorder()
	req := newRequest(http.MethodDelete, "/api/projects/"+victimProject, nil)
	req = withURLParam(req, "id", victimProject)
	testHandler.DeleteProject(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteProject = %d, want 204: %s", w.Code, w.Body.String())
	}

	if hierarchyMembershipExists(t, victimMembership) {
		t.Fatal("victim hierarchy membership survived Project deletion")
	}
	if !hierarchyMembershipExists(t, survivorMembership) {
		t.Fatal("another Project's hierarchy membership was deleted")
	}

	var survivorProjectExists bool
	if err := testPool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM project WHERE id = $1)`, survivorProject).Scan(&survivorProjectExists); err != nil {
		t.Fatalf("check survivor Project: %v", err)
	}
	if !survivorProjectExists {
		t.Fatal("another Project was deleted while cleaning hierarchy memberships")
	}
}

func TestDeleteWorkspaceCleansHierarchyMembershipsAndPreservesOtherWorkspace(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	suffix := time.Now().UnixNano()
	slug := fmt.Sprintf("handler-tests-hierarchy-delete-%d", suffix)

	var victimWorkspace string
	if err := testPool.QueryRow(ctx, `
INSERT INTO workspace (name, slug)
VALUES ($1, $2)
RETURNING id
`, "Hierarchy Delete Victim", slug).Scan(&victimWorkspace); err != nil {
		t.Fatalf("create victim Workspace: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
INSERT INTO member (workspace_id, user_id, role)
VALUES ($1, $2, 'owner')
`, victimWorkspace, testUserID); err != nil {
		t.Fatalf("create victim Workspace owner: %v", err)
	}

	victimProject := insertHierarchyTestProject(t, victimWorkspace, "Hierarchy workspace victim")
	victimMembership := insertHierarchyTestMembership(t, victimWorkspace, victimProject)
	survivorProject := insertHierarchyTestProject(t, testWorkspaceID, "Hierarchy workspace survivor")
	survivorMembership := insertHierarchyTestMembership(t, testWorkspaceID, survivorProject)

	t.Cleanup(func() {
		bg := context.Background()
		_, _ = testPool.Exec(bg, `DELETE FROM project_agent_membership WHERE id = ANY($1::uuid[])`, []string{victimMembership, survivorMembership})
		_, _ = testPool.Exec(bg, `DELETE FROM project WHERE id = ANY($1::uuid[])`, []string{victimProject, survivorProject})
		_, _ = testPool.Exec(bg, `DELETE FROM workspace WHERE id = $1`, victimWorkspace)
	})

	w := httptest.NewRecorder()
	req := newRequest(http.MethodDelete, "/api/workspaces/"+victimWorkspace, nil)
	req.Header.Set("X-Workspace-ID", victimWorkspace)
	req = withURLParam(req, "id", victimWorkspace)
	testHandler.DeleteWorkspace(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteWorkspace = %d, want 204: %s", w.Code, w.Body.String())
	}

	if hierarchyMembershipExists(t, victimMembership) {
		t.Fatal("victim hierarchy membership survived Workspace deletion")
	}
	if !hierarchyMembershipExists(t, survivorMembership) {
		t.Fatal("another Workspace's hierarchy membership was deleted")
	}

	var victimWorkspaceExists bool
	if err := testPool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM workspace WHERE id = $1)`, victimWorkspace).Scan(&victimWorkspaceExists); err != nil {
		t.Fatalf("check victim Workspace: %v", err)
	}
	if victimWorkspaceExists {
		t.Fatal("victim Workspace still exists after successful deletion")
	}
}
