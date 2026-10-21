package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	inboxwriter "github.com/multica-ai/multica/server/internal/inbox"
	"github.com/multica-ai/multica/server/internal/projecthierarchy"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

var (
	ErrEscalationInvalid      = errors.New("invalid internal escalation request")
	ErrEscalationConflict     = errors.New("escalation request identity already used for different intent or route")
	ErrEscalationDecisionGone = errors.New("escalation decision Issue was deleted")
	ErrEscalationNoHuman      = errors.New("no eligible human intervention recipient")
)

// HierarchyEscalationCreate is server-resolved input, never an HTTP request DTO.
// Authorize must freshly validate current MAT/task ownership and native manager
// invocation/runtime permission, including replay; it is not caller-controlled.
type HierarchyEscalationCreate struct {
	WorkspaceID, ProjectID, IssueID, SourceAgentID, SourceTaskID, RequestID pgtype.UUID
	Reason, Context                                                         string
	Authorize                                                               func(context.Context, *db.Queries, projecthierarchy.EscalationRelation) error
}

type escalationCreateState struct {
	input    HierarchyEscalationCreate
	relation projecthierarchy.EscalationRelation
	inbox    []db.InboxItem
}

// Escalate creates a native manager decision or root human-intervention Issue.
// Successful replay does not retry enqueue; native crash reconciliation is R3.
func (s *IssueService) Escalate(ctx context.Context, in HierarchyEscalationCreate, opts IssueCreateOpts) (IssueCreateResult, error) {
	in.Reason = strings.TrimSpace(in.Reason)
	in.Context = strings.TrimSpace(in.Context)
	if !validEscalation(in) || opts.HierarchyDelegation != nil || opts.HierarchyEscalation != nil || !opts.AssignedAgentRunFireAt.IsZero() {
		return IssueCreateResult{}, ErrEscalationInvalid
	}
	opts.HierarchyEscalation = &in
	opts.ActorID = util.UUIDToString(in.SourceAgentID)
	opts.AnalyticsAgentID = opts.ActorID
	return s.Create(ctx, IssueCreateParams{
		WorkspaceID: in.WorkspaceID, ProjectID: in.ProjectID, ParentIssueID: in.IssueID,
		Title: "Escalation", Status: "todo", Priority: "none", CreatorType: "agent", CreatorID: in.SourceAgentID,
		OriginType: pgtype.Text{String: "agent_create", Valid: true}, OriginID: in.SourceTaskID,
	}, opts)
}

func validEscalation(in HierarchyEscalationCreate) bool {
	if in.Authorize == nil || in.Reason == "" || len(in.Reason) > 8000 || len(in.Context) > 16000 || !utf8.ValidString(in.Reason) || !utf8.ValidString(in.Context) || strings.ContainsRune(in.Reason+in.Context, 0) {
		return false
	}
	for _, id := range []pgtype.UUID{in.WorkspaceID, in.ProjectID, in.IssueID, in.SourceAgentID, in.SourceTaskID, in.RequestID} {
		if !id.Valid || id.Bytes == [16]byte{} {
			return false
		}
	}
	return true
}

func prepareHierarchyEscalation(ctx context.Context, tx pgx.Tx, p *IssueCreateParams, in HierarchyEscalationCreate, decisionID pgtype.UUID) (*escalationCreateState, *IssueCreateResult, error) {
	if !validEscalation(in) || p.WorkspaceID != in.WorkspaceID || p.ProjectID != in.ProjectID || p.ParentIssueID != in.IssueID || p.CreatorType != "agent" || p.CreatorID != in.SourceAgentID || p.OriginID != in.SourceTaskID || !p.OriginType.Valid || p.OriginType.String != "agent_create" || len(p.AttachmentIDs) > 0 || len(p.LabelIDs) > 0 || len(p.Properties) > 0 || p.SourceContext != nil || p.Stage.Valid || p.StartDate.Valid || p.DueDate.Valid {
		return nil, nil, ErrEscalationInvalid
	}
	r, err := projecthierarchy.CheckEscalationRelation(ctx, tx, projecthierarchy.EscalationRelationInput{WorkspaceID: in.WorkspaceID, ProjectID: in.ProjectID, IssueID: in.IssueID, SourceAgentID: in.SourceAgentID})
	if err != nil {
		return nil, nil, err
	}
	q := db.New(tx)
	task, err := q.GetAgentTaskInWorkspace(ctx, db.GetAgentTaskInWorkspaceParams{ID: in.SourceTaskID, WorkspaceID: in.WorkspaceID})
	if err != nil {
		return nil, nil, err
	}
	if task.AgentID != in.SourceAgentID || task.IssueID != in.IssueID || task.Status != "running" || task.SquadID.Valid || task.IsLeaderTask {
		return nil, nil, ErrEscalationInvalid
	}
	if err := in.Authorize(ctx, q, r); err != nil {
		return nil, nil, err
	}
	intent, err := json.Marshal(struct {
		ProjectID, ManagerMembershipID, ManagerAgentID pgtype.UUID
		Human                                          bool
		Reason, Context                                string
	}{in.ProjectID, r.ManagerMembershipID, r.ManagerAgentID, r.HumanInterventionRequired, in.Reason, in.Context})
	if err != nil {
		return nil, nil, err
	}
	hash := sha256.Sum256(intent)
	var oldDecision pgtype.UUID
	var oldHash []byte
	err = tx.QueryRow(ctx, `SELECT decision_issue_id,request_hash FROM project_hierarchy_escalation WHERE workspace_id=$1 AND source_issue_id=$2 AND source_agent_id=$3 AND request_id=$4`, in.WorkspaceID, in.IssueID, in.SourceAgentID, in.RequestID).Scan(&oldDecision, &oldHash)
	if err == nil {
		if !bytes.Equal(hash[:], oldHash) {
			return nil, nil, ErrEscalationConflict
		}
		old, err := q.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: oldDecision, WorkspaceID: in.WorkspaceID})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, ErrEscalationDecisionGone
		}
		if err != nil {
			return nil, nil, err
		}
		return nil, &IssueCreateResult{Issue: old, EscalationReplayed: true}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, err
	}
	source, err := q.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: in.IssueID, WorkspaceID: in.WorkspaceID})
	if err != nil {
		return nil, nil, err
	}
	title := []rune(source.Title)
	if len(title) > 180 {
		title = title[:180]
	}
	p.Title = "Escalation: " + string(title)
	p.Status = "todo"
	p.Priority = source.Priority
	p.AssigneeType = pgtype.Text{String: "agent", Valid: true}
	p.AssigneeID = r.ManagerAgentID
	if r.HumanInterventionRequired {
		p.Title = "Human intervention: " + string(title)
		p.AssigneeType = pgtype.Text{}
		p.AssigneeID = pgtype.UUID{}
	}
	p.Description = pgtype.Text{String: fmt.Sprintf("Source Issue: %s\n\nReason:\n%s\n\nContext:\n%s", util.UUIDToString(in.IssueID), in.Reason, in.Context), Valid: true}
	_, err = tx.Exec(ctx, `INSERT INTO project_hierarchy_escalation(workspace_id,source_issue_id,source_agent_id,request_id,project_id,source_membership_id,manager_membership_id,manager_agent_id,human_intervention,decision_issue_id,source_task_id,reason,context,request_hash)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`, in.WorkspaceID, in.IssueID, in.SourceAgentID, in.RequestID, in.ProjectID, r.SourceMembershipID, r.ManagerMembershipID, r.ManagerAgentID, r.HumanInterventionRequired, decisionID, in.SourceTaskID, in.Reason, in.Context, hash[:])
	if err != nil {
		return nil, nil, fmt.Errorf("record escalation identity: %w", err)
	}
	return &escalationCreateState{input: in, relation: r}, nil, nil
}

func (st *escalationCreateState) finish(ctx context.Context, tx pgx.Tx, issue db.Issue) error {
	if !st.relation.HumanInterventionRequired {
		return nil
	}
	rows, err := tx.Query(ctx, `SELECT DISTINCT m.user_id FROM member m WHERE m.workspace_id=$1 AND
 (m.role IN ('owner','admin') OR m.user_id=(SELECT lead_id FROM project WHERE id=$2 AND workspace_id=$1 AND lead_type='member')) ORDER BY m.user_id`, issue.WorkspaceID, issue.ProjectID)
	if err != nil {
		return err
	}
	var recipients []pgtype.UUID
	for rows.Next() {
		var id pgtype.UUID
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		recipients = append(recipients, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	details, err := json.Marshal(map[string]any{"source_issue_id": util.UUIDToString(st.input.IssueID), "source_agent_id": util.UUIDToString(st.input.SourceAgentID), "request_id": util.UUIDToString(st.input.RequestID), "project_id": util.UUIDToString(issue.ProjectID), "human_intervention": true})
	if err != nil {
		return err
	}
	for _, recipient := range recipients {
		item, inserted, err := inboxwriter.CreateIfEligible(ctx, db.New(tx), db.CreateInboxItemParams{ID: dbid.NewV7(), WorkspaceID: issue.WorkspaceID, RecipientType: "member", RecipientID: recipient, Type: "hierarchy_escalation", Severity: "action_required", IssueID: issue.ID, Title: issue.Title, Body: pgtype.Text{String: st.input.Reason, Valid: true}, ActorType: pgtype.Text{String: "agent", Valid: true}, ActorID: st.input.SourceAgentID, Details: details})
		if err != nil {
			return err
		}
		if inserted {
			st.inbox = append(st.inbox, item)
		}
	}
	if len(st.inbox) == 0 {
		return ErrEscalationNoHuman
	}
	return nil
}
