package projecthierarchy

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestEscalationDerivesOnlyTheDirectManager(t *testing.T) {
	pool := openHierarchyTestPool(t)
	for _, name := range []string{"direct", "no Project", "moved Project", "different assignee", "Squad assignee", "nonmember", "foreign workspace", "archived source", "archived source membership", "archived manager", "archived manager membership", "missing manager", "missing manager Agent", "missing source Agent", "system manager", "foreign manager", "self cycle", "broken ancestor", "invalid identity"} {
		t.Run(name, func(t *testing.T) {
			f := newHierarchyServiceFixture(t, pool)
			rootAgent, managerAgent, workerAgent := f.agent(t, "root"), f.agent(t, "manager"), f.agent(t, "worker")
			root := f.createMembership(t, rootAgent, pgtype.UUID{}, "Root")
			manager := f.createMembership(t, managerAgent, root.ID, "Manager")
			worker := f.createMembership(t, workerAgent, manager.ID, "Worker")
			issue := delegationTestParent(t, f, workerAgent)
			in := EscalationRelationInput{WorkspaceID: f.workspaceID, ProjectID: f.projectID, IssueID: issue, SourceAgentID: workerAgent}
			switch name {
			case "no Project":
				f.fx.Exec(t, `UPDATE issue SET project_id=NULL WHERE id=$1`, issue)
			case "moved Project":
				f.fx.Exec(t, `UPDATE issue SET project_id=$2 WHERE id=$1`, issue, f.fx.Project(t, "Other"))
			case "different assignee":
				f.fx.Exec(t, `UPDATE issue SET assignee_id=$2 WHERE id=$1`, issue, rootAgent)
			case "Squad assignee":
				squad := f.fx.Squad(t, "Squad", f.fx.Agent(t, "Squad lead", ""))
				f.fx.Exec(t, `UPDATE issue SET assignee_type='squad',assignee_id=$2 WHERE id=$1`, issue, squad)
			case "nonmember":
				in.SourceAgentID = f.agent(t, "outsider")
				f.fx.Exec(t, `UPDATE issue SET assignee_id=$2 WHERE id=$1`, issue, in.SourceAgentID)
			case "foreign workspace":
				in.WorkspaceID = newHierarchyServiceFixture(t, pool).workspaceID
			case "archived source":
				f.fx.Exec(t, `UPDATE agent SET archived_at=now() WHERE id=$1`, workerAgent)
			case "archived source membership":
				f.fx.Exec(t, `UPDATE project_agent_membership SET archived_at=now() WHERE id=$1`, worker.ID)
			case "archived manager":
				f.fx.Exec(t, `UPDATE agent SET archived_at=now() WHERE id=$1`, managerAgent)
			case "archived manager membership":
				f.fx.Exec(t, `UPDATE project_agent_membership SET archived_at=now() WHERE id=$1`, manager.ID)
			case "missing manager":
				f.fx.Exec(t, `DELETE FROM project_agent_membership WHERE id=$1`, manager.ID)
			case "missing manager Agent":
				f.fx.Exec(t, `DELETE FROM agent WHERE id=$1`, managerAgent)
			case "missing source Agent":
				f.fx.Exec(t, `DELETE FROM agent WHERE id=$1`, workerAgent)
			case "system manager":
				f.fx.Exec(t, `UPDATE agent SET kind='system' WHERE id=$1`, managerAgent)
			case "foreign manager":
				other := newHierarchyServiceFixture(t, pool)
				foreign := other.agent(t, "Foreign")
				f.fx.Exec(t, `UPDATE project_agent_membership SET agent_id=$2 WHERE id=$1`, manager.ID, foreign)
			case "self cycle":
				f.fx.Exec(t, `UPDATE project_agent_membership SET manager_membership_id=id WHERE id=$1`, worker.ID)
			case "broken ancestor":
				f.fx.Exec(t, `UPDATE project_agent_membership SET manager_membership_id=$2 WHERE id=$1`, manager.ID, testUUID(t))
			case "invalid identity":
				in.SourceAgentID = pgtype.UUID{Valid: true}
			}
			tx, err := pool.Begin(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(context.Background())
			facts, err := CheckEscalationRelation(t.Context(), tx, in)
			if name == "direct" {
				if err != nil || facts.IssueID != issue || facts.ProjectID != f.projectID || facts.SourceMembershipID != worker.ID || facts.ManagerMembershipID != manager.ID || facts.ManagerAgentID != managerAgent || facts.ManagerAgentID == rootAgent || facts.HumanInterventionRequired {
					t.Fatalf("direct manager facts=%+v err=%v", facts, err)
				}
				// Deliberately no runtimes: these facts alone cannot approve execution.
			} else {
				code := CodeEscalationNotAllowed
				if name == "invalid identity" {
					code = CodeInvalidInput
				}
				if !IsCode(err, code) || facts != (EscalationRelation{}) {
					t.Fatalf("rejected path yielded facts=%+v err=%v", facts, err)
				}
			}
		})
	}
}

func TestEscalationRootBoundaryIgnoresAgentProjectLead(t *testing.T) {
	pool := openHierarchyTestPool(t)
	for _, lead := range []string{"none", "root", "other Agent"} {
		t.Run(lead, func(t *testing.T) {
			f := newHierarchyServiceFixture(t, pool)
			source := f.agent(t, "root")
			root := f.createMembership(t, source, pgtype.UUID{}, "Root")
			if lead != "none" {
				id := source
				if lead == "other Agent" {
					id = f.agent(t, "Project lead")
				}
				f.fx.Exec(t, `UPDATE project SET lead_type='agent',lead_id=$2 WHERE id=$1`, f.projectID, id)
			}
			issue := delegationTestParent(t, f, source)
			tx, err := pool.Begin(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(context.Background())
			facts, err := CheckEscalationRelation(t.Context(), tx, EscalationRelationInput{WorkspaceID: f.workspaceID, ProjectID: f.projectID, IssueID: issue, SourceAgentID: source})
			if err != nil || facts.SourceMembershipID != root.ID || !facts.HumanInterventionRequired || facts.ManagerMembershipID.Valid || facts.ManagerAgentID.Valid {
				t.Fatalf("root boundary=%+v err=%v", facts, err)
			}
		})
	}
}

func TestEscalationNextHopRequiresAnotherOwnIssueAndFreshTransaction(t *testing.T) {
	pool := openHierarchyTestPool(t)
	f := newHierarchyServiceFixture(t, pool)
	rootAgent, managerAgent, workerAgent := f.agent(t, "root"), f.agent(t, "manager"), f.agent(t, "worker")
	root := f.createMembership(t, rootAgent, pgtype.UUID{}, "Root")
	manager := f.createMembership(t, managerAgent, root.ID, "Manager")
	f.createMembership(t, workerAgent, manager.ID, "Worker")
	workerIssue := delegationTestParent(t, f, workerAgent)
	managerIssue := delegationTestParent(t, f, managerAgent)
	for _, source := range []pgtype.UUID{workerAgent, managerAgent} {
		issue, want := workerIssue, managerAgent
		if source == managerAgent {
			issue, want = managerIssue, rootAgent
		}
		tx, err := pool.Begin(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		facts, err := CheckEscalationRelation(t.Context(), tx, EscalationRelationInput{WorkspaceID: f.workspaceID, ProjectID: f.projectID, IssueID: issue, SourceAgentID: source})
		_ = tx.Rollback(t.Context())
		if err != nil || facts.ManagerAgentID != want {
			t.Fatalf("hop=%+v err=%v", facts, err)
		}
	}
	// A manager cannot use a worker's Issue as if that Issue were its own task.
	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	_, err = CheckEscalationRelation(t.Context(), tx, EscalationRelationInput{WorkspaceID: f.workspaceID, ProjectID: f.projectID, IssueID: workerIssue, SourceAgentID: managerAgent})
	requireHierarchyCode(t, err, CodeEscalationNotAllowed)
}

func TestEscalationRetainsLocksWithoutWritingOrReassigning(t *testing.T) {
	pool := openHierarchyTestPool(t)
	for _, commit := range []bool{false, true} {
		t.Run(map[bool]string{false: "rollback", true: "commit"}[commit], func(t *testing.T) {
			f := newHierarchyServiceFixture(t, pool)
			managerAgent, source := f.agent(t, "manager"), f.agent(t, "worker")
			manager := f.createMembership(t, managerAgent, pgtype.UUID{}, "Root")
			f.createMembership(t, source, manager.ID, "Worker")
			issue := delegationTestParent(t, f, source)
			before, err := db.New(pool).GetIssue(t.Context(), issue)
			if err != nil {
				t.Fatal(err)
			}
			tx, err := pool.Begin(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(context.Background())
			_, err = CheckEscalationRelation(t.Context(), tx, EscalationRelationInput{WorkspaceID: f.workspaceID, ProjectID: f.projectID, IssueID: issue, SourceAgentID: source})
			if err != nil {
				t.Fatal(err)
			}
			locks := []struct {
				sql string
				id  pgtype.UUID
			}{
				{`SELECT id FROM workspace WHERE id=$1 FOR UPDATE NOWAIT`, f.workspaceID},
				{`SELECT id FROM project WHERE id=$1 FOR UPDATE NOWAIT`, f.projectID},
				{`SELECT id FROM issue WHERE id=$1 FOR NO KEY UPDATE NOWAIT`, issue},
				{`SELECT id FROM agent WHERE id=$1 FOR NO KEY UPDATE NOWAIT`, source},
				{`SELECT id FROM agent WHERE id=$1 FOR NO KEY UPDATE NOWAIT`, managerAgent},
			}
			for _, lock := range locks {
				other, err := pool.Begin(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				_, err = other.Exec(t.Context(), lock.sql, lock.id)
				_ = other.Rollback(t.Context())
				var pgErr *pgconn.PgError
				if !errors.As(err, &pgErr) || pgErr.Code != "55P03" {
					t.Fatalf("missing retained lock: %v", err)
				}
			}
			if commit {
				err = tx.Commit(t.Context())
			} else {
				err = tx.Rollback(t.Context())
			}
			if err != nil {
				t.Fatal(err)
			}
			after, err := db.New(pool).GetIssue(t.Context(), issue)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatal("relation check mutated Issue")
			}
			for _, table := range []string{"agent_task_queue", "comment", "inbox_item", "issue_wakeup", "activity_log"} {
				if n := f.fx.Count(t, `SELECT count(*) FROM `+table+` WHERE workspace_id=$1`, f.workspaceID); n != 0 {
					t.Fatalf("relation check wrote %s", table)
				}
			}
			if n := f.fx.Count(t, `SELECT count(*) FROM issue WHERE workspace_id=$1`, f.workspaceID); n != 1 {
				t.Fatal("relation check created Issue")
			}
			for _, lock := range locks {
				other, err := pool.Begin(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				_, err = other.Exec(t.Context(), lock.sql, lock.id)
				_ = other.Rollback(t.Context())
				if err != nil {
					t.Fatalf("lock retained after completion: %v", err)
				}
			}
		})
	}
}

func TestEscalationRejectsConcurrentArchiveAndRevalidatesLaterTurns(t *testing.T) {
	pool := openHierarchyTestPool(t)
	f := newHierarchyServiceFixture(t, pool)
	root := f.createMembership(t, f.agent(t, "root"), pgtype.UUID{}, "Root")
	managerAgent, source := f.agent(t, "manager"), f.agent(t, "worker")
	manager := f.createMembership(t, managerAgent, root.ID, "Manager")
	worker := f.createMembership(t, source, manager.ID, "Worker")
	issue := delegationTestParent(t, f, source)
	in := EscalationRelationInput{WorkspaceID: f.workspaceID, ProjectID: f.projectID, IssueID: issue, SourceAgentID: source}
	owner, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Rollback(context.Background())
	if _, err := db.New(owner).GetAgentForUpdate(t.Context(), managerAgent); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = CheckEscalationRelation(ctx, tx, in)
	_ = tx.Rollback(context.Background())
	requireHierarchyCode(t, err, CodeEscalationNotAllowed)
	if err := owner.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.ReparentMembership(ctx, ReparentMembershipInput{WorkspaceID: f.workspaceID, ProjectID: f.projectID, MembershipID: worker.ID, ManagerMembershipID: root.ID}); err != nil {
		t.Fatal(err)
	}
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	facts, err := CheckEscalationRelation(ctx, tx, in)
	if err != nil || facts.ManagerMembershipID != root.ID || facts.ManagerAgentID == managerAgent {
		t.Fatalf("stale manager reused: %+v %v", facts, err)
	}
}

func TestEscalationRequiresTransactionAndNonzeroIdentities(t *testing.T) {
	_, err := CheckEscalationRelation(context.Background(), nil, EscalationRelationInput{})
	requireHierarchyCode(t, err, CodeInvalidInput)
}

// Interleave immediately before the first shared Agent lock, after hierarchy
// discovery. This exposes the archive/restore race without timer assumptions.
type escalationInterleavingTx struct {
	pgx.Tx
	beforeAgentLock func()
}

func (tx *escalationInterleavingTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if tx.beforeAgentLock != nil && strings.Contains(sql, "FROM agent WHERE") && strings.Contains(sql, "FOR SHARE") {
		run := tx.beforeAgentLock
		tx.beforeAgentLock = nil
		run()
	}
	return tx.Tx.QueryRow(ctx, sql, args...)
}

func TestEscalationRechecksMembershipAfterConcurrentArchiveAndRestore(t *testing.T) {
	pool := openHierarchyTestPool(t)
	f := newHierarchyServiceFixture(t, pool)
	manager := f.createMembership(t, f.agent(t, "manager"), pgtype.UUID{}, "Root")
	source := f.agent(t, "worker")
	f.createMembership(t, source, manager.ID, "Worker")
	issue := delegationTestParent(t, f, source)
	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	interleaved := &escalationInterleavingTx{Tx: tx, beforeAgentLock: func() {
		archive, err := pool.Begin(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		defer archive.Rollback(context.Background())
		if err := archiveInTx(t, f, archive, source); err != nil {
			t.Fatal(err)
		}
		if err := archive.Commit(t.Context()); err != nil {
			t.Fatal(err)
		}
		if _, err := db.New(pool).RestoreAgent(t.Context(), source); err != nil {
			t.Fatal(err)
		}
	}}
	facts, err := CheckEscalationRelation(t.Context(), interleaved, EscalationRelationInput{WorkspaceID: f.workspaceID, ProjectID: f.projectID, IssueID: issue, SourceAgentID: source})
	if !IsCode(err, CodeEscalationNotAllowed) || facts != (EscalationRelation{}) {
		t.Fatalf("archive/restore revived stale membership: %+v %v", facts, err)
	}
}
