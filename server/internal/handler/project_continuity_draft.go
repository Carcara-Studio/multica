package handler

import (
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/projectmemory"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func (h *Handler) GetProjectContinuityDraft(w http.ResponseWriter, r *http.Request) {
	p, ok := h.continuityProject(w, r)
	if !ok {
		return
	}
	current := projectCheckpoint(p)
	if !current.Valid() {
		writeError(w, 500, "stored continuity checkpoint is invalid")
		return
	}
	source, err := h.Queries.GetLatestProjectContinuityDraftSource(r.Context(), db.GetLatestProjectContinuityDraftSourceParams{ProjectID: p.ID, WorkspaceID: p.WorkspaceID})
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, 200, projectmemory.EmptyDraft(current, "no_eligible_completed_task"))
		return
	}
	if err != nil {
		writeError(w, 500, "continuity source lookup failed")
		return
	}
	draft := projectmemory.BuildDraft(current, projectmemory.DraftSource{TaskID: uuidToString(source.TaskID), IssueID: uuidToString(source.IssueID), IssueRevision: source.IssueRevision, IssueStatus: source.IssueStatus, CompletedAt: timestampToString(source.CompletedAt)}, source.Result)
	writeJSON(w, 200, draft)
}
