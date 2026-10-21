package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/projecthierarchy"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type hierarchyDispatchFixture struct {
	f      *hierarchyCreateFixture
	create func(context.Context) (IssueCreateResult, error)
	ledger string
}

func newHierarchyDispatchFixture(t *testing.T, escalation bool) hierarchyDispatchFixture {
	t.Helper()
	var f *hierarchyCreateFixture
	var create func(context.Context) (IssueCreateResult, error)
	ledger := "project_hierarchy_delegation"
	if escalation {
		var in HierarchyEscalationCreate
		f, in = escalationFixture(t, true)
		create = func(ctx context.Context) (IssueCreateResult, error) {
			return f.svc.Escalate(ctx, in, IssueCreateOpts{})
		}
		ledger = "project_hierarchy_escalation"
	} else {
		f = newHierarchyCreateFixture(t)
		create = func(ctx context.Context) (IssueCreateResult, error) { return f.svc.Create(ctx, f.p, f.opts) }
	}
	f.fx.Cleanup(t, "DELETE FROM agent_task_queue WHERE issue_id IN (SELECT id FROM issue WHERE parent_issue_id=$1)", f.p.ParentIssueID)
	return hierarchyDispatchFixture{f, create, ledger}
}
func (d hierarchyDispatchFixture) crashAfterCommit(t *testing.T) db.AgentTaskQueue {
	t.Helper()
	crash := errors.New("simulated server crash after commit")
	d.f.svc.TxStarter = &afterCommitTxStarter{pool: d.f.fx.Pool, afterCommit: func() { panic(crash) }}
	var caught any
	func() {
		defer func() { caught = recover() }()
		_, err := d.create(context.Background())
		if err != nil {
			t.Errorf("create before crash: %v", err)
		}
	}()
	d.f.svc.TxStarter = d.f.fx.Pool
	if caught != crash {
		t.Fatalf("commit crash=%v", caught)
	}
	var id pgtype.UUID
	d.f.fx.QueryRow(t, "SELECT initial_task_id FROM "+d.ledger+" WHERE workspace_id=$1", d.f.p.WorkspaceID).Scan(&id)
	if !id.Valid {
		t.Fatal("committed receipt has no initial task")
	}
	task, err := d.f.svc.Queries.GetAgentTask(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != "deferred" || !task.FireAt.Valid {
		t.Fatalf("crash task=%+v", task)
	}
	var fenced bool
	d.f.fx.QueryRow(t, "SELECT context->>'channel_issue_media_pending'='true' FROM agent_task_queue WHERE id=$1", id).Scan(&fenced)
	if !fenced || d.f.events.Load() != 0 {
		t.Fatalf("native reservation/event before hot path: %v / %d", fenced, d.f.events.Load())
	}
	if task.OriginatorUserID != util.MustParseUUID(d.f.fx.UserID) || task.DelegatedFromTaskID != d.f.p.OriginID {
		t.Fatalf("lost native lineage: %+v", task)
	}
	return task
}

func TestHierarchyDispatchCrashRecoveryUsesTheSameNativeTask(t *testing.T) {
	for _, escalation := range []bool{false, true} {
		name := "delegation"
		if escalation {
			name = "escalation"
		}
		t.Run(name, func(t *testing.T) {
			d := newHierarchyDispatchFixture(t, escalation)
			task := d.crashAfterCommit(t)
			replay, err := d.create(context.Background())
			if err != nil || replay.AssignedTaskID != task.ID {
				t.Fatalf("replay receipt: %+v / %v", replay, err)
			}
			current, err := d.f.svc.Queries.GetAgentTask(context.Background(), task.ID)
			if err != nil || current.Status != "deferred" {
				t.Fatalf("replay promoted work: %+v / %v", current, err)
			}
			d.f.fx.Exec(t, "UPDATE agent_task_queue SET fire_at=clock_timestamp()-interval '1 second' WHERE id=$1", task.ID)
			var queued atomic.Int32
			bus := events.New()
			bus.Subscribe(protocol.EventTaskQueued, func(events.Event) { queued.Add(1) })
			type result struct {
				tasks []db.AgentTaskQueue
				err   error
			}
			results := make(chan result, 4)
			start := make(chan struct{})
			for i := range 4 {
				go func() {
					<-start
					s := NewTaskService(db.New(d.f.fx.Pool), d.f.fx.Pool, nil, bus)
					if i%2 == 0 {
						tasks, err := s.ClaimTasksForRuntimes(context.Background(), []pgtype.UUID{task.RuntimeID}, 4)
						results <- result{tasks, err}
					} else {
						one, err := s.ClaimTaskForRuntime(context.Background(), task.RuntimeID)
						var tasks []db.AgentTaskQueue
						if one != nil {
							tasks = append(tasks, *one)
						}
						results <- result{tasks, err}
					}
				}()
			}
			close(start)
			claimed := 0
			for range 4 {
				r := <-results
				if r.err != nil {
					t.Fatal(r.err)
				}
				for _, got := range r.tasks {
					if got.ID != task.ID {
						t.Fatalf("replacement task=%s", util.UUIDToString(got.ID))
					}
					claimed++
				}
			}
			if claimed != 1 || queued.Load() != 1 {
				t.Fatalf("restart claims/queued events=%d/%d", claimed, queued.Load())
			}
			if n := d.f.fx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id=$1", task.IssueID); n != 1 {
				t.Fatalf("recovery tasks=%d", n)
			}
			current, err = d.f.svc.Queries.GetAgentTask(context.Background(), task.ID)
			if err != nil || current.Status != "dispatched" {
				t.Fatalf("recovered task: %+v / %v", current, err)
			}
		})
	}
}

func TestHierarchyDispatchReplayNeverRecreatesExecutionHistory(t *testing.T) {
	for _, escalation := range []bool{false, true} {
		name := "delegation"
		if escalation {
			name = "escalation"
		}
		t.Run(name, func(t *testing.T) {
			d := newHierarchyDispatchFixture(t, escalation)
			first, err := d.create(context.Background())
			if err != nil || !first.AssignedTaskID.Valid {
				t.Fatalf("initial task: %+v / %v", first, err)
			}
			for _, status := range []string{"deferred", "dispatched", "waiting_local_directory", "running", "completed", "failed", "cancelled"} {
				d.f.fx.Exec(t, "UPDATE agent_task_queue SET status=$1,fire_at=clock_timestamp()+interval '1 hour' WHERE id=$2", status, first.AssignedTaskID)
				replay, err := d.create(context.Background())
				if err != nil || replay.AssignedTaskID != first.AssignedTaskID {
					t.Fatalf("%s replay=%+v / %v", status, replay, err)
				}
				var got string
				d.f.fx.QueryRow(t, "SELECT status FROM agent_task_queue WHERE id=$1", first.AssignedTaskID).Scan(&got)
				if got != status {
					t.Fatalf("replay restarted %s as %s", status, got)
				}
			}
			d.f.fx.Exec(t, "DELETE FROM agent_task_queue WHERE id=$1", first.AssignedTaskID)
			replay, err := d.create(context.Background())
			if err != nil || replay.AssignedTaskID != first.AssignedTaskID {
				t.Fatalf("deleted execution receipt=%+v / %v", replay, err)
			}
			d.f.fx.Exec(t, "UPDATE "+d.ledger+" SET initial_task_id=NULL WHERE workspace_id=$1", d.f.p.WorkspaceID)
			replay, err = d.create(context.Background())
			if err != nil || replay.AssignedTaskID.Valid {
				t.Fatalf("legacy replay=%+v / %v", replay, err)
			}
			if n := d.f.fx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id=$1", first.Issue.ID); n != 0 {
				t.Fatalf("ambiguous history replayed %d tasks", n)
			}
			if d.f.events.Load() != 1 {
				t.Fatalf("replay creation events=%d", d.f.events.Load())
			}
		})
	}
}

func TestHierarchyDispatchNativeCommentMergeKeepsItsReservedTask(t *testing.T) {
	d := newHierarchyDispatchFixture(t, false)
	task := d.crashAfterCommit(t)
	ctx := context.Background()
	q := d.f.svc.Queries
	issue, err := q.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: task.IssueID, WorkspaceID: d.f.p.WorkspaceID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = d.f.svc.TaskService.EnqueueTaskForIssue(ctx, issue); !errors.Is(err, ErrDuplicatePendingTask) {
		t.Fatalf("competing enqueue=%v", err)
	}
	comment := util.MustParseUUID(d.f.fx.Comment(t, util.UUIDToString(task.IssueID), "Additional context"))
	// Native coalescing is thread-scoped. Establish this task's effective
	// comment thread before testing its same-thread merge/overlay protection.
	d.f.fx.Exec(t, "UPDATE agent_task_queue SET trigger_comment_id=$2 WHERE id=$1", task.ID, comment)
	overlay := json.RawMessage(`{"mcpServers":{"comment":{"command":"fixture-only"}}}`)
	merged, err := q.MergeCommentIntoPendingTask(ctx, db.MergeCommentIntoPendingTaskParams{IssueID: task.IssueID, AgentID: task.AgentID, NewTriggerCommentID: comment, NewOriginatorUserID: task.OriginatorUserID, NewAccountableUserID: task.AccountableUserID, NewOriginatorSource: pgtype.Text{String: "direct_human", Valid: true}, NewTriggerEvidenceKind: pgtype.Text{String: "comment", Valid: true}, NewTriggerEvidenceRefID: comment, NewRuntimeMcpOverlay: overlay})
	if err != nil || merged.ID != task.ID {
		t.Fatalf("native merge=%+v / %v", merged, err)
	}
	builder := &stubOverlayBuilder{resp: json.RawMessage(`{"mcpServers":{"stale":{"command":"must-not-win"}}}`)}
	d.f.svc.TaskService.Composio = builder
	d.f.svc.TaskService.FeatureFlags = composioMCPAppsTestFlags(true)
	d.f.svc.finishHierarchyDispatch(ctx, task)
	got, err := q.GetAgentTask(ctx, task.ID)
	if err != nil || got.TriggerCommentID != comment || got.Status != "queued" || !strings.Contains(string(got.RuntimeMcpOverlay), "comment") || strings.Contains(string(got.RuntimeMcpOverlay), "stale") {
		t.Fatalf("merged plan overwritten: %+v / %v", got, err)
	}
	if n := d.f.fx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id=$1", task.IssueID); n != 1 {
		t.Fatalf("comment created %d tasks", n)
	}
}

type hierarchyReceiptRejectStarter struct{ pool *pgxpool.Pool }

func (s hierarchyReceiptRejectStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return hierarchyReceiptRejectTx{tx}, nil
}

type hierarchyReceiptRejectTx struct{ pgx.Tx }

func (t hierarchyReceiptRejectTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if strings.Contains(sql, "SET initial_task_id=") {
		return pgconn.CommandTag{}, errors.New("fixture receipt write rejection")
	}
	return t.Tx.Exec(ctx, sql, args...)
}

func TestHierarchyDispatchRollbackCoversNativeTaskAndReceipt(t *testing.T) {
	for _, escalation := range []bool{false, true} {
		name := "delegation"
		if escalation {
			name = "escalation"
		}
		t.Run(name, func(t *testing.T) {
			d := newHierarchyDispatchFixture(t, escalation)
			d.f.svc.TxStarter = hierarchyReceiptRejectStarter{d.f.fx.Pool}
			if _, err := d.create(context.Background()); err == nil {
				t.Fatal("receipt failure committed")
			}
			if n := d.f.fx.Count(t, "SELECT count(*) FROM "+d.ledger+" WHERE workspace_id=$1", d.f.p.WorkspaceID); n != 0 {
				t.Fatalf("receipt leak=%d", n)
			}
			if n := d.f.fx.Count(t, "SELECT count(*) FROM issue WHERE parent_issue_id=$1", d.f.p.ParentIssueID); n != 0 {
				t.Fatalf("Issue leak=%d", n)
			}
			if n := d.f.fx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE id<>$1 AND agent_id IN ($2,$3)", d.f.p.OriginID, d.f.p.CreatorID, d.f.p.AssigneeID); n != 0 {
				t.Fatalf("task leak=%d", n)
			}
			if d.f.events.Load() != 0 {
				t.Fatal("rollback emitted creation")
			}
		})
	}
}

func TestHierarchyDispatchAdmissionCannotCommitWithoutReadyTask(t *testing.T) {
	d := newHierarchyDispatchFixture(t, false)
	d.f.opts.HierarchyDelegation.Authorize = func(context.Context, *db.Queries, projecthierarchy.DelegationRelation) error { return nil }
	d.f.fx.Exec(t, "UPDATE agent SET runtime_id=NULL WHERE id=$1", d.f.p.AssigneeID)
	if _, err := d.create(context.Background()); !errors.Is(err, ErrHierarchyDispatchUnavailable) {
		t.Fatalf("readiness=%v", err)
	}
	if n := d.f.fx.Count(t, "SELECT count(*) FROM issue WHERE parent_issue_id=$1", d.f.p.ParentIssueID); n != 0 {
		t.Fatal("committed undeliverable Issue")
	}
}

func TestHierarchyDispatchPromotionCannotUndoConcurrentClaimOrCancel(t *testing.T) {
	for _, batch := range []bool{false, true} {
		for _, status := range []string{"dispatched", "cancelled"} {
			name := "single/" + status
			if batch {
				name = "batch/" + status
			}
			t.Run(name, func(t *testing.T) {
				d := newHierarchyDispatchFixture(t, false)
				task := d.crashAfterCommit(t)
				ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
				defer cancel()
				d.f.fx.Exec(t, "UPDATE agent_task_queue SET fire_at=clock_timestamp()-interval '1 second' WHERE id=$1", task.ID)
				conn, err := d.f.fx.Pool.Acquire(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer conn.Release()
				pid := conn.Conn().PgConn().PID()
				tx, err := d.f.fx.Pool.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback(context.Background())
				var locked pgtype.UUID
				if err = tx.QueryRow(ctx, "SELECT id FROM agent_task_queue WHERE id=$1 FOR UPDATE", task.ID).Scan(&locked); err != nil {
					t.Fatal(err)
				}
				type result struct {
					tasks []db.AgentTaskQueue
					err   error
				}
				done := make(chan result, 1)
				go func() {
					q := db.New(conn)
					var tasks []db.AgentTaskQueue
					var err error
					if batch {
						tasks, err = q.PromoteDueDeferredTasksForRuntimes(ctx, db.PromoteDueDeferredTasksForRuntimesParams{RuntimeIds: []pgtype.UUID{task.RuntimeID}, RuntimeStaleSecs: 120})
					} else {
						tasks, err = q.PromoteDueDeferredTasksForRuntime(ctx, db.PromoteDueDeferredTasksForRuntimeParams{RuntimeID: task.RuntimeID, RuntimeStaleSecs: 120})
					}
					done <- result{tasks, err}
				}()
				blocked := false
				deadline := time.Now().Add(5 * time.Second)
				for time.Now().Before(deadline) {
					if err = d.f.fx.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND wait_event_type='Lock')", pid).Scan(&blocked); err != nil {
						t.Fatal(err)
					}
					if blocked {
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
				if !blocked {
					t.Fatal("promoter never waited on the native task row")
				}
				if _, err = tx.Exec(ctx, "UPDATE agent_task_queue SET status=$1 WHERE id=$2", status, task.ID); err != nil {
					t.Fatal(err)
				}
				if err = tx.Commit(ctx); err != nil {
					t.Fatal(err)
				}
				r := <-done
				if r.err != nil || len(r.tasks) != 0 {
					t.Fatalf("stale promotion reactivated %s: %+v / %v", status, r.tasks, r.err)
				}
				var actual string
				d.f.fx.QueryRow(t, "SELECT status FROM agent_task_queue WHERE id=$1", task.ID).Scan(&actual)
				if actual != status {
					t.Fatalf("concurrent %s became %s", status, actual)
				}
			})
		}
	}
}

func TestHierarchyDispatchLateContinuationPreservesExecutionHistory(t *testing.T) {
	for _, column := range []string{"dispatched_at", "started_at"} {
		t.Run(column, func(t *testing.T) {
			d := newHierarchyDispatchFixture(t, false)
			task := d.crashAfterCommit(t)
			d.f.fx.Exec(t, "UPDATE agent_task_queue SET "+column+"=clock_timestamp() WHERE id=$1", task.ID)
			builder := &stubOverlayBuilder{resp: json.RawMessage(`{"mcpServers":{"late":{"command":"must-not-win"}}}`)}
			d.f.svc.TaskService.Composio = builder
			d.f.svc.TaskService.FeatureFlags = composioMCPAppsTestFlags(true)
			d.f.svc.finishHierarchyDispatch(context.Background(), task)
			got, err := d.f.svc.Queries.GetAgentTask(context.Background(), task.ID)
			if err != nil || got.Status != "deferred" || strings.Contains(string(got.RuntimeMcpOverlay), "late") {
				t.Fatalf("late continuation changed execution history: %+v / %v", got, err)
			}
		})
	}
}
