package projecthierarchy

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func activeMemberships(rows []db.ProjectAgentMembership) map[pgtype.UUID]db.ProjectAgentMembership {
	active := make(map[pgtype.UUID]db.ProjectAgentMembership, len(rows))
	for _, row := range rows {
		if row.ArchivedAt.Valid {
			continue
		}
		active[row.ID] = row
	}
	return active
}

func validateActiveTree(rows []db.ProjectAgentMembership) error {
	active := activeMemberships(rows)
	if len(active) == 0 {
		return nil
	}

	rootCount := 0
	for id, row := range active {
		if !row.ManagerMembershipID.Valid {
			rootCount++
			continue
		}
		if row.ManagerMembershipID == id {
			return domainError(CodeCycleDetected, "membership cannot manage itself")
		}
		if _, ok := active[row.ManagerMembershipID]; !ok {
			return domainError(CodeManagerNotActive, "manager membership is not active in the same project")
		}
	}

	state := make(map[pgtype.UUID]uint8, len(active))
	var visit func(pgtype.UUID) error
	visit = func(id pgtype.UUID) error {
		switch state[id] {
		case 1:
			return domainError(CodeCycleDetected, "hierarchy contains a cycle")
		case 2:
			return nil
		}

		state[id] = 1
		row := active[id]
		if row.ManagerMembershipID.Valid {
			if err := visit(row.ManagerMembershipID); err != nil {
				return err
			}
		}
		state[id] = 2
		return nil
	}

	for id := range active {
		if err := visit(id); err != nil {
			return err
		}
	}

	if rootCount != 1 {
		return domainError(CodeInvalidRootCount, "non-empty hierarchy must contain exactly one active root; got %d", rootCount)
	}
	return nil
}

func validateActiveAgents(ctx context.Context, tx pgx.Tx, q *db.Queries, workspaceID pgtype.UUID, rows []db.ProjectAgentMembership) error {
	if err := lockHierarchyAgents(ctx, tx, workspaceID, rows); err != nil {
		return err
	}
	for _, row := range rows {
		if row.ArchivedAt.Valid {
			continue
		}
		agent, err := q.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{
			ID:          row.AgentID,
			WorkspaceID: workspaceID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return domainError(CodeAgentNotEligible, "active membership references a missing, cross-workspace, or non-user agent")
			}
			return fmt.Errorf("load hierarchy agent: %w", err)
		}
		if agent.ArchivedAt.Valid {
			return domainError(CodeAgentArchived, "active membership references an archived agent")
		}
	}
	return nil
}

func cloneMemberships(rows []db.ProjectAgentMembership) []db.ProjectAgentMembership {
	cloned := make([]db.ProjectAgentMembership, len(rows))
	copy(cloned, rows)
	return cloned
}

func findMembershipByID(rows []db.ProjectAgentMembership, id pgtype.UUID) (int, db.ProjectAgentMembership, bool) {
	for i, row := range rows {
		if row.ID == id {
			return i, row, true
		}
	}
	return -1, db.ProjectAgentMembership{}, false
}

func findMembershipByAgent(rows []db.ProjectAgentMembership, agentID pgtype.UUID) (int, db.ProjectAgentMembership, bool) {
	for i, row := range rows {
		if row.AgentID == agentID {
			return i, row, true
		}
	}
	return -1, db.ProjectAgentMembership{}, false
}

func hasActiveMembership(rows []db.ProjectAgentMembership) bool {
	for _, row := range rows {
		if !row.ArchivedAt.Valid {
			return true
		}
	}
	return false
}

func activeChildExists(rows []db.ProjectAgentMembership, managerID pgtype.UUID) bool {
	for _, row := range rows {
		if row.ArchivedAt.Valid || !row.ManagerMembershipID.Valid {
			continue
		}
		if row.ManagerMembershipID == managerID {
			return true
		}
	}
	return false
}
