package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var ErrHierarchyDispatchUnavailable = errors.New("hierarchy initial task unavailable")

// prepareHierarchyDispatch reserves the native sole assigned-task slot in the
// Issue transaction. The native historical media-pending marker is also its
// pre-claim uniqueness/coalescing fence; retain it rather than inventing a queue.
func (s *IssueService) prepareHierarchyDispatch(ctx context.Context, tx pgx.Tx, issue db.Issue, opts IssueCreateOpts) (db.AgentTaskQueue, error) {
	// Root human intervention has no Agent task.
	if !issue.AssigneeID.Valid {
		return db.AgentTaskQueue{}, nil
	}
	if s.TaskService == nil {
		return db.AgentTaskQueue{}, errors.New("hierarchy task service unavailable")
	}
	q := s.Queries.WithTx(tx)
	if !s.shouldEnqueueAgentTaskWithQueries(ctx, q, issue) {
		return db.AgentTaskQueue{}, ErrHierarchyDispatchUnavailable
	}
	var deadline time.Time
	if err := tx.QueryRow(ctx, "SELECT clock_timestamp() + interval '1 minute'").Scan(&deadline); err != nil {
		return db.AgentTaskQueue{}, fmt.Errorf("hierarchy dispatch deadline: %w", err)
	}
	// Native transaction-scoped planning does not wire a bus, daemon notifier or
	// optional external MCP client. Hydrate/promote only after commit.
	task, err := s.TaskService.createDeferredChannelIssueTaskWithQueries(ctx, q, issue, deadline)
	if err != nil {
		return db.AgentTaskQueue{}, fmt.Errorf("prepare hierarchy initial task: %w", err)
	}
	var rows int64
	if d := opts.HierarchyDelegation; d != nil {
		tag, err := tx.Exec(ctx, `UPDATE project_hierarchy_delegation SET initial_task_id=$1 WHERE workspace_id=$2 AND parent_issue_id=$3 AND delegator_agent_id=$4 AND request_id=$5 AND child_issue_id=$6 AND initial_task_id IS NULL`, task.ID, issue.WorkspaceID, issue.ParentIssueID, issue.CreatorID, d.RequestID, issue.ID)
		if err != nil {
			return db.AgentTaskQueue{}, fmt.Errorf("record delegation initial task: %w", err)
		}
		rows = tag.RowsAffected()
	} else if e := opts.HierarchyEscalation; e != nil {
		tag, err := tx.Exec(ctx, `UPDATE project_hierarchy_escalation SET initial_task_id=$1 WHERE workspace_id=$2 AND source_issue_id=$3 AND source_agent_id=$4 AND request_id=$5 AND decision_issue_id=$6 AND NOT human_intervention AND initial_task_id IS NULL`, task.ID, issue.WorkspaceID, issue.ParentIssueID, issue.CreatorID, e.RequestID, issue.ID)
		if err != nil {
			return db.AgentTaskQueue{}, fmt.Errorf("record escalation initial task: %w", err)
		}
		rows = tag.RowsAffected()
	}
	if rows != 1 {
		return db.AgentTaskQueue{}, errors.New("hierarchy initial task receipt mismatch")
	}
	return task, nil
}

func (s *IssueService) finishHierarchyDispatch(ctx context.Context, task db.AgentTaskQueue) {
	if !task.ID.Valid {
		return
	}
	if err := s.TaskService.hydrateDeferredChannelIssueTaskOverlay(ctx, task); err != nil {
		slog.Warn("hierarchy initial task overlay hydration failed", "task_id", util.UUIDToString(task.ID), "error", err)
	}
	if err := s.TaskService.PromoteDeferredChannelIssueTask(ctx, task.ID); err != nil {
		// The durable DB-clock deadline and native polling recover this same row.
		// Do not report an uncommitted failure or enqueue a replacement after commit.
		slog.Warn("hierarchy initial task promotion deferred to native polling", "task_id", util.UUIDToString(task.ID), "error", err)
	}
}
