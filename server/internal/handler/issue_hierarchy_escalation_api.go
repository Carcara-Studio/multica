package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/projecthierarchy"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func (h *Handler) CreateHierarchyEscalation(w http.ResponseWriter, r *http.Request) {
	token, hash, err := h.hierarchyDelegationActor(r)
	if err != nil {
		h.writeHierarchyEscalationError(w, err)
		return
	}
	var req struct {
		RequestID string `json:"request_id"`
		Reason    string `json:"reason"`
		Context   string `json:"context"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 160<<10))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&req); err != nil {
		writeError(w, 400, "invalid escalation request")
		return
	}
	if err = dec.Decode(&struct{}{}); err != io.EOF {
		writeError(w, 400, "invalid escalation request")
		return
	}
	id, ok := parseUUIDOrBadRequest(w, req.RequestID, "request_id")
	if !ok {
		return
	}
	req.Reason = strings.TrimSpace(req.Reason)
	req.Context = strings.TrimSpace(req.Context)
	if id.Bytes == [16]byte{} || req.Reason == "" || len(req.Reason) > 8000 || len(req.Context) > 16000 || !utf8.ValidString(req.Reason+req.Context) || strings.ContainsRune(req.Reason+req.Context, 0) {
		writeError(w, 400, "invalid escalation request")
		return
	}
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if issue.WorkspaceID != token.WorkspaceID || !issue.ProjectID.Valid {
		h.writeHierarchyEscalationError(w, errHierarchyDelegationDenied)
		return
	}
	prefix := h.getIssuePrefix(r.Context(), token.WorkspaceID)
	res, err := h.IssueService.Escalate(r.Context(), service.HierarchyEscalationCreate{WorkspaceID: token.WorkspaceID, ProjectID: issue.ProjectID, IssueID: issue.ID, SourceAgentID: token.AgentID, SourceTaskID: token.TaskID, RequestID: id, Reason: req.Reason, Context: req.Context, Authorize: h.hierarchyEscalationAuthorization(token, hash, issue.ID)}, service.IssueCreateOpts{
		BroadcastPayload: func(i db.Issue, _ []db.Attachment, _ []db.IssueLabel) map[string]any {
			payload := issueToResponse(i, prefix)
			h.fillStatusCategory(r.Context(), i.WorkspaceID, &payload)
			return map[string]any{"issue": payload}
		},
	})
	if err != nil {
		if writeIssueLimitReached(w, err) {
			return
		}
		h.writeHierarchyEscalationError(w, err)
		return
	}
	status := http.StatusCreated
	if res.EscalationReplayed {
		status = http.StatusOK
	}
	writeJSON(w, status, HierarchyDelegationResponse{IssueID: uuidToString(res.Issue.ID), Replayed: res.EscalationReplayed})
}

func (h *Handler) writeHierarchyEscalationError(w http.ResponseWriter, err error) {
	status, code := 500, "escalation_failed"
	switch {
	case projecthierarchy.IsCode(err, projecthierarchy.CodeInvalidInput):
		status, code = 400, "invalid_escalation"
	case errors.Is(err, service.ErrHierarchyDispatchUnavailable), errors.Is(err, service.ErrEscalationInvalid), errors.Is(err, errHierarchyDelegationDenied), projecthierarchy.IsCode(err, projecthierarchy.CodeEscalationNotAllowed):
		status, code = 403, "escalation_not_allowed"
	case projecthierarchy.IsCode(err, projecthierarchy.CodeHierarchyBusy):
		status, code = 409, "escalation_busy"
	case errors.Is(err, service.ErrEscalationConflict):
		status, code = 409, "escalation_conflict"
	case errors.Is(err, service.ErrEscalationDecisionGone):
		status, code = 409, "escalation_decision_deleted"
	case errors.Is(err, service.ErrEscalationNoHuman):
		status, code = 409, "escalation_human_recipient_required"
	}
	writeJSON(w, status, map[string]string{"error": code, "code": code})
}

type hierarchyEscalationRecord struct {
	SourceAgentName   string  `json:"source_agent_name"`
	ManagerAgentName  string  `json:"manager_agent_name"`
	RequestID         string  `json:"request_id"`
	SourceIssueID     string  `json:"source_issue_id"`
	SourceAgentID     string  `json:"source_agent_id"`
	SourceTaskID      string  `json:"source_task_id"`
	ManagerAgentID    *string `json:"manager_agent_id"`
	HumanIntervention bool    `json:"human_intervention"`
	DecisionIssueID   string  `json:"decision_issue_id"`
	DecisionStatus    *string `json:"decision_status"`
	Reason            string  `json:"reason"`
	Context           string  `json:"context"`
	Repair            string  `json:"repair"`
}

// ListHierarchyEscalations is a read snapshot; it never admits or dispatches work.
func (h *Handler) ListHierarchyEscalations(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	var before pgtype.UUID
	if value := r.URL.Query().Get("before"); value != "" {
		before, ok = parseUUIDOrBadRequest(w, value, "before")
		if !ok {
			return
		}
	}
	rows, err := h.DB.Query(r.Context(), `SELECT e.request_id,e.source_issue_id,e.source_agent_id,e.source_task_id,e.manager_agent_id,e.human_intervention,e.decision_issue_id,i.status,e.reason,e.context,COALESCE(sa.name,''),COALESCE(ma.name,''),
 CASE WHEN i.id IS NULL THEN 'decision_deleted'
 WHEN NOT e.human_intervention AND (i.assignee_type IS DISTINCT FROM 'agent' OR i.assignee_id IS DISTINCT FROM e.manager_agent_id) THEN 'decision_reassigned'
 WHEN NOT e.human_intervention AND NOT EXISTS(SELECT 1 FROM agent_task_queue t WHERE t.issue_id=e.decision_issue_id) THEN 'dispatch_not_recorded'
 ELSE '' END
 FROM project_hierarchy_escalation e LEFT JOIN issue i ON i.id=e.decision_issue_id AND i.workspace_id=e.workspace_id
 LEFT JOIN agent sa ON sa.id=e.source_agent_id AND sa.workspace_id=e.workspace_id
 LEFT JOIN agent ma ON ma.id=e.manager_agent_id AND ma.workspace_id=e.workspace_id
 WHERE e.workspace_id=$1 AND (e.source_issue_id=$2 OR e.decision_issue_id=$2) AND ($3::uuid IS NULL OR e.decision_issue_id<$3)
 ORDER BY e.decision_issue_id DESC LIMIT 51`, issue.WorkspaceID, issue.ID, before)
	if err != nil {
		writeError(w, 500, "failed to read escalations")
		return
	}
	records := []hierarchyEscalationRecord{}
	for rows.Next() {
		var x hierarchyEscalationRecord
		var request, source, agent, task, manager, decision pgtype.UUID
		var state pgtype.Text
		if err = rows.Scan(&request, &source, &agent, &task, &manager, &x.HumanIntervention, &decision, &state, &x.Reason, &x.Context, &x.SourceAgentName, &x.ManagerAgentName, &x.Repair); err != nil {
			break
		}
		x.RequestID = uuidToString(request)
		x.SourceIssueID = uuidToString(source)
		x.SourceAgentID = uuidToString(agent)
		x.SourceTaskID = uuidToString(task)
		x.ManagerAgentID = uuidToPtr(manager)
		x.DecisionIssueID = uuidToString(decision)
		x.DecisionStatus = textToPtr(state)
		records = append(records, x)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		writeError(w, 500, "failed to read escalations")
		return
	}
	var next *string
	if len(records) > 50 {
		records = records[:50]
		v := records[49].DecisionIssueID
		next = &v
	}
	statuses := issuestatus.NewResolver(issue.WorkspaceID)
	unavailable := map[string]bool{}
	for i := range records {
		x := &records[i]
		if x.DecisionStatus != nil {
			category := statuses.Category(r.Context(), h.Queries, *x.DecisionStatus)
			if statuses.Err() != nil {
				writeError(w, 500, "failed to read escalation status")
				return
			}
			if category == "done" || category == "closed" {
				x.Repair = ""
				continue
			}
		}
		if x.ManagerAgentID == nil || x.DecisionStatus == nil || x.Repair != "" {
			continue
		}
		if blocked, cached := unavailable[*x.ManagerAgentID]; cached {
			if blocked {
				x.Repair = "manager_unavailable"
			}
			continue
		}
		manager, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{ID: parseUUID(*x.ManagerAgentID), WorkspaceID: issue.WorkspaceID})
		if errors.Is(err, pgx.ErrNoRows) {
			x.Repair = "manager_unavailable"
			unavailable[*x.ManagerAgentID] = true
			continue
		}
		if err != nil {
			writeError(w, 500, "failed to read escalation manager")
			return
		}
		verdict, err := service.AgentReadiness(r.Context(), service.RuntimeLookup{Queries: h.Queries}, manager)
		if err != nil {
			writeError(w, 500, "failed to read escalation readiness")
			return
		}
		unavailable[*x.ManagerAgentID] = manager.ArchivedAt.Valid || verdict.Blocked()
		if unavailable[*x.ManagerAgentID] {
			x.Repair = "manager_unavailable"
		}
	}
	writeJSON(w, 200, map[string]any{"items": records, "next_cursor": next})
}
