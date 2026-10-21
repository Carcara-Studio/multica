package projecthierarchy

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// EscalationRelationInput contains server-resolved identities. There is no
// requested target: escalation follows the persisted direct-manager edge.
// Call before acquiring Issue/Agent/property/counter locks. The caller owns the
// transaction and must roll it back on any error.
type EscalationRelationInput struct {
	WorkspaceID   pgtype.UUID
	ProjectID     pgtype.UUID
	IssueID       pgtype.UUID
	SourceAgentID pgtype.UUID
}

// EscalationRelation is a transaction-bound structural fact, not authenticated
// authority, runtime/invocation approval, an idempotency receipt or a routing
// action. HumanInterventionRequired identifies the root boundary; it does not
// notify a human. Recheck in every later transaction, including retries.
type EscalationRelation struct {
	IssueID                   pgtype.UUID
	ProjectID                 pgtype.UUID
	SourceMembershipID        pgtype.UUID
	ManagerMembershipID       pgtype.UUID
	ManagerAgentID            pgtype.UUID
	HumanInterventionRequired bool
}

// CheckEscalationRelation validates one direct upward edge or the root boundary
// for the acting Agent's directly assigned Issue. It performs no writes, task
// creation, reassignment, automatic parent completion or ancestor traversal.
func CheckEscalationRelation(ctx context.Context, tx pgx.Tx, in EscalationRelationInput) (EscalationRelation, error) {
	empty := EscalationRelation{}
	if tx == nil {
		return empty, domainError(CodeInvalidInput, "escalation requires a transaction")
	}
	for _, id := range []pgtype.UUID{in.WorkspaceID, in.ProjectID, in.IssueID, in.SourceAgentID} {
		if !id.Valid || id.Bytes == [16]byte{} {
			return empty, domainError(CodeInvalidInput, "escalation identities are required")
		}
	}
	deny := func() (EscalationRelation, error) {
		return empty, domainError(CodeEscalationNotAllowed, "direct-manager escalation relation unavailable")
	}
	var workspace pgtype.UUID
	// Match delegation's native counter/teardown fence before Project/Issue locks.
	if err := tx.QueryRow(ctx, `SELECT id FROM workspace WHERE id=$1 FOR NO KEY UPDATE`, in.WorkspaceID).Scan(&workspace); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return deny()
		}
		return empty, fmt.Errorf("lock escalation workspace: %w", err)
	}
	q := db.New(tx)
	if _, err := q.LockProjectForHierarchyMutation(ctx, db.LockProjectForHierarchyMutationParams{ProjectID: in.ProjectID, WorkspaceID: in.WorkspaceID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return deny()
		}
		return empty, fmt.Errorf("lock escalation project: %w", err)
	}
	issue, err := q.LockIssueForDescriptionUpdate(ctx, db.LockIssueForDescriptionUpdateParams{ID: in.IssueID, WorkspaceID: in.WorkspaceID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return deny()
		}
		return empty, fmt.Errorf("lock escalation Issue: %w", err)
	}
	if issue.ProjectID != in.ProjectID || !issue.AssigneeType.Valid || issue.AssigneeType.String != "agent" || issue.AssigneeID != in.SourceAgentID {
		return deny()
	}
	load := func() ([]db.ProjectAgentMembership, error) {
		return q.ListProjectHierarchyMemberships(ctx, db.ListProjectHierarchyMembershipsParams{ProjectID: in.ProjectID, WorkspaceID: in.WorkspaceID})
	}
	rows, err := load()
	if err != nil {
		return empty, fmt.Errorf("load escalation hierarchy: %w", err)
	}
	if validateActiveTree(rows) != nil {
		return deny()
	}
	_, source, ok := findMembershipByAgent(rows, in.SourceAgentID)
	if !ok || source.ArchivedAt.Valid {
		return deny()
	}
	selected := []db.ProjectAgentMembership{source}
	var manager db.ProjectAgentMembership
	if source.ManagerMembershipID.Valid {
		_, manager, ok = findMembershipByID(rows, source.ManagerMembershipID)
		if !ok || manager.ArchivedAt.Valid {
			return deny()
		}
		selected = append(selected, manager)
	}
	if err := validateActiveAgents(ctx, tx, q, in.WorkspaceID, selected); err != nil {
		if IsCode(err, CodeAgentNotEligible) || IsCode(err, CodeAgentArchived) || IsCode(err, CodeHierarchyBusy) {
			return deny()
		}
		return empty, fmt.Errorf("validate escalation Agents: %w", err)
	}
	// Archive never takes the Project lock. Its Agent locks can precede ours;
	// even archive followed by restore must not revive this earlier membership
	// snapshot. Re-read under retained shared Agent locks before returning facts.
	rows, err = load()
	if err != nil {
		return empty, fmt.Errorf("reload escalation hierarchy: %w", err)
	}
	if validateActiveTree(rows) != nil {
		return deny()
	}
	_, current, ok := findMembershipByAgent(rows, in.SourceAgentID)
	if !ok || current.ArchivedAt.Valid || current.ID != source.ID || current.ManagerMembershipID != source.ManagerMembershipID {
		return deny()
	}
	result := EscalationRelation{IssueID: issue.ID, ProjectID: in.ProjectID, SourceMembershipID: source.ID}
	if !source.ManagerMembershipID.Valid {
		// Project lead is intentionally not an organizational edge or a substitute
		// Agent target, even when that lead is this root or another Agent.
		result.HumanInterventionRequired = true
		return result, nil
	}
	_, currentManager, ok := findMembershipByID(rows, manager.ID)
	if !ok || currentManager.ArchivedAt.Valid || currentManager.AgentID != manager.AgentID {
		return deny()
	}
	result.ManagerMembershipID = manager.ID
	result.ManagerAgentID = manager.AgentID
	return result, nil
}
