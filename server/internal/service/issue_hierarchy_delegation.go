package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/projecthierarchy"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var (
	ErrDelegationInvalid   = errors.New("invalid internal delegation request")
	ErrDelegationConflict  = errors.New("delegation request identity already used for different work")
	ErrDelegationChildGone = errors.New("delegated child no longer exists in this workspace")
)

// HierarchyDelegationCreate is an internal admission contract, never a request
// DTO. A future authenticated transport must supply fresh actor/task,
// invocation and runtime authorization through Authorize, including on replay.
// There is intentionally no production caller in this increment.
type HierarchyDelegationCreate struct {
	RequestID pgtype.UUID
	Authorize func(context.Context, *db.Queries, projecthierarchy.DelegationRelation) error
}

func prepareHierarchyDelegation(ctx context.Context, tx pgx.Tx, p IssueCreateParams, opts IssueCreateOpts, childID pgtype.UUID) (*IssueCreateResult, error) {
	d := opts.HierarchyDelegation
	if d == nil {
		return nil, nil
	}
	if !d.RequestID.Valid || d.RequestID.Bytes == [16]byte{} || d.Authorize == nil ||
		p.CreatorType != "agent" || !p.AssigneeType.Valid || p.AssigneeType.String != "agent" ||
		strings.TrimSpace(p.Title) == "" || p.Status != "todo" ||
		p.SourceContext != nil || len(p.AttachmentIDs) != 0 || len(p.LabelIDs) != 0 ||
		len(p.Properties) != 0 || p.StartDate.Valid || p.DueDate.Valid || p.Stage.Valid ||
		!opts.AssignedAgentRunFireAt.IsZero() || !p.OriginType.Valid ||
		p.OriginType.String != "agent_create" || !p.OriginID.Valid || p.OriginID.Bytes == [16]byte{} {
		return nil, ErrDelegationInvalid
	}
	relation, err := projecthierarchy.CheckDelegationRelation(ctx, tx, projecthierarchy.DelegationRelationInput{
		WorkspaceID: p.WorkspaceID, ProjectID: p.ProjectID, ParentID: p.ParentIssueID,
		DelegatorID: p.CreatorID, TargetID: p.AssigneeID,
	})
	if err != nil {
		return nil, err
	}
	qtx := db.New(tx)
	if err := d.Authorize(ctx, qtx, relation); err != nil {
		return nil, err
	}
	// Only these work fields are supported. Origin task is lineage, not intent:
	// a later authenticated Agent turn can retry the same logical operation.
	intent, err := json.Marshal(struct {
		ProjectID   pgtype.UUID
		TargetID    pgtype.UUID
		Title       string
		Description pgtype.Text
		Priority    string
	}{p.ProjectID, p.AssigneeID, p.Title, p.Description, p.Priority})
	if err != nil {
		return nil, fmt.Errorf("encode delegation intent: %w", err)
	}
	hash := sha256.Sum256(intent)
	var existingID pgtype.UUID
	var existingHash []byte
	err = tx.QueryRow(ctx, `
SELECT child_issue_id, request_hash FROM project_hierarchy_delegation
WHERE workspace_id = $1 AND parent_issue_id = $2 AND delegator_agent_id = $3 AND request_id = $4
`, p.WorkspaceID, p.ParentIssueID, p.CreatorID, d.RequestID).Scan(&existingID, &existingHash)
	if err == nil {
		if !bytes.Equal(existingHash, hash[:]) {
			return nil, ErrDelegationConflict
		}
		child, err := qtx.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: existingID, WorkspaceID: p.WorkspaceID})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrDelegationChildGone
		}
		if err != nil {
			return nil, fmt.Errorf("load delegated child: %w", err)
		}
		return &IssueCreateResult{Issue: child, DelegationReplayed: true}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("load delegation identity: %w", err)
	}
	// Workspace serialization from the relation gate prevents two creators
	// reaching this insert concurrently. The unique index is a final backstop.
	if _, err := tx.Exec(ctx, `
INSERT INTO project_hierarchy_delegation
(workspace_id, parent_issue_id, delegator_agent_id, request_id, project_id, target_agent_id, child_issue_id, source_task_id, request_hash)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
`, p.WorkspaceID, p.ParentIssueID, p.CreatorID, d.RequestID, p.ProjectID, p.AssigneeID, childID, p.OriginID, hash[:]); err != nil {
		return nil, fmt.Errorf("record delegation identity: %w", err)
	}
	return nil, nil
}
