package projecthierarchy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// lockHierarchyAgents must precede membership writes, including deactivation.
// Archive writers already own the Agent update locks and never lock Projects.
// NOWAIT prevents introducing a Project -> Agent wait into native runtime,
// revocation and Issue lock orders. A failed transaction must be rolled back.
func lockHierarchyAgents(ctx context.Context, tx pgx.Tx, workspaceID pgtype.UUID, rows []db.ProjectAgentMembership) error {
	ids := make([]pgtype.UUID, 0, len(rows))
	for _, row := range rows {
		if !row.ArchivedAt.Valid {
			ids = append(ids, row.AgentID)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return bytes.Compare(ids[i].Bytes[:], ids[j].Bytes[:]) < 0 })
	for i, id := range ids {
		if i > 0 && id == ids[i-1] {
			continue
		}
		var locked pgtype.UUID
		err := tx.QueryRow(ctx, `SELECT id FROM agent WHERE id=$1 AND workspace_id=$2 AND kind='user' FOR SHARE NOWAIT`, id, workspaceID).Scan(&locked)
		if errors.Is(err, pgx.ErrNoRows) {
			return domainError(CodeAgentNotEligible, "hierarchy Agent is unavailable")
		}
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "55P03" {
				return domainError(CodeHierarchyBusy, "hierarchy Agent is changing; retry with current state")
			}
			return fmt.Errorf("lock hierarchy Agent: %w", err)
		}
	}
	return nil
}

type ArchiveBlocker struct {
	ProjectID    string `json:"project_id"`
	MembershipID string `json:"membership_id"`
	AgentID      string `json:"agent_id"`
}

type ArchiveConflict struct{ Blockers []ArchiveBlocker }

func (e *ArchiveConflict) Error() string {
	return "Agent is an active hierarchy root or manager; reorganize the listed Projects before archiving"
}

// GuardArchivedAgents runs AFTER the native archive UPDATE and BEFORE any
// commit or external side effect. The caller owns all returned Agents' row
// locks and must roll back on error. Keeping discovery behind those locks also
// excludes membership creation/reactivation in previously unrelated Projects.
// This function takes no Project locks and performs no reparenting.
func GuardArchivedAgents(ctx context.Context, tx pgx.Tx, workspaceID pgtype.UUID, archived []db.Agent) error {
	if len(archived) == 0 {
		return nil
	}
	ids := make([]pgtype.UUID, 0, len(archived))
	for _, a := range archived {
		if a.WorkspaceID != workspaceID || !a.ArchivedAt.Valid {
			return fmt.Errorf("hierarchy archive guard requires archived Agents in one workspace")
		}
		ids = append(ids, a.ID)
	}
	rows, err := tx.Query(ctx, `SELECT m.project_id,m.id,m.agent_id FROM project_agent_membership m
 WHERE m.workspace_id=$1 AND m.agent_id=ANY($2::uuid[]) AND m.archived_at IS NULL
 AND (m.manager_membership_id IS NULL OR EXISTS (
 SELECT 1 FROM project_agent_membership c WHERE c.workspace_id=m.workspace_id
 AND c.project_id=m.project_id AND c.manager_membership_id=m.id AND c.archived_at IS NULL))
 ORDER BY m.project_id,m.id`, workspaceID, ids)
	if err != nil {
		return fmt.Errorf("inspect hierarchy archive dependencies: %w", err)
	}
	blockers := make([]ArchiveBlocker, 0)
	for rows.Next() {
		var project, membership, agent pgtype.UUID
		if err = rows.Scan(&project, &membership, &agent); err != nil {
			rows.Close()
			return err
		}
		blockers = append(blockers, ArchiveBlocker{uuid.UUID(project.Bytes).String(), uuid.UUID(membership.Bytes).String(), uuid.UUID(agent.Bytes).String()})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(blockers) > 0 {
		return &ArchiveConflict{Blockers: blockers}
	}
	_, err = tx.Exec(ctx, `UPDATE project_agent_membership SET archived_at=now(),updated_at=now()
 WHERE workspace_id=$1 AND agent_id=ANY($2::uuid[]) AND archived_at IS NULL`, workspaceID, ids)
	if err != nil {
		return fmt.Errorf("deactivate archived Agent memberships: %w", err)
	}
	return nil
}
