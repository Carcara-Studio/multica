package projecthierarchy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// TxStarter abstracts transaction creation and is satisfied by pgxpool.Pool.
type TxStarter interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Service is the authoritative write boundary for Project hierarchy structure.
// Public adapters must not write project_agent_membership directly.
type Service struct {
	Queries   *db.Queries
	TxStarter TxStarter
}

func NewService(q *db.Queries, txStarter TxStarter) *Service {
	return &Service{Queries: q, TxStarter: txStarter}
}

type CreateMembershipInput struct {
	WorkspaceID         pgtype.UUID
	ProjectID           pgtype.UUID
	AgentID             pgtype.UUID
	ManagerMembershipID pgtype.UUID
	Title               string
}

type ReparentMembershipInput struct {
	WorkspaceID         pgtype.UUID
	ProjectID           pgtype.UUID
	MembershipID        pgtype.UUID
	ManagerMembershipID pgtype.UUID
}

type ArchiveMembershipInput struct {
	WorkspaceID  pgtype.UUID
	ProjectID    pgtype.UUID
	MembershipID pgtype.UUID
}

type ManagerChange struct {
	MembershipID        pgtype.UUID
	ManagerMembershipID pgtype.UUID
}

type ApplyStructureInput struct {
	WorkspaceID pgtype.UUID
	ProjectID   pgtype.UUID
	Changes     []ManagerChange
}

func requireUUID(name string, id pgtype.UUID) error {
	if !id.Valid {
		return domainError(CodeInvalidInput, "%s is required", name)
	}
	return nil
}

func (s *Service) beginProjectMutation(
	ctx context.Context,
	workspaceID pgtype.UUID,
	projectID pgtype.UUID,
) (pgx.Tx, *db.Queries, []db.ProjectAgentMembership, error) {
	if s == nil || s.Queries == nil || s.TxStarter == nil {
		return nil, nil, nil, fmt.Errorf("project hierarchy service is not configured")
	}
	if err := requireUUID("workspace_id", workspaceID); err != nil {
		return nil, nil, nil, err
	}
	if err := requireUUID("project_id", projectID); err != nil {
		return nil, nil, nil, err
	}

	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("begin hierarchy mutation: %w", err)
	}
	q := s.Queries.WithTx(tx)

	if _, err := q.LockProjectForHierarchyMutation(ctx, db.LockProjectForHierarchyMutationParams{
		ProjectID:   projectID,
		WorkspaceID: workspaceID,
	}); err != nil {
		_ = tx.Rollback(ctx)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, nil, domainError(CodeProjectNotFound, "project is not in the requested workspace")
		}
		return nil, nil, nil, fmt.Errorf("lock hierarchy project: %w", err)
	}

	rows, err := q.ListProjectHierarchyMemberships(ctx, db.ListProjectHierarchyMembershipsParams{
		ProjectID:   projectID,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		_ = tx.Rollback(ctx)
		return nil, nil, nil, fmt.Errorf("list hierarchy memberships: %w", err)
	}
	// Lock original memberships as well as the eventual active tree: removing a
	// leaf must also serialize with Agent archival.
	if err := lockHierarchyAgents(ctx, tx, workspaceID, rows); err != nil {
		_ = tx.Rollback(ctx)
		return nil, nil, nil, err
	}
	return tx, q, rows, nil
}

func commitMutation(ctx context.Context, tx pgx.Tx) error {
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit hierarchy mutation: %w", err)
	}
	return nil
}

func (s *Service) CreateMembership(ctx context.Context, input CreateMembershipInput) (db.ProjectAgentMembership, error) {
	if err := requireUUID("agent_id", input.AgentID); err != nil {
		return db.ProjectAgentMembership{}, err
	}

	tx, q, rows, err := s.beginProjectMutation(ctx, input.WorkspaceID, input.ProjectID)
	if err != nil {
		return db.ProjectAgentMembership{}, err
	}
	defer tx.Rollback(ctx)

	existingIndex, existing, exists := findMembershipByAgent(rows, input.AgentID)
	if exists && !existing.ArchivedAt.Valid {
		return db.ProjectAgentMembership{}, domainError(CodeMembershipExists, "agent already has an active membership in this project")
	}

	activeExists := hasActiveMembership(rows)
	if !activeExists && input.ManagerMembershipID.Valid {
		return db.ProjectAgentMembership{}, domainError(CodeRootRequired, "the first active membership must be the project root")
	}
	if activeExists && !input.ManagerMembershipID.Valid {
		return db.ProjectAgentMembership{}, domainError(CodeInvalidRootCount, "a non-empty hierarchy already has an active root")
	}
	if input.ManagerMembershipID.Valid {
		_, manager, ok := findMembershipByID(rows, input.ManagerMembershipID)
		if !ok || manager.ArchivedAt.Valid {
			return db.ProjectAgentMembership{}, domainError(CodeManagerNotActive, "manager membership is not active in this project")
		}
	}

	tentative := cloneMemberships(rows)
	if exists {
		tentative[existingIndex].ManagerMembershipID = input.ManagerMembershipID
		tentative[existingIndex].Title = input.Title
		tentative[existingIndex].ArchivedAt = pgtype.Timestamptz{}
	} else {
		id := dbid.NewV7()
		if !id.Valid {
			return db.ProjectAgentMembership{}, fmt.Errorf("generate hierarchy membership id")
		}
		tentative = append(tentative, db.ProjectAgentMembership{
			ID:                  id,
			WorkspaceID:         input.WorkspaceID,
			ProjectID:           input.ProjectID,
			AgentID:             input.AgentID,
			ManagerMembershipID: input.ManagerMembershipID,
			Title:               input.Title,
		})
	}

	if err := validateActiveTree(tentative); err != nil {
		return db.ProjectAgentMembership{}, err
	}
	if err := validateActiveAgents(ctx, tx, q, input.WorkspaceID, tentative); err != nil {
		return db.ProjectAgentMembership{}, err
	}

	var saved db.ProjectAgentMembership
	if exists {
		saved, err = q.ReactivateProjectHierarchyMembership(ctx, db.ReactivateProjectHierarchyMembershipParams{
			ID:                  existing.ID,
			ProjectID:           input.ProjectID,
			WorkspaceID:         input.WorkspaceID,
			ManagerMembershipID: input.ManagerMembershipID,
			Title:               input.Title,
		})
	} else {
		newID := tentative[len(tentative)-1].ID
		saved, err = q.CreateProjectHierarchyMembership(ctx, db.CreateProjectHierarchyMembershipParams{
			ID:                  newID,
			WorkspaceID:         input.WorkspaceID,
			ProjectID:           input.ProjectID,
			AgentID:             input.AgentID,
			ManagerMembershipID: input.ManagerMembershipID,
			Title:               input.Title,
		})
	}
	if err != nil {
		return db.ProjectAgentMembership{}, fmt.Errorf("persist hierarchy membership: %w", err)
	}
	if err := commitMutation(ctx, tx); err != nil {
		return db.ProjectAgentMembership{}, err
	}
	return saved, nil
}

func (s *Service) ReparentMembership(ctx context.Context, input ReparentMembershipInput) (db.ProjectAgentMembership, error) {
	if err := requireUUID("membership_id", input.MembershipID); err != nil {
		return db.ProjectAgentMembership{}, err
	}
	if err := requireUUID("manager_membership_id", input.ManagerMembershipID); err != nil {
		return db.ProjectAgentMembership{}, err
	}

	tx, q, rows, err := s.beginProjectMutation(ctx, input.WorkspaceID, input.ProjectID)
	if err != nil {
		return db.ProjectAgentMembership{}, err
	}
	defer tx.Rollback(ctx)

	index, membership, ok := findMembershipByID(rows, input.MembershipID)
	if !ok || membership.ArchivedAt.Valid {
		return db.ProjectAgentMembership{}, domainError(CodeMembershipNotFound, "active membership not found")
	}
	if !membership.ManagerMembershipID.Valid {
		return db.ProjectAgentMembership{}, domainError(CodeRootChangeRequiresBulk, "root changes require an explicit atomic structural operation")
	}
	_, manager, ok := findMembershipByID(rows, input.ManagerMembershipID)
	if !ok || manager.ArchivedAt.Valid {
		return db.ProjectAgentMembership{}, domainError(CodeManagerNotActive, "manager membership is not active in this project")
	}

	tentative := cloneMemberships(rows)
	tentative[index].ManagerMembershipID = input.ManagerMembershipID
	if err := validateActiveTree(tentative); err != nil {
		return db.ProjectAgentMembership{}, err
	}
	if err := validateActiveAgents(ctx, tx, q, input.WorkspaceID, tentative); err != nil {
		return db.ProjectAgentMembership{}, err
	}

	updated, err := q.UpdateProjectHierarchyMembershipManager(ctx, db.UpdateProjectHierarchyMembershipManagerParams{
		ID:                  input.MembershipID,
		ProjectID:           input.ProjectID,
		WorkspaceID:         input.WorkspaceID,
		ManagerMembershipID: input.ManagerMembershipID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.ProjectAgentMembership{}, domainError(CodeMembershipNotFound, "active membership disappeared during mutation")
		}
		return db.ProjectAgentMembership{}, fmt.Errorf("reparent hierarchy membership: %w", err)
	}
	if err := commitMutation(ctx, tx); err != nil {
		return db.ProjectAgentMembership{}, err
	}
	return updated, nil
}

func (s *Service) ArchiveMembership(ctx context.Context, input ArchiveMembershipInput) (db.ProjectAgentMembership, error) {
	if err := requireUUID("membership_id", input.MembershipID); err != nil {
		return db.ProjectAgentMembership{}, err
	}

	tx, q, rows, err := s.beginProjectMutation(ctx, input.WorkspaceID, input.ProjectID)
	if err != nil {
		return db.ProjectAgentMembership{}, err
	}
	defer tx.Rollback(ctx)

	index, membership, ok := findMembershipByID(rows, input.MembershipID)
	if !ok || membership.ArchivedAt.Valid {
		return db.ProjectAgentMembership{}, domainError(CodeMembershipNotFound, "active membership not found")
	}
	if !membership.ManagerMembershipID.Valid {
		return db.ProjectAgentMembership{}, domainError(CodeRootChangeRequiresBulk, "root membership cannot be archived by a leaf operation")
	}
	if activeChildExists(rows, input.MembershipID) {
		return db.ProjectAgentMembership{}, domainError(CodeOrphaningMembership, "membership has active direct reports")
	}

	tentative := cloneMemberships(rows)
	tentative[index].ArchivedAt = pgtype.Timestamptz{Time: time.Now(), Valid: true}
	if err := validateActiveTree(tentative); err != nil {
		return db.ProjectAgentMembership{}, err
	}
	if err := validateActiveAgents(ctx, tx, q, input.WorkspaceID, tentative); err != nil {
		return db.ProjectAgentMembership{}, err
	}

	archived, err := q.ArchiveProjectHierarchyMembership(ctx, db.ArchiveProjectHierarchyMembershipParams{
		ID:          input.MembershipID,
		ProjectID:   input.ProjectID,
		WorkspaceID: input.WorkspaceID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.ProjectAgentMembership{}, domainError(CodeMembershipNotFound, "active membership disappeared during mutation")
		}
		return db.ProjectAgentMembership{}, fmt.Errorf("archive hierarchy membership: %w", err)
	}
	if err := commitMutation(ctx, tx); err != nil {
		return db.ProjectAgentMembership{}, err
	}
	return archived, nil
}

func (s *Service) ApplyStructure(ctx context.Context, input ApplyStructureInput) ([]db.ProjectAgentMembership, error) {
	tx, q, rows, err := s.beginProjectMutation(ctx, input.WorkspaceID, input.ProjectID)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	tentative := cloneMemberships(rows)
	seen := make(map[pgtype.UUID]struct{}, len(input.Changes))
	for _, change := range input.Changes {
		if err := requireUUID("membership_id", change.MembershipID); err != nil {
			return nil, err
		}
		if _, dup := seen[change.MembershipID]; dup {
			return nil, domainError(CodeDuplicateChange, "membership appears more than once in structural change set")
		}
		seen[change.MembershipID] = struct{}{}

		index, membership, ok := findMembershipByID(tentative, change.MembershipID)
		if !ok || membership.ArchivedAt.Valid {
			return nil, domainError(CodeMembershipNotFound, "structural change references an inactive membership")
		}
		tentative[index].ManagerMembershipID = change.ManagerMembershipID
	}

	if err := validateActiveTree(tentative); err != nil {
		return nil, err
	}
	if err := validateActiveAgents(ctx, tx, q, input.WorkspaceID, tentative); err != nil {
		return nil, err
	}

	changes := append([]ManagerChange(nil), input.Changes...)
	sort.Slice(changes, func(i, j int) bool {
		if changes[i].ManagerMembershipID.Valid != changes[j].ManagerMembershipID.Valid {
			// Demote the old root before promoting a new one so the active-root
			// unique index is never transiently violated inside the transaction.
			return changes[i].ManagerMembershipID.Valid
		}
		return bytes.Compare(changes[i].MembershipID.Bytes[:], changes[j].MembershipID.Bytes[:]) < 0
	})

	for _, change := range changes {
		if _, err := q.UpdateProjectHierarchyMembershipManager(ctx, db.UpdateProjectHierarchyMembershipManagerParams{
			ID:                  change.MembershipID,
			ProjectID:           input.ProjectID,
			WorkspaceID:         input.WorkspaceID,
			ManagerMembershipID: change.ManagerMembershipID,
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, domainError(CodeMembershipNotFound, "active membership disappeared during structural mutation")
			}
			return nil, fmt.Errorf("apply hierarchy structural change: %w", err)
		}
	}

	persisted, err := q.ListProjectHierarchyMemberships(ctx, db.ListProjectHierarchyMembershipsParams{
		ProjectID:   input.ProjectID,
		WorkspaceID: input.WorkspaceID,
	})
	if err != nil {
		return nil, fmt.Errorf("reload hierarchy after structural mutation: %w", err)
	}
	if err := validateActiveTree(persisted); err != nil {
		return nil, fmt.Errorf("revalidate persisted hierarchy: %w", err)
	}
	if err := validateActiveAgents(ctx, tx, q, input.WorkspaceID, persisted); err != nil {
		return nil, fmt.Errorf("revalidate persisted hierarchy agents: %w", err)
	}
	if err := commitMutation(ctx, tx); err != nil {
		return nil, err
	}
	return persisted, nil
}
