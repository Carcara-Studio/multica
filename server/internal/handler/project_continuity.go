package handler

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/projectmemory"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func projectCheckpoint(p db.Project) projectmemory.Checkpoint {
	return projectmemory.Checkpoint{ProjectID: uuidToString(p.ID), Revision: uuidToString(p.ContinuityRevision), Content: p.ContinuityContent.String, ReviewedBy: uuidToString(p.ContinuityReviewedBy), ReviewedAt: timestampToString(p.ContinuityReviewedAt)}
}

func (h *Handler) continuityProject(w http.ResponseWriter, r *http.Request) (db.Project, bool) {
	if middleware.DaemonIDFromContext(r.Context()) != "" {
		writeError(w, 403, "daemon tokens cannot manage project continuity")
		return db.Project{}, false
	}
	actor, _ := h.resolveActor(r, requestUserID(r), h.resolveWorkspaceID(r))
	if actor != "member" {
		writeError(w, 403, "project continuity management requires a human member")
		return db.Project{}, false
	}
	if _, ok := h.requireWorkspaceRole(w, r, h.resolveWorkspaceID(r), "project not found", "owner", "admin", "member"); !ok {
		return db.Project{}, false
	}
	return h.loadProjectForResource(w, r, chi.URLParam(r, "id"))
}

func (h *Handler) GetProjectContinuity(w http.ResponseWriter, r *http.Request) {
	p, ok := h.continuityProject(w, r)
	if !ok {
		return
	}
	c := projectCheckpoint(p)
	if !c.Valid() {
		writeError(w, 500, "stored continuity checkpoint is invalid")
		return
	}
	writeJSON(w, 200, c)
}

type projectContinuityRequest struct {
	ExpectedRevision *string `json:"expected_revision"`
	Content          *string `json:"content"`
}

func (h *Handler) SetProjectContinuity(w http.ResponseWriter, r *http.Request) {
	h.writeProjectContinuity(w, r, false)
}
func (h *Handler) ClearProjectContinuity(w http.ResponseWriter, r *http.Request) {
	h.writeProjectContinuity(w, r, true)
}

func (h *Handler) writeProjectContinuity(w http.ResponseWriter, r *http.Request, clear bool) {
	p, ok := h.continuityProject(w, r)
	if !ok {
		return
	}
	var req projectContinuityRequest
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384))
	d.DisallowUnknownFields()
	if d.Decode(&req) != nil || d.Decode(new(any)) != io.EOF || req.ExpectedRevision == nil {
		writeError(w, 400, "expected_revision and one valid JSON request are required")
		return
	}
	var expected pgtype.UUID
	if *req.ExpectedRevision != "" {
		var valid bool
		expected, valid = parseUUIDOrBadRequest(w, *req.ExpectedRevision, "expected_revision")
		if !valid {
			return
		}
		if expected.Bytes == [16]byte{} {
			writeError(w, 400, "expected_revision must be nonzero")
			return
		}
	}
	var content pgtype.Text
	if clear {
		if req.Content != nil {
			writeError(w, 400, "clear accepts only expected_revision")
			return
		}
	} else {
		if req.Content == nil {
			writeError(w, 400, "content is required")
			return
		}
		if err := projectmemory.ValidateContent(*req.Content); err != nil {
			writeError(w, 400, err.Error())
			return
		}
		content = pgtype.Text{String: *req.Content, Valid: true}
	}
	userID, valid := parseUUIDOrBadRequest(w, requestUserID(r), "user_id")
	if !valid {
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "continuity transaction unavailable")
		return
	}
	defer tx.Rollback(r.Context())
	q := h.Queries.WithTx(tx)
	if _, err = q.LockWorkspaceForChatSessionCreate(r.Context(), p.WorkspaceID); err != nil {
		writeError(w, 403, "workspace unavailable")
		return
	}
	member, err := q.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{UserID: userID, WorkspaceID: p.WorkspaceID})
	if err != nil || (member.Role != "owner" && member.Role != "admin" && member.Role != "member") {
		writeError(w, 403, "project continuity requires current member write access")
		return
	}
	updated, err := q.SetProjectContinuity(r.Context(), db.SetProjectContinuityParams{ProjectID: p.ID, WorkspaceID: p.WorkspaceID, ExpectedRevision: expected, NewRevision: pgtype.UUID{Bytes: [16]byte(uuid.New()), Valid: true}, Content: content, ReviewedBy: member.UserID})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 409, "project or continuity revision changed; read again before saving")
		return
	}
	if err != nil {
		slog.Warn("write project continuity failed", "project_id", uuidToString(p.ID))
		writeError(w, 500, "could not save project continuity")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, 500, "could not commit project continuity")
		return
	}
	writeJSON(w, 200, projectCheckpoint(updated))
}
