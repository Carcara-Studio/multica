package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/projecthierarchy"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var errHierarchyDelegationDenied = errors.New("hierarchy delegation not allowed")

type CreateHierarchyDelegationRequest struct {
	RequestID     string  `json:"request_id"`
	TargetAgentID string  `json:"target_agent_id"`
	Title         string  `json:"title"`
	Description   *string `json:"description,omitempty"`
	Priority      string  `json:"priority,omitempty"`
}

type HierarchyDelegationResponse struct {
	IssueID  string `json:"issue_id"`
	Replayed bool   `json:"replayed"`
}

// hierarchyDelegationActor binds only the real bearer MAT credential. Neither
// request DTOs nor resolveActor's middleware-less compatibility path are used.
func (h *Handler) hierarchyDelegationActor(r *http.Request) (db.TaskToken, string, error) {
	bearer, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || !strings.HasPrefix(bearer, "mat_") || r.Header.Get("X-Actor-Source") != "task_token" {
		return db.TaskToken{}, "", errHierarchyDelegationDenied
	}
	hash := auth.HashToken(bearer)
	token, err := h.Queries.GetTaskTokenByHash(r.Context(), hash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.TaskToken{}, "", errHierarchyDelegationDenied
		}
		return db.TaskToken{}, "", err
	}
	if uuidToString(token.UserID) != requestUserID(r) ||
		uuidToString(token.AgentID) != r.Header.Get("X-Agent-ID") ||
		uuidToString(token.TaskID) != r.Header.Get("X-Task-ID") ||
		uuidToString(token.WorkspaceID) != h.resolveWorkspaceID(r) {
		return db.TaskToken{}, "", errHierarchyDelegationDenied
	}
	return token, hash, nil
}

func (h *Handler) authorizeHierarchySource(ctx context.Context, qtx *db.Queries, token db.TaskToken, hash string, parentID pgtype.UUID) (db.AgentTaskQueue, error) {
	current, err := qtx.GetTaskTokenByHash(ctx, hash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.AgentTaskQueue{}, errHierarchyDelegationDenied
		}
		return db.AgentTaskQueue{}, err
	}
	if !current.ExpiresAt.Valid || !current.ExpiresAt.Time.After(time.Now()) || current.ID != token.ID || current.UserID != token.UserID || current.TaskID != token.TaskID ||
		current.AgentID != token.AgentID || current.WorkspaceID != token.WorkspaceID {
		return db.AgentTaskQueue{}, errHierarchyDelegationDenied
	}
	if _, err := qtx.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID: token.UserID, WorkspaceID: token.WorkspaceID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.AgentTaskQueue{}, errHierarchyDelegationDenied
		}
		return db.AgentTaskQueue{}, err
	}
	task, err := qtx.GetAgentTaskInWorkspace(ctx, db.GetAgentTaskInWorkspaceParams{ID: token.TaskID, WorkspaceID: token.WorkspaceID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.AgentTaskQueue{}, errHierarchyDelegationDenied
		}
		return db.AgentTaskQueue{}, err
	}
	source, err := qtx.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: token.AgentID, WorkspaceID: token.WorkspaceID})
	if err != nil {
		return db.AgentTaskQueue{}, err
	}
	if task.Status != "running" || task.AgentID != token.AgentID || task.IssueID != parentID ||
		task.SquadID.Valid || task.IsLeaderTask || !task.RuntimeID.Valid ||
		task.RuntimeID != source.RuntimeID || source.ArchivedAt.Valid {
		return db.AgentTaskQueue{}, errHierarchyDelegationDenied
	}
	return task, nil
}

func (h *Handler) hierarchyDelegationAuthorization(token db.TaskToken, hash string, parentID, targetID pgtype.UUID) func(context.Context, *db.Queries, projecthierarchy.DelegationRelation) error {
	return func(ctx context.Context, qtx *db.Queries, relation projecthierarchy.DelegationRelation) error {
		if relation.ParentID != parentID {
			return errHierarchyDelegationDenied
		}
		task, err := h.authorizeHierarchySource(ctx, qtx, token, hash, parentID)
		if err != nil {
			return err
		}

		target, err := qtx.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: targetID, WorkspaceID: token.WorkspaceID})
		if err != nil {
			return err
		}
		// The permission helper only needs Queries. Bind it to this transaction;
		// never mutate the shared handler or borrow its pool for these checks.
		scoped := &Handler{Queries: qtx}
		if !scoped.canInvokeAgent(ctx, target, "agent", uuidToString(token.AgentID),
			uuidToString(task.OriginatorUserID), uuidToString(token.WorkspaceID)) {
			return errHierarchyDelegationDenied
		}
		verdict, err := service.AgentReadiness(ctx, service.RuntimeLookup{Queries: qtx}, target)
		if err != nil {
			return err
		}
		if verdict.Blocked() {
			return errHierarchyDelegationDenied
		}
		return nil
	}
}

// CreateHierarchyDelegation is task-token-only. Authentication is revalidated
// in the native creation transaction on both new work and durable replay.
func (h *Handler) CreateHierarchyDelegation(w http.ResponseWriter, r *http.Request) {
	token, hash, err := h.hierarchyDelegationActor(r)
	if err != nil {
		h.writeHierarchyDelegationError(w, err)
		return
	}
	var req CreateHierarchyDelegationRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid delegation request")
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF || strings.TrimSpace(req.Title) == "" {
		writeError(w, http.StatusBadRequest, "invalid delegation request")
		return
	}
	requestID, ok := parseUUIDOrBadRequest(w, req.RequestID, "request_id")
	if !ok {
		return
	}
	targetID, ok := parseUUIDOrBadRequest(w, req.TargetAgentID, "target_agent_id")
	if !ok {
		return
	}
	priority := req.Priority
	if priority == "" {
		priority = "none"
	}
	if !validateIssueEnum(w, "priority", priority, validIssuePriorities) {
		return
	}
	parent, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if !parent.ProjectID.Valid || parent.WorkspaceID != token.WorkspaceID {
		h.writeHierarchyDelegationError(w, errHierarchyDelegationDenied)
		return
	}
	prefix := h.getIssuePrefix(r.Context(), token.WorkspaceID)
	res, err := h.IssueService.Create(r.Context(), service.IssueCreateParams{
		WorkspaceID: token.WorkspaceID, ProjectID: parent.ProjectID, ParentIssueID: parent.ID,
		Title: req.Title, Description: ptrToText(req.Description), Status: "todo", Priority: priority,
		CreatorType: "agent", CreatorID: token.AgentID,
		AssigneeType: pgtype.Text{String: "agent", Valid: true}, AssigneeID: targetID,
		OriginType: pgtype.Text{String: "agent_create", Valid: true}, OriginID: token.TaskID,
	}, service.IssueCreateOpts{
		ActorID: uuidToString(token.AgentID), AnalyticsAgentID: uuidToString(targetID),
		HierarchyDelegation: &service.HierarchyDelegationCreate{
			RequestID: requestID, Authorize: h.hierarchyDelegationAuthorization(token, hash, parent.ID, targetID),
		},
		BroadcastPayload: func(issue db.Issue, _ []db.Attachment, _ []db.IssueLabel) map[string]any {
			payload := issueToResponse(issue, prefix)
			h.fillStatusCategory(r.Context(), issue.WorkspaceID, &payload)
			return map[string]any{"issue": payload}
		},
	})
	if err != nil {
		if writeIssueLimitReached(w, err) {
			return
		}
		h.writeHierarchyDelegationError(w, err)
		return
	}
	status := http.StatusCreated
	if res.DelegationReplayed {
		status = http.StatusOK
	}
	writeJSON(w, status, HierarchyDelegationResponse{IssueID: util.UUIDToString(res.Issue.ID), Replayed: res.DelegationReplayed})
}

func (h *Handler) writeHierarchyDelegationError(w http.ResponseWriter, err error) {
	status, code := http.StatusInternalServerError, "delegation_failed"
	switch {
	case errors.Is(err, service.ErrHierarchyDispatchUnavailable), errors.Is(err, errHierarchyDelegationDenied), errors.Is(err, service.ErrDelegationInvalid),
		projecthierarchy.IsCode(err, projecthierarchy.CodeDelegationNotAllowed):
		status, code = http.StatusForbidden, "delegation_not_allowed"
	case projecthierarchy.IsCode(err, projecthierarchy.CodeInvalidInput):
		status, code = http.StatusBadRequest, "invalid_delegation"
	case errors.Is(err, service.ErrDelegationConflict):
		status, code = http.StatusConflict, "delegation_conflict"
	case errors.Is(err, service.ErrDelegationChildGone):
		status, code = http.StatusGone, "delegation_child_unavailable"
	}
	writeJSON(w, status, map[string]string{"code": code, "error": code})
}
