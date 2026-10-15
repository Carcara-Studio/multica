package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func hierarchyAPIProject(t *testing.T) string {
	t.Helper()
	projectID := dbfx.Project(t, "Hierarchy API "+uuid.NewString())
	// project_agent_membership deliberately has no FK/cascade. Delete rows
	// before the Fixture removes the Project so this test leaves no history.
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM project_agent_membership WHERE project_id = $1::uuid`, projectID)
	})
	return projectID
}

func hierarchyAPIAgent(t *testing.T, name string) string {
	t.Helper()
	return dbfx.Agent(t, name+" "+uuid.NewString(), "")
}

func createHierarchyMembershipViaAPI(
	t *testing.T,
	projectID string,
	agentID string,
	managerID *string,
	title string,
) ProjectHierarchyMembershipResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest(http.MethodPost, "/api/projects/"+projectID+"/hierarchy/memberships", CreateProjectHierarchyMembershipRequest{
		AgentID:             agentID,
		ManagerMembershipID: managerID,
		Title:               title,
	})
	req = withURLParam(req, "id", projectID)
	testHandler.CreateProjectHierarchyMembership(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create hierarchy membership: status=%d body=%s", w.Code, w.Body.String())
	}
	var response ProjectHierarchyMembershipResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode hierarchy membership: %v", err)
	}
	return response
}

func TestProjectHierarchyAPIGetEmptyAndActiveOnly(t *testing.T) {
	projectID := hierarchyAPIProject(t)

	w := httptest.NewRecorder()
	req := newRequest(http.MethodGet, "/api/projects/"+projectID+"/hierarchy", nil)
	req = withURLParam(req, "id", projectID)
	testHandler.GetProjectHierarchy(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("empty hierarchy: status=%d body=%s", w.Code, w.Body.String())
	}
	var empty ProjectHierarchyResponse
	if err := json.NewDecoder(w.Body).Decode(&empty); err != nil {
		t.Fatalf("decode empty hierarchy: %v", err)
	}
	if empty.Total != 0 || empty.Memberships == nil || len(empty.Memberships) != 0 {
		t.Fatalf("empty hierarchy = %+v, want stable empty collection", empty)
	}

	root := createHierarchyMembershipViaAPI(t, projectID, hierarchyAPIAgent(t, "root"), nil, "Project Lead")
	child := createHierarchyMembershipViaAPI(t, projectID, hierarchyAPIAgent(t, "child"), &root.ID, "Engineer")

	archive := httptest.NewRecorder()
	archiveReq := newRequest(http.MethodDelete, "/api/projects/"+projectID+"/hierarchy/memberships/"+child.ID, nil)
	archiveReq = withURLParam(archiveReq, "id", projectID)
	archiveReq = withURLParam(archiveReq, "membershipId", child.ID)
	testHandler.ArchiveProjectHierarchyMembership(archive, archiveReq)
	if archive.Code != http.StatusNoContent {
		t.Fatalf("archive child: status=%d body=%s", archive.Code, archive.Body.String())
	}

	w = httptest.NewRecorder()
	req = newRequest(http.MethodGet, "/api/projects/"+projectID+"/hierarchy", nil)
	req = withURLParam(req, "id", projectID)
	testHandler.GetProjectHierarchy(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("active hierarchy: status=%d body=%s", w.Code, w.Body.String())
	}
	var response ProjectHierarchyResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode hierarchy: %v", err)
	}
	if response.Total != 1 || len(response.Memberships) != 1 || response.Memberships[0].ID != root.ID {
		t.Fatalf("active hierarchy = %+v, want only root %s", response, root.ID)
	}
}

func TestProjectHierarchyAPIMutationsDelegateToService(t *testing.T) {
	projectID := hierarchyAPIProject(t)
	root := createHierarchyMembershipViaAPI(t, projectID, hierarchyAPIAgent(t, "root"), nil, "Lead")
	child := createHierarchyMembershipViaAPI(t, projectID, hierarchyAPIAgent(t, "child"), &root.ID, "Engineer")
	other := createHierarchyMembershipViaAPI(t, projectID, hierarchyAPIAgent(t, "other"), &root.ID, "Other Lead")

	// One-node reparent.
	w := httptest.NewRecorder()
	req := newRequest(http.MethodPut, "/api/projects/"+projectID+"/hierarchy/memberships/"+child.ID+"/manager", ReparentProjectHierarchyMembershipRequest{
		ManagerMembershipID: other.ID,
	})
	req = withURLParam(req, "id", projectID)
	req = withURLParam(req, "membershipId", child.ID)
	testHandler.ReparentProjectHierarchyMembership(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("reparent child: status=%d body=%s", w.Code, w.Body.String())
	}
	var reparented ProjectHierarchyMembershipResponse
	if err := json.NewDecoder(w.Body).Decode(&reparented); err != nil {
		t.Fatalf("decode reparented membership: %v", err)
	}
	if reparented.ManagerMembershipID == nil || *reparented.ManagerMembershipID != other.ID {
		t.Fatalf("reparented manager = %v, want %s", reparented.ManagerMembershipID, other.ID)
	}

	// Atomic root replacement: old root reports to other, other becomes root.
	w = httptest.NewRecorder()
	req = newRequest(http.MethodPut, "/api/projects/"+projectID+"/hierarchy/structure", ApplyProjectHierarchyStructureRequest{
		Changes: []ProjectHierarchyStructureChangeRequest{
			{MembershipID: root.ID, ManagerMembershipID: &other.ID},
			{MembershipID: other.ID, ManagerMembershipID: nil},
		},
	})
	req = withURLParam(req, "id", projectID)
	testHandler.ApplyProjectHierarchyStructure(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("replace root: status=%d body=%s", w.Code, w.Body.String())
	}
	var changed ProjectHierarchyResponse
	if err := json.NewDecoder(w.Body).Decode(&changed); err != nil {
		t.Fatalf("decode structure result: %v", err)
	}
	if changed.Total != 2 {
		t.Fatalf("changed membership count=%d, want 2", changed.Total)
	}

	// The old child now remains below other and can be archived as a leaf.
	w = httptest.NewRecorder()
	req = newRequest(http.MethodDelete, "/api/projects/"+projectID+"/hierarchy/memberships/"+child.ID, nil)
	req = withURLParam(req, "id", projectID)
	req = withURLParam(req, "membershipId", child.ID)
	testHandler.ArchiveProjectHierarchyMembership(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("archive leaf: status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestProjectHierarchyAPIReactivationReusesMembershipID(t *testing.T) {
	projectID := hierarchyAPIProject(t)
	root := createHierarchyMembershipViaAPI(t, projectID, hierarchyAPIAgent(t, "root"), nil, "Lead")
	agentID := hierarchyAPIAgent(t, "returning")
	first := createHierarchyMembershipViaAPI(t, projectID, agentID, &root.ID, "Engineer")

	w := httptest.NewRecorder()
	req := newRequest(http.MethodDelete, "/api/projects/"+projectID+"/hierarchy/memberships/"+first.ID, nil)
	req = withURLParam(req, "id", projectID)
	req = withURLParam(req, "membershipId", first.ID)
	testHandler.ArchiveProjectHierarchyMembership(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("archive membership: status=%d body=%s", w.Code, w.Body.String())
	}

	reactivated := createHierarchyMembershipViaAPI(t, projectID, agentID, &root.ID, "Senior Engineer")
	if reactivated.ID != first.ID {
		t.Fatalf("reactivated id=%s, want original %s", reactivated.ID, first.ID)
	}
	if reactivated.Title != "Senior Engineer" {
		t.Fatalf("reactivated title=%q, want Senior Engineer", reactivated.Title)
	}
}

func TestProjectHierarchyAPIValidationAndDomainMapping(t *testing.T) {
	projectID := hierarchyAPIProject(t)
	root := createHierarchyMembershipViaAPI(t, projectID, hierarchyAPIAgent(t, "root"), nil, "Lead")

	t.Run("malformed json", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := newRequest(http.MethodPost, "/api/projects/"+projectID+"/hierarchy/memberships", nil)
		req.Body = io.NopCloser(strings.NewReader("{"))
		req = withURLParam(req, "id", projectID)
		testHandler.CreateProjectHierarchyMembership(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status=%d body=%s, want 400", w.Code, w.Body.String())
		}
	})

	t.Run("malformed project id", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := newRequest(http.MethodGet, "/api/projects/not-a-uuid/hierarchy", nil)
		req = withURLParam(req, "id", "not-a-uuid")
		testHandler.GetProjectHierarchy(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status=%d body=%s, want 400", w.Code, w.Body.String())
		}
	})

	t.Run("malformed agent id", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := newRequest(http.MethodPost, "/api/projects/"+projectID+"/hierarchy/memberships", CreateProjectHierarchyMembershipRequest{
			AgentID: "not-a-uuid",
		})
		req = withURLParam(req, "id", projectID)
		testHandler.CreateProjectHierarchyMembership(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status=%d body=%s, want 400", w.Code, w.Body.String())
		}
	})

	t.Run("malformed membership id", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := newRequest(http.MethodDelete, "/api/projects/"+projectID+"/hierarchy/memberships/not-a-uuid", nil)
		req = withURLParam(req, "id", projectID)
		req = withURLParam(req, "membershipId", "not-a-uuid")
		testHandler.ArchiveProjectHierarchyMembership(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status=%d body=%s, want 400", w.Code, w.Body.String())
		}
	})

	t.Run("project outside workspace", func(t *testing.T) {
		w := httptest.NewRecorder()
		missingProject := uuid.NewString()
		req := newRequest(http.MethodGet, "/api/projects/"+missingProject+"/hierarchy", nil)
		req = withURLParam(req, "id", missingProject)
		testHandler.GetProjectHierarchy(w, req)
		if w.Code != http.StatusNotFound {
			t.Fatalf("status=%d body=%s, want 404", w.Code, w.Body.String())
		}
	})

	t.Run("missing membership", func(t *testing.T) {
		w := httptest.NewRecorder()
		missing := uuid.NewString()
		req := newRequest(http.MethodDelete, "/api/projects/"+projectID+"/hierarchy/memberships/"+missing, nil)
		req = withURLParam(req, "id", projectID)
		req = withURLParam(req, "membershipId", missing)
		testHandler.ArchiveProjectHierarchyMembership(w, req)
		if w.Code != http.StatusNotFound {
			t.Fatalf("status=%d body=%s, want 404", w.Code, w.Body.String())
		}
		var payload map[string]any
		_ = json.NewDecoder(w.Body).Decode(&payload)
		if payload["code"] != "membership_not_found" {
			t.Fatalf("code=%v, want membership_not_found", payload["code"])
		}
	})

	t.Run("second root conflict", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := newRequest(http.MethodPost, "/api/projects/"+projectID+"/hierarchy/memberships", CreateProjectHierarchyMembershipRequest{
			AgentID: hierarchyAPIAgent(t, "second-root"),
			Title:   "Other Root",
		})
		req = withURLParam(req, "id", projectID)
		testHandler.CreateProjectHierarchyMembership(w, req)
		if w.Code != http.StatusConflict {
			t.Fatalf("status=%d body=%s, want 409", w.Code, w.Body.String())
		}
		var payload map[string]any
		_ = json.NewDecoder(w.Body).Decode(&payload)
		if payload["code"] != "invalid_root_count" {
			t.Fatalf("code=%v, want invalid_root_count", payload["code"])
		}
	})

	t.Run("archived agent conflict", func(t *testing.T) {
		archivedAgent := dbfx.Agent(t, "archived hierarchy api "+uuid.NewString(), "", testutil.Cols{
			"archived_at": testutil.Raw("now()"),
		})
		w := httptest.NewRecorder()
		req := newRequest(http.MethodPost, "/api/projects/"+projectID+"/hierarchy/memberships", CreateProjectHierarchyMembershipRequest{
			AgentID:             archivedAgent,
			ManagerMembershipID: &root.ID,
			Title:               "Archived Engineer",
		})
		req = withURLParam(req, "id", projectID)
		testHandler.CreateProjectHierarchyMembership(w, req)
		if w.Code != http.StatusConflict {
			t.Fatalf("status=%d body=%s, want 409", w.Code, w.Body.String())
		}
		var payload map[string]any
		_ = json.NewDecoder(w.Body).Decode(&payload)
		if payload["code"] != "agent_archived" {
			t.Fatalf("code=%v, want agent_archived", payload["code"])
		}
		body := strings.ToLower(w.Body.String())
		if strings.Contains(body, "project_agent_membership") || strings.Contains(body, "select ") || strings.Contains(body, "insert ") {
			t.Fatalf("response leaked persistence internals: %s", w.Body.String())
		}
	})

	t.Run("cross workspace agent conflict", func(t *testing.T) {
		otherWorkspace := dbfx.Workspace(t, "Hierarchy API Other", "hierarchy-api-other-"+uuid.NewString())
		crossWorkspaceAgent := dbfx.Agent(t, "cross workspace hierarchy api "+uuid.NewString(), "", testutil.Cols{
			"workspace_id": otherWorkspace,
		})
		w := httptest.NewRecorder()
		req := newRequest(http.MethodPost, "/api/projects/"+projectID+"/hierarchy/memberships", CreateProjectHierarchyMembershipRequest{
			AgentID:             crossWorkspaceAgent,
			ManagerMembershipID: &root.ID,
			Title:               "Foreign Engineer",
		})
		req = withURLParam(req, "id", projectID)
		testHandler.CreateProjectHierarchyMembership(w, req)
		if w.Code != http.StatusConflict {
			t.Fatalf("status=%d body=%s, want 409", w.Code, w.Body.String())
		}
		var payload map[string]any
		_ = json.NewDecoder(w.Body).Decode(&payload)
		if payload["code"] != "agent_not_eligible" {
			t.Fatalf("code=%v, want agent_not_eligible", payload["code"])
		}
		body := strings.ToLower(w.Body.String())
		if strings.Contains(body, "project_agent_membership") || strings.Contains(body, "select ") || strings.Contains(body, "insert ") {
			t.Fatalf("response leaked persistence internals: %s", w.Body.String())
		}
	})
}
