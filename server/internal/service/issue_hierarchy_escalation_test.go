package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/projecthierarchy"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func escalationFixture(t *testing.T, manager bool) (*hierarchyCreateFixture, HierarchyEscalationCreate) {
	t.Helper()
	f := newHierarchyCreateFixture(t)
	source := f.p.CreatorID
	if manager {
		source = f.p.AssigneeID
		f.fx.Exec(t, "UPDATE issue SET assignee_id=$1 WHERE id=$2", source, f.p.ParentIssueID)
		f.fx.Exec(t, "UPDATE agent_task_queue SET agent_id=$1 WHERE id=$2", source, f.p.OriginID)
	}
	f.fx.Cleanup(t, "DELETE FROM project_hierarchy_escalation WHERE workspace_id=$1", f.p.WorkspaceID)
	in := HierarchyEscalationCreate{WorkspaceID: f.p.WorkspaceID, ProjectID: f.p.ProjectID, IssueID: f.p.ParentIssueID, SourceAgentID: source, SourceTaskID: f.p.OriginID, RequestID: util.MustParseUUID(uuid.NewString()), Reason: "Need a decision", Context: "Two incompatible requirements"}
	in.Authorize = func(context.Context, *db.Queries, projecthierarchy.EscalationRelation) error { return nil }
	return f, in
}

func TestHierarchyEscalationManagerConcurrentReplayAndLineage(t *testing.T) {
	f, in := escalationFixture(t, true)
	type outcome struct {
		r   IssueCreateResult
		err error
	}
	results := make(chan outcome, 2)
	start := make(chan struct{})
	for range 2 {
		go func() {
			<-start
			r, err := f.svc.Escalate(context.Background(), in, IssueCreateOpts{})
			results <- outcome{r, err}
		}()
	}
	close(start)
	a, b := <-results, <-results
	if a.err != nil || b.err != nil {
		t.Fatalf("concurrent escalation: %v / %v", a.err, b.err)
	}
	if a.r.Issue.ID != b.r.Issue.ID || a.r.EscalationReplayed == b.r.EscalationReplayed {
		t.Fatalf("duplicate decisions: %+v / %+v", a.r, b.r)
	}
	decision := a.r.Issue
	if decision.AssigneeID != f.p.CreatorID || decision.ParentIssueID != in.IssueID || decision.ProjectID != in.ProjectID || decision.OriginID != in.SourceTaskID {
		t.Fatalf("decision lineage: %+v", decision)
	}
	var human string
	f.fx.QueryRow(t, "SELECT originator_user_id::text FROM agent_task_queue WHERE issue_id=$1", decision.ID).Scan(&human)
	if human != f.fx.UserID {
		t.Fatalf("lost native originator: %s", human)
	}
	f.fx.Exec(t, "UPDATE agent_task_queue SET status='completed' WHERE issue_id=$1", decision.ID)
	f.fx.Exec(t, "UPDATE issue SET title='Decision made',status='done' WHERE id=$1", decision.ID)
	f.fx.Exec(t, "UPDATE agent_task_queue SET status='completed' WHERE id=$1", in.SourceTaskID)
	in.SourceTaskID = util.MustParseUUID(f.fx.Task(t, util.UUIDToString(in.SourceAgentID), testutil.Cols{"issue_id": in.IssueID, "status": "running", "runtime_id": f.runtimeID, "originator_user_id": f.fx.UserID, "accountable_user_id": f.fx.UserID}))
	replay, err := f.svc.Escalate(context.Background(), in, IssueCreateOpts{})
	if err != nil || !replay.EscalationReplayed || replay.Issue.Title != "Decision made" || replay.Issue.OriginID != f.p.OriginID {
		t.Fatalf("later turn replay: %+v / %v", replay, err)
	}
	if n := f.fx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id=$1", decision.ID); n != 1 {
		t.Fatalf("replay tasks=%d", n)
	}
	if f.events.Load() != 1 {
		t.Fatalf("creation events=%d", f.events.Load())
	}
	var assignee, status string
	f.fx.QueryRow(t, "SELECT assignee_id::text,status FROM issue WHERE id=$1", in.IssueID).Scan(&assignee, &status)
	if assignee != util.UUIDToString(in.SourceAgentID) || status != "todo" {
		t.Fatalf("source changed: %s/%s", assignee, status)
	}
}

func TestHierarchyEscalationConflictAuthorizationAndTombstone(t *testing.T) {
	f, in := escalationFixture(t, true)
	first, err := f.svc.Escalate(context.Background(), in, IssueCreateOpts{})
	if err != nil {
		t.Fatal(err)
	}
	in.Context = "changed"
	if _, err = f.svc.Escalate(context.Background(), in, IssueCreateOpts{}); !errors.Is(err, ErrEscalationConflict) {
		t.Fatalf("intent conflict: %v", err)
	}
	in.Context = "Two incompatible requirements"
	allowed := in.Authorize
	denied := errors.New("revoked authorization")
	in.Authorize = func(context.Context, *db.Queries, projecthierarchy.EscalationRelation) error { return denied }
	if _, err = f.svc.Escalate(context.Background(), in, IssueCreateOpts{}); !errors.Is(err, denied) {
		t.Fatalf("replay authorization: %v", err)
	}
	in.Authorize = allowed
	// A valid tree reversal changes the route to the human boundary. The same
	// request must not silently select a new destination.
	f.fx.Exec(t, "UPDATE project_agent_membership SET manager_membership_id=(SELECT id FROM project_agent_membership WHERE project_id=$1 AND agent_id=$2) WHERE project_id=$1 AND agent_id=$3", in.ProjectID, in.SourceAgentID, f.p.CreatorID)
	f.fx.Exec(t, "UPDATE project_agent_membership SET manager_membership_id=NULL WHERE project_id=$1 AND agent_id=$2", in.ProjectID, in.SourceAgentID)
	if _, err = f.svc.Escalate(context.Background(), in, IssueCreateOpts{}); !errors.Is(err, ErrEscalationConflict) {
		t.Fatalf("changed route: %v", err)
	}
	f.fx.Exec(t, "UPDATE project_agent_membership SET manager_membership_id=(SELECT id FROM project_agent_membership WHERE project_id=$1 AND agent_id=$2) WHERE project_id=$1 AND agent_id=$3", in.ProjectID, f.p.CreatorID, in.SourceAgentID)
	f.fx.Exec(t, "UPDATE project_agent_membership SET manager_membership_id=NULL WHERE project_id=$1 AND agent_id=$2", in.ProjectID, f.p.CreatorID)
	f.fx.Exec(t, "DELETE FROM issue WHERE id=$1", first.Issue.ID)
	if _, err = f.svc.Escalate(context.Background(), in, IssueCreateOpts{}); !errors.Is(err, ErrEscalationDecisionGone) {
		t.Fatalf("deleted decision: %v", err)
	}
	if n := f.fx.Count(t, "SELECT count(*) FROM issue WHERE parent_issue_id=$1", in.IssueID); n != 0 {
		t.Fatalf("recreated tombstone: %d", n)
	}
}

func TestHierarchyEscalationRootInboxAndRollback(t *testing.T) {
	t.Run("human receipt and replay", func(t *testing.T) {
		f, in := escalationFixture(t, false)
		// An Agent project lead must never become a root escalation destination.
		f.fx.Exec(t, "UPDATE project SET lead_type='agent',lead_id=$1 WHERE id=$2", f.p.AssigneeID, in.ProjectID)
		first, err := f.svc.Escalate(context.Background(), in, IssueCreateOpts{})
		if err != nil {
			t.Fatal(err)
		}
		if first.Issue.AssigneeID.Valid || first.Issue.AssigneeType.Valid {
			t.Fatalf("root assigned to an Agent: %+v", first.Issue)
		}
		if n := f.fx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id=$1", first.Issue.ID); n != 0 {
			t.Fatalf("root dispatched %d tasks", n)
		}
		if n := f.fx.Count(t, "SELECT count(*) FROM inbox_item WHERE issue_id=$1 AND recipient_type='member' AND recipient_id=$2 AND severity='action_required'", first.Issue.ID, f.fx.UserID); n != 1 {
			t.Fatalf("human inbox=%d", n)
		}
		replay, err := f.svc.Escalate(context.Background(), in, IssueCreateOpts{})
		if err != nil || !replay.EscalationReplayed {
			t.Fatalf("root replay: %+v / %v", replay, err)
		}
		if n := f.fx.Count(t, "SELECT count(*) FROM inbox_item WHERE issue_id=$1", first.Issue.ID); n != 1 {
			t.Fatalf("duplicate inbox=%d", n)
		}
	})
	for _, mode := range []string{"commit rejection", "no human"} {
		t.Run(mode, func(t *testing.T) {
			f, in := escalationFixture(t, false)
			want := ErrEscalationNoHuman
			if mode == "commit rejection" {
				want = errors.New("commit rejected")
				f.svc.TxStarter = &rejectIssueCommitTxStarter{pool: f.fx.Pool, err: want}
			} else {
				f.fx.Exec(t, "UPDATE member SET role='member' WHERE workspace_id=$1", in.WorkspaceID)
			}
			if _, err := f.svc.Escalate(context.Background(), in, IssueCreateOpts{}); !errors.Is(err, want) {
				t.Fatalf("rollback: %v", err)
			}
			if n := f.fx.Count(t, "SELECT count(*) FROM project_hierarchy_escalation WHERE workspace_id=$1", in.WorkspaceID); n != 0 {
				t.Fatalf("leaked receipt=%d", n)
			}
			if n := f.fx.Count(t, "SELECT count(*) FROM issue WHERE parent_issue_id=$1", in.IssueID); n != 0 {
				t.Fatalf("leaked decision=%d", n)
			}
			if n := f.fx.Count(t, "SELECT count(*) FROM inbox_item WHERE workspace_id=$1", in.WorkspaceID); n != 0 {
				t.Fatalf("leaked inbox=%d", n)
			}
			if f.events.Load() != 0 {
				t.Fatalf("rollback events=%d", f.events.Load())
			}
		})
	}
}

func TestHierarchyEscalationWorkspaceTeardown(t *testing.T) {
	f, in := escalationFixture(t, false)
	if _, err := f.svc.Escalate(context.Background(), in, IssueCreateOpts{}); err != nil {
		t.Fatal(err)
	}
	tx, err := f.fx.Pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	q := db.New(tx)
	if _, err = q.LockWorkspaceForDelete(context.Background(), in.WorkspaceID); err != nil {
		t.Fatal(err)
	}
	if err = q.DeleteWorkspace(context.Background(), in.WorkspaceID); err != nil {
		t.Fatal(err)
	}
	var n int
	if err = tx.QueryRow(context.Background(), "SELECT count(*) FROM project_hierarchy_escalation WHERE workspace_id=$1", in.WorkspaceID).Scan(&n); err != nil || n != 0 {
		t.Fatalf("teardown receipt=%d / %v", n, err)
	}
}
