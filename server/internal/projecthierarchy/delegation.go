package projecthierarchy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// DelegationRelationInput contains server-resolved identities, not actor claims
// from a request body. The caller owns the transaction and must roll it back on
// any error. Call before acquiring Issue/property/Agent/counter locks.
type DelegationRelationInput struct {
	WorkspaceID pgtype.UUID
	ProjectID   pgtype.UUID
	ParentID    pgtype.UUID
	DelegatorID pgtype.UUID
	TargetID    pgtype.UUID
}

// DelegationRelation contains structural facts only. It is not caller
// authentication, invocation/runtime approval, idempotency or an execution
// lease, and must never authorize a later transaction without rechecking.
type DelegationRelation struct {
	ParentID               pgtype.UUID
	ProjectID              pgtype.UUID
	DelegatorMembershipID  pgtype.UUID
	TargetMembershipID     pgtype.UUID
}

// CheckDelegationRelation locks and validates one direct edge inside the
// caller's existing transaction. No child Issue, task or side effect is created.
func CheckDelegationRelation(ctx context.Context, tx pgx.Tx, in DelegationRelationInput) (DelegationRelation, error) {
	empty := DelegationRelation{}
	if tx == nil {
		return empty, domainError(CodeInvalidInput, "delegation requires a transaction")
	}
	for _, id := range []pgtype.UUID{in.WorkspaceID, in.ProjectID, in.ParentID, in.DelegatorID, in.TargetID} {
		if !id.Valid || id.Bytes == [16]byte{} {
			return empty, domainError(CodeInvalidInput, "delegation identities are required")
		}
	}
	deny := func() (DelegationRelation, error) {
		return empty, domainError(CodeDelegationNotAllowed, "direct-report delegation relation unavailable")
	}
	var workspace pgtype.UUID
	// Fence workspace deletion before taking descendant locks. This is the same
	// key-share/delete exclusion used by other application-owned relationships.
	if err := tx.QueryRow(ctx, "SELECT id FROM workspace WHERE id = $1 FOR KEY SHARE", in.WorkspaceID).Scan(&workspace); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return deny()
		}
		return empty, fmt.Errorf("lock delegation workspace: %w", err)
	}
	q := db.New(tx)
	if _, err := q.LockProjectForHierarchyMutation(ctx, db.LockProjectForHierarchyMutationParams{
		ProjectID: in.ProjectID, WorkspaceID: in.WorkspaceID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return deny()
		}
		return empty, fmt.Errorf("lock delegation project: %w", err)
	}
	parent, err := q.LockIssueForDescriptionUpdate(ctx, db.LockIssueForDescriptionUpdateParams{
		ID: in.ParentID, WorkspaceID: in.WorkspaceID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return deny()
		}
		return empty, fmt.Errorf("lock delegation parent: %w", err)
	}
	// This initial slice delegates the executing Agent's own directly assigned
	// Issue; mentions and Squad coordination are not hierarchy authority.
	if parent.ProjectID != in.ProjectID || !parent.AssigneeType.Valid ||
		parent.AssigneeType.String != "agent" || parent.AssigneeID != in.DelegatorID {
		return deny()
	}
	agents := []pgtype.UUID{in.DelegatorID, in.TargetID}
	sort.Slice(agents, func(i, j int) bool {
		return bytes.Compare(agents[i].Bytes[:], agents[j].Bytes[:]) < 0
	})
	for i, id := range agents {
		if i > 0 && id == agents[i-1] {
			continue
		}
		var locked pgtype.UUID
		if err := tx.QueryRow(ctx, "SELECT id FROM agent WHERE id = $1 AND workspace_id = $2 FOR SHARE", id, in.WorkspaceID).Scan(&locked); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return deny()
			}
			return empty, fmt.Errorf("lock delegation agent: %w", err)
		}
		agent, err := q.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: id, WorkspaceID: in.WorkspaceID})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return deny()
			}
			return empty, fmt.Errorf("load delegation agent: %w", err)
		}
		if agent.ArchivedAt.Valid {
			return deny()
		}
	}
	rows, err := q.ListProjectHierarchyMemberships(ctx, db.ListProjectHierarchyMembershipsParams{
		ProjectID: in.ProjectID, WorkspaceID: in.WorkspaceID,
	})
	if err != nil {
		return empty, fmt.Errorf("load delegation hierarchy: %w", err)
	}
	if err := validateActiveTree(rows); err != nil {
		return deny()
	}
	_, source, sourceFound := findMembershipByAgent(rows, in.DelegatorID)
	_, target, targetFound := findMembershipByAgent(rows, in.TargetID)
	if !sourceFound || !targetFound || source.ArchivedAt.Valid || target.ArchivedAt.Valid ||
		source.ID == target.ID || !target.ManagerMembershipID.Valid || target.ManagerMembershipID != source.ID {
		return deny()
	}
	return DelegationRelation{
		ParentID: in.ParentID, ProjectID: in.ProjectID,
		DelegatorMembershipID: source.ID, TargetMembershipID: target.ID,
	}, nil
}
