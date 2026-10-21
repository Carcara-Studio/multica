package handler

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/projecthierarchy"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// hierarchyEscalationAuthorization composes R1 with the existing native MAT,
// ownership, originator invocation and runtime gates. R2 exposes the transport;
// this callback is never derived from a client-supplied authority claim.
func (h *Handler) hierarchyEscalationAuthorization(token db.TaskToken, hash string, issueID pgtype.UUID) func(context.Context, *db.Queries, projecthierarchy.EscalationRelation) error {
	return func(ctx context.Context, q *db.Queries, r projecthierarchy.EscalationRelation) error {
		if r.IssueID != issueID {
			return errHierarchyDelegationDenied
		}
		if r.HumanInterventionRequired {
			_, err := h.authorizeHierarchySource(ctx, q, token, hash, issueID)
			return err
		}
		return h.hierarchyDelegationAuthorization(token, hash, issueID, r.ManagerAgentID)(ctx, q, projecthierarchy.DelegationRelation{ParentID: issueID, ProjectID: r.ProjectID})
	}
}
