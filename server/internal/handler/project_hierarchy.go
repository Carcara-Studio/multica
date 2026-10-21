package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/projecthierarchy"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ProjectHierarchyMembershipResponse is the stable HTTP representation of one
// active Project hierarchy membership. The API intentionally exposes a flat
// adjacency list rather than a recursively nested tree so arbitrary hierarchy
// depth does not become part of the transport contract.
type ProjectHierarchyMembershipResponse struct {
	ID                  string  `json:"id"`
	AgentID             string  `json:"agent_id"`
	ManagerMembershipID *string `json:"manager_membership_id"`
	Title               string  `json:"title"`
	CreatedAt           string  `json:"created_at"`
	UpdatedAt           string  `json:"updated_at"`
}

type ProjectHierarchyResponse struct {
	Memberships []ProjectHierarchyMembershipResponse `json:"memberships"`
	Total       int                                  `json:"total"`
}

type CreateProjectHierarchyMembershipRequest struct {
	AgentID             string  `json:"agent_id"`
	ManagerMembershipID *string `json:"manager_membership_id"`
	Title               string  `json:"title"`
}

type ReparentProjectHierarchyMembershipRequest struct {
	ManagerMembershipID string `json:"manager_membership_id"`
}

type ProjectHierarchyStructureChangeRequest struct {
	MembershipID        string  `json:"membership_id"`
	ManagerMembershipID *string `json:"manager_membership_id"`
}

type ApplyProjectHierarchyStructureRequest struct {
	Changes []ProjectHierarchyStructureChangeRequest `json:"changes"`
}

func projectHierarchyMembershipToResponse(row db.ProjectAgentMembership) ProjectHierarchyMembershipResponse {
	return ProjectHierarchyMembershipResponse{
		ID:                  uuidToString(row.ID),
		AgentID:             uuidToString(row.AgentID),
		ManagerMembershipID: uuidToPtr(row.ManagerMembershipID),
		Title:               row.Title,
		CreatedAt:           timestampToString(row.CreatedAt),
		UpdatedAt:           timestampToString(row.UpdatedAt),
	}
}

// loadProjectForHierarchy scopes Project hierarchy access to the request's
// current Workspace. It deliberately mirrors the existing Project nested
// resource boundary without making hierarchy depend on project_resource code.
func (h *Handler) loadProjectForHierarchy(w http.ResponseWriter, r *http.Request) (db.Project, bool) {
	projectID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "project id")
	if !ok {
		return db.Project{}, false
	}
	workspaceID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace id")
	if !ok {
		return db.Project{}, false
	}
	project, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
		ID:          projectID,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return db.Project{}, false
	}
	return project, true
}

func parseOptionalHierarchyUUID(w http.ResponseWriter, value *string, fieldName string) (pgtype.UUID, bool) {
	if value == nil {
		return pgtype.UUID{}, true
	}
	return parseUUIDOrBadRequest(w, *value, fieldName)
}

func writeProjectHierarchyDomainError(w http.ResponseWriter, err error, fallbackMessage string) {
	var domainErr *projecthierarchy.DomainError
	if !errors.As(err, &domainErr) {
		writeError(w, http.StatusInternalServerError, fallbackMessage)
		return
	}

	status := http.StatusConflict
	switch domainErr.Code {
	case projecthierarchy.CodeInvalidInput:
		status = http.StatusBadRequest
	case projecthierarchy.CodeProjectNotFound, projecthierarchy.CodeMembershipNotFound:
		status = http.StatusNotFound
	case projecthierarchy.CodeHierarchyBusy, projecthierarchy.CodeMembershipExists,
		projecthierarchy.CodeAgentNotEligible,
		projecthierarchy.CodeAgentArchived,
		projecthierarchy.CodeManagerNotActive,
		projecthierarchy.CodeRootRequired,
		projecthierarchy.CodeRootChangeRequiresBulk,
		projecthierarchy.CodeInvalidRootCount,
		projecthierarchy.CodeCycleDetected,
		projecthierarchy.CodeOrphaningMembership,
		projecthierarchy.CodeDuplicateChange:
		status = http.StatusConflict
	default:
		writeError(w, http.StatusInternalServerError, fallbackMessage)
		return
	}

	message := domainErr.Detail
	if message == "" {
		message = domainErr.Error()
	}
	writeJSON(w, status, map[string]any{
		"error": message,
		"code":  string(domainErr.Code),
	})
}

// GetProjectHierarchy returns the active hierarchy for a Project as a flat
// adjacency list. Archived membership history remains internal.
func (h *Handler) GetProjectHierarchy(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForHierarchy(w, r)
	if !ok {
		return
	}

	rows, err := h.Queries.ListProjectHierarchyMemberships(r.Context(), db.ListProjectHierarchyMembershipsParams{
		ProjectID:   project.ID,
		WorkspaceID: project.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list project hierarchy")
		return
	}

	memberships := make([]ProjectHierarchyMembershipResponse, 0, len(rows))
	for _, row := range rows {
		if row.ArchivedAt.Valid {
			continue
		}
		memberships = append(memberships, projectHierarchyMembershipToResponse(row))
	}
	writeJSON(w, http.StatusOK, ProjectHierarchyResponse{
		Memberships: memberships,
		Total:       len(memberships),
	})
}

// CreateProjectHierarchyMembership creates a Project membership or reactivates
// the existing archived row for the same Agent. All structural validation is
// owned by projecthierarchy.Service.
func (h *Handler) CreateProjectHierarchyMembership(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForHierarchy(w, r)
	if !ok {
		return
	}
	if _, ok := requireUserID(w, r); !ok {
		return
	}

	var req CreateProjectHierarchyMembershipRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	agentID, ok := parseUUIDOrBadRequest(w, req.AgentID, "agent_id")
	if !ok {
		return
	}
	managerID, ok := parseOptionalHierarchyUUID(w, req.ManagerMembershipID, "manager_membership_id")
	if !ok {
		return
	}

	service := projecthierarchy.NewService(h.Queries, h.TxStarter)
	row, err := service.CreateMembership(r.Context(), projecthierarchy.CreateMembershipInput{
		WorkspaceID:         project.WorkspaceID,
		ProjectID:           project.ID,
		AgentID:             agentID,
		ManagerMembershipID: managerID,
		Title:               req.Title,
	})
	if err != nil {
		writeProjectHierarchyDomainError(w, err, "failed to create project hierarchy membership")
		return
	}
	writeJSON(w, http.StatusCreated, projectHierarchyMembershipToResponse(row))
}

// ReparentProjectHierarchyMembership changes one active non-root membership's
// direct manager. Root changes must use ApplyProjectHierarchyStructure so the
// complete final tree changes atomically.
func (h *Handler) ReparentProjectHierarchyMembership(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForHierarchy(w, r)
	if !ok {
		return
	}
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	membershipID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "membershipId"), "membership id")
	if !ok {
		return
	}

	var req ReparentProjectHierarchyMembershipRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	managerID, ok := parseUUIDOrBadRequest(w, req.ManagerMembershipID, "manager_membership_id")
	if !ok {
		return
	}

	service := projecthierarchy.NewService(h.Queries, h.TxStarter)
	row, err := service.ReparentMembership(r.Context(), projecthierarchy.ReparentMembershipInput{
		WorkspaceID:         project.WorkspaceID,
		ProjectID:           project.ID,
		MembershipID:        membershipID,
		ManagerMembershipID: managerID,
	})
	if err != nil {
		writeProjectHierarchyDomainError(w, err, "failed to reparent project hierarchy membership")
		return
	}
	writeJSON(w, http.StatusOK, projectHierarchyMembershipToResponse(row))
}

// ArchiveProjectHierarchyMembership archives one active leaf non-root
// membership. The service rejects root or descendant-orphaning removals.
func (h *Handler) ArchiveProjectHierarchyMembership(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForHierarchy(w, r)
	if !ok {
		return
	}
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	membershipID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "membershipId"), "membership id")
	if !ok {
		return
	}

	service := projecthierarchy.NewService(h.Queries, h.TxStarter)
	if _, err := service.ArchiveMembership(r.Context(), projecthierarchy.ArchiveMembershipInput{
		WorkspaceID:  project.WorkspaceID,
		ProjectID:    project.ID,
		MembershipID: membershipID,
	}); err != nil {
		writeProjectHierarchyDomainError(w, err, "failed to archive project hierarchy membership")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ApplyProjectHierarchyStructure atomically applies an explicit set of manager
// edge changes. It is the only HTTP operation that can replace the Project
// hierarchy root.
func (h *Handler) ApplyProjectHierarchyStructure(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForHierarchy(w, r)
	if !ok {
		return
	}
	if _, ok := requireUserID(w, r); !ok {
		return
	}

	var req ApplyProjectHierarchyStructureRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	changes := make([]projecthierarchy.ManagerChange, 0, len(req.Changes))
	for _, change := range req.Changes {
		membershipID, ok := parseUUIDOrBadRequest(w, change.MembershipID, "membership_id")
		if !ok {
			return
		}
		managerID, ok := parseOptionalHierarchyUUID(w, change.ManagerMembershipID, "manager_membership_id")
		if !ok {
			return
		}
		changes = append(changes, projecthierarchy.ManagerChange{
			MembershipID:        membershipID,
			ManagerMembershipID: managerID,
		})
	}

	service := projecthierarchy.NewService(h.Queries, h.TxStarter)
	rows, err := service.ApplyStructure(r.Context(), projecthierarchy.ApplyStructureInput{
		WorkspaceID: project.WorkspaceID,
		ProjectID:   project.ID,
		Changes:     changes,
	})
	if err != nil {
		writeProjectHierarchyDomainError(w, err, "failed to update project hierarchy structure")
		return
	}
	memberships := make([]ProjectHierarchyMembershipResponse, len(rows))
	for i, row := range rows {
		memberships[i] = projectHierarchyMembershipToResponse(row)
	}
	writeJSON(w, http.StatusOK, ProjectHierarchyResponse{
		Memberships: memberships,
		Total:       len(memberships),
	})
}
