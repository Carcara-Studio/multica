package service

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/projecthierarchy"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type hierarchyCreateFixture struct {
	fx     *testutil.Fixture
	svc    *IssueService
	p      IssueCreateParams
	opts   IssueCreateOpts
	events atomic.Int32
}

func newHierarchyCreateFixture(t *testing.T) *hierarchyCreateFixture {
	t.Helper()
	pool := sharedTestPool(t)
	fx := testutil.New(pool, "", "")
	fx.UserID = fx.User(t, "hierarchy create", uuid.NewString()+"@multica.test")
	fx.WorkspaceID = fx.Workspace(t, "hierarchy create", "hierarchy-create-"+uuid.NewString())
	fx.Member(t, fx.WorkspaceID, fx.UserID, "owner")
	project := util.MustParseUUID(fx.Project(t, "delegation"))
	source := util.MustParseUUID(fx.Agent(t, "source", ""))
	runtime := fx.Runtime(t, "fixture-only runtime")
	target := util.MustParseUUID(fx.Agent(t, "target", runtime))
	workspace := util.MustParseUUID(fx.WorkspaceID)
	q := db.New(pool)
	h := projecthierarchy.NewService(q, pool)
	root, err := h.CreateMembership(context.Background(), projecthierarchy.CreateMembershipInput{
		WorkspaceID: workspace, ProjectID: project, AgentID: source,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.CreateMembership(context.Background(), projecthierarchy.CreateMembershipInput{
		WorkspaceID: workspace, ProjectID: project, AgentID: target, ManagerMembershipID: root.ID,
	}); err != nil {
		t.Fatal(err)
	}
	fx.Cleanup(t, "DELETE FROM project_agent_membership WHERE project_id = $1", project)
	parent := util.MustParseUUID(fx.Issue(t, "parent", testutil.Cols{
		"project_id": project, "assignee_type": "agent", "assignee_id": source,
	}))
	fx.Exec(t, "UPDATE workspace SET issue_counter = 1 WHERE id = $1", workspace)
	task := util.MustParseUUID(fx.Task(t, util.UUIDToString(source), testutil.Cols{
		"issue_id": parent, "status": "running", "originator_user_id": fx.UserID, "accountable_user_id": fx.UserID,
	}))
	f := &hierarchyCreateFixture{fx: fx}
	bus := events.New()
	bus.Subscribe(protocol.EventIssueCreated, func(events.Event) { f.events.Add(1) })
	f.svc = NewIssueService(q, pool, bus, nil, &TaskService{Queries: q, TxStarter: pool, Bus: bus})
	f.p = IssueCreateParams{
		WorkspaceID: workspace, ProjectID: project, ParentIssueID: parent,
		Title: "delegated work", Status: "todo", Priority: "none",
		CreatorType: "agent", CreatorID: source,
		AssigneeType: pgtype.Text{String: "agent", Valid: true}, AssigneeID: target,
		OriginType: pgtype.Text{String: "agent_create", Valid: true}, OriginID: task,
	}
	f.opts.HierarchyDelegation = &HierarchyDelegationCreate{
		RequestID: util.MustParseUUID(uuid.NewString()),
		// Test-only actor/invocation admission with the native readiness check.
		// Scheduling inserts a queue row only; no daemon or CLI is executed.
		Authorize: func(ctx context.Context, qtx *db.Queries, r projecthierarchy.DelegationRelation) error {
			if r.ParentID != parent || r.ProjectID != project {
				return errors.New("unexpected fixture scope")
			}
			agent, err := qtx.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: target, WorkspaceID: workspace})
			if err != nil {
				return err
			}
			verdict, err := AgentReadiness(ctx, RuntimeLookup{Queries: qtx}, agent)
			if err != nil {
				return err
			}
			if verdict.Blocked() {
				return errors.New("fixture target runtime blocked")
			}
			return nil
		},
	}
	fx.Cleanup(t, "DELETE FROM issue WHERE parent_issue_id = $1", parent)
	fx.Cleanup(t, "DELETE FROM project_hierarchy_delegation WHERE workspace_id = $1", workspace)
	return f
}

func (f *hierarchyCreateFixture) create() (IssueCreateResult, error) {
	return f.svc.Create(context.Background(), f.p, f.opts)
}

func TestHierarchyCreateReplayConcurrentAndFollowup(t *testing.T) {
	f := newHierarchyCreateFixture(t)
	type result struct {
		value IssueCreateResult
		err   error
	}
	results := make(chan result, 2)
	start := make(chan struct{})
	for range 2 {
		go func() {
			<-start
			value, err := f.create()
			results <- result{value, err}
		}()
	}
	close(start)
	a, b := <-results, <-results
	if a.err != nil || b.err != nil {
		t.Fatalf("concurrent creates: %v / %v", a.err, b.err)
	}
	if a.value.Issue.ID != b.value.Issue.ID || a.value.DelegationReplayed == b.value.DelegationReplayed {
		t.Fatalf("retry did not settle to one child: %+v / %+v", a.value, b.value)
	}
	child := a.value.Issue
	if child.ParentIssueID != f.p.ParentIssueID || child.ProjectID != f.p.ProjectID ||
		child.AssigneeID != f.p.AssigneeID || child.OriginID != f.p.OriginID {
		t.Fatalf("native lineage/assignment mismatch: %+v", child)
	}
	if f.events.Load() != 1 {
		t.Fatalf("creation events = %d", f.events.Load())
	}
	if n := f.fx.Count(t, "SELECT count(*) FROM project_hierarchy_delegation WHERE workspace_id = $1", f.p.WorkspaceID); n != 1 {
		t.Fatalf("ledger rows = %d", n)
	}
	var taskID pgtype.UUID
	f.fx.QueryRow(t, "SELECT id FROM agent_task_queue WHERE issue_id = $1", child.ID).Scan(&taskID)
	f.fx.Exec(t, "UPDATE agent_task_queue SET status = 'completed' WHERE id = $1", taskID)
	f.fx.Exec(t, "UPDATE issue SET title = 'edited', status = 'done' WHERE id = $1", child.ID)
	// Origin lineage from a different turn must not change the operation identity.
	f.fx.Exec(t, "UPDATE agent_task_queue SET status = 'completed' WHERE id = $1", f.p.OriginID)
	f.p.OriginID = util.MustParseUUID(f.fx.Task(t, util.UUIDToString(f.p.CreatorID), testutil.Cols{
		"issue_id": f.p.ParentIssueID, "status": "running",
		"originator_user_id": f.fx.UserID, "accountable_user_id": f.fx.UserID,
	}))
	replay, err := f.create()
	if err != nil || !replay.DelegationReplayed || replay.Issue.ID != child.ID || replay.Issue.Title != "edited" {
		t.Fatalf("completed/edited replay: %+v, %v", replay, err)
	}
	if n := f.fx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id = $1", child.ID); n != 1 {
		t.Fatalf("replay dispatched new work after completion: %d tasks", n)
	}
	f.opts.HierarchyDelegation.RequestID = util.MustParseUUID(uuid.NewString())
	followup, err := f.create()
	if err != nil || followup.DelegationReplayed || followup.Issue.ID == child.ID {
		t.Fatalf("explicit follow-up: %+v, %v", followup, err)
	}
	if f.events.Load() != 2 {
		t.Fatalf("follow-up events = %d", f.events.Load())
	}
	var counter int32
	f.fx.QueryRow(t, "SELECT issue_counter FROM workspace WHERE id = $1", f.p.WorkspaceID).Scan(&counter)
	if counter != 3 {
		t.Fatalf("counter = %d, want parent + two logical children", counter)
	}
	var parentStatus string
	f.fx.QueryRow(t, "SELECT status FROM issue WHERE id = $1", f.p.ParentIssueID).Scan(&parentStatus)
	if parentStatus != "todo" {
		t.Fatalf("parent status changed to %q", parentStatus)
	}
}

func TestHierarchyCreateConflictDeletedChildAndFreshAuthorization(t *testing.T) {
	f := newHierarchyCreateFixture(t)
	first, err := f.create()
	if err != nil {
		t.Fatal(err)
	}
	f.p.Title = "different work"
	if _, err := f.create(); !errors.Is(err, ErrDelegationConflict) {
		t.Fatalf("changed intent = %v", err)
	}
	f.p.Title = "delegated work"
	denied := errors.New("fixture invocation denied")
	authorize := f.opts.HierarchyDelegation.Authorize
	f.opts.HierarchyDelegation.Authorize = func(context.Context, *db.Queries, projecthierarchy.DelegationRelation) error { return denied }
	if _, err := f.create(); !errors.Is(err, denied) {
		t.Fatalf("replay bypassed fresh authorization: %v", err)
	}
	f.opts.HierarchyDelegation.Authorize = authorize
	f.fx.Exec(t, "DELETE FROM issue WHERE id = $1", first.Issue.ID)
	if _, err := f.create(); !errors.Is(err, ErrDelegationChildGone) {
		t.Fatalf("deleted child = %v", err)
	}
	if f.events.Load() != 1 {
		t.Fatalf("replay emitted creation event: %d", f.events.Load())
	}
	if n := f.fx.Count(t, "SELECT count(*) FROM issue WHERE parent_issue_id = $1", f.p.ParentIssueID); n != 0 {
		t.Fatalf("deleted child recreated: %d", n)
	}
}

func TestHierarchyCreateRollbackAndDeniedRelation(t *testing.T) {
	f := newHierarchyCreateFixture(t)
	rejected := errors.New("fixture commit rejection")
	f.svc.TxStarter = &rejectIssueCommitTxStarter{pool: f.fx.Pool, err: rejected}
	if _, err := f.create(); !errors.Is(err, rejected) {
		t.Fatalf("commit rejection = %v", err)
	}
	if n := f.fx.Count(t, "SELECT count(*) FROM project_hierarchy_delegation WHERE workspace_id = $1", f.p.WorkspaceID); n != 0 {
		t.Fatalf("rollback left %d ledger rows", n)
	}
	if n := f.fx.Count(t, "SELECT count(*) FROM issue WHERE parent_issue_id = $1", f.p.ParentIssueID); n != 0 || f.events.Load() != 0 {
		t.Fatalf("rollback leaked child/event: %d / %d", n, f.events.Load())
	}
	var counter int32
	f.fx.QueryRow(t, "SELECT issue_counter FROM workspace WHERE id = $1", f.p.WorkspaceID).Scan(&counter)
	if counter != 1 {
		t.Fatalf("rollback counter = %d", counter)
	}
	f.svc.TxStarter = f.fx.Pool
	f.fx.Exec(t, "UPDATE issue SET assignee_id = $1 WHERE id = $2", f.p.AssigneeID, f.p.ParentIssueID)
	if _, err := f.create(); err == nil {
		t.Fatal("reassigned parent admitted delegation")
	}
	f.fx.Exec(t, "UPDATE issue SET assignee_id = $1 WHERE id = $2", f.p.CreatorID, f.p.ParentIssueID)
	f.opts.HierarchyDelegation.Authorize = nil
	if _, err := f.create(); !errors.Is(err, ErrDelegationInvalid) {
		t.Fatalf("missing authorization = %v", err)
	}
}

func TestHierarchyCreateWorkspaceTeardownSweepsIdentity(t *testing.T) {
	f := newHierarchyCreateFixture(t)
	if _, err := f.create(); err != nil {
		t.Fatal(err)
	}
	tx, err := f.fx.Pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	q := db.New(tx)
	if _, err := q.LockWorkspaceForDelete(context.Background(), f.p.WorkspaceID); err != nil {
		t.Fatal(err)
	}
	if err := q.DeleteWorkspace(context.Background(), f.p.WorkspaceID); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := tx.QueryRow(context.Background(), "SELECT count(*) FROM project_hierarchy_delegation WHERE workspace_id = $1", f.p.WorkspaceID).Scan(&n); err != nil || n != 0 {
		t.Fatalf("workspace teardown ledger = %d, %v", n, err)
	}
}

func TestHierarchyCreateRejectsInvalidTaskAndUnsupportedWork(t *testing.T) {
	for _, mutate := range []struct {
		name string
		change func(*IssueCreateParams)
	}{
		{"missing task", func(p *IssueCreateParams) { p.OriginID = util.MustParseUUID(uuid.NewString()) }},
		{"source capture", func(p *IssueCreateParams) { p.SourceContext = &SourceContextCapture{} }},
		{"labels", func(p *IssueCreateParams) { p.LabelIDs = []pgtype.UUID{util.MustParseUUID(uuid.NewString())} }},
		{"stage", func(p *IssueCreateParams) { p.Stage = pgtype.Int4{Int32: 1, Valid: true} }},
		{"custom status", func(p *IssueCreateParams) { p.Status = "review" }},
	} {
		t.Run(mutate.name, func(t *testing.T) {
			f := newHierarchyCreateFixture(t)
			mutate.change(&f.p)
			if _, err := f.create(); !errors.Is(err, ErrDelegationInvalid) {
				t.Fatalf("invalid request = %v", err)
			}
			if n := f.fx.Count(t, "SELECT count(*) FROM project_hierarchy_delegation WHERE workspace_id = $1", f.p.WorkspaceID); n != 0 {
				t.Fatalf("invalid request left %d identities", n)
			}
		})
	}
}
