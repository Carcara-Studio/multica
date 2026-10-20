package projecthierarchy

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func delegationTestParent(t *testing.T, f *hierarchyServiceFixture, source pgtype.UUID) pgtype.UUID {
	t.Helper()
	id := f.fx.Issue(t, "Delegation parent", testutil.Cols{
		"project_id": f.projectID, "assignee_type": "agent", "assignee_id": source,
	})
	return pgUUIDFromString(t, id)
}

func TestDelegationRelationRejectsNonDirectOrUnavailableTargets(t *testing.T) {
	pool := openHierarchyTestPool(t)
	for _, name := range []string{"direct", "deeper descendant", "peer", "upward", "self", "nonmember", "other project", "foreign workspace", "no project", "moved project", "different assignee", "squad assignee", "archived source agent", "archived target agent", "archived target membership", "missing parent", "invalid identity"} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			f := newHierarchyServiceFixture(t, pool)
			rootAgent, targetAgent := f.agent(t, "Source"), f.agent(t, "Target")
			root := f.createMembership(t, rootAgent, pgtype.UUID{}, "Lead")
			target := f.createMembership(t, targetAgent, root.ID, "Report")
			parent := delegationTestParent(t, f, rootAgent)
			in := DelegationRelationInput{
				WorkspaceID: f.workspaceID, ProjectID: f.projectID, ParentID: parent,
				DelegatorID: rootAgent, TargetID: targetAgent,
			}
			exec := func(sql string, args ...any) {
				t.Helper()
				if _, err := pool.Exec(ctx, sql, args...); err != nil {
					t.Fatal(err)
				}
			}
			switch name {
			case "deeper descendant":
				deep := f.agent(t, "Descendant")
				f.createMembership(t, deep, target.ID, "Nested")
				in.TargetID = deep
			case "peer":
				peer := f.agent(t, "Peer")
				f.createMembership(t, peer, root.ID, "Peer role")
				in.DelegatorID, in.TargetID = targetAgent, peer
				exec("UPDATE issue SET assignee_id = $2 WHERE id = $1", parent, targetAgent)
			case "upward":
				in.DelegatorID, in.TargetID = targetAgent, rootAgent
				exec("UPDATE issue SET assignee_id = $2 WHERE id = $1", parent, targetAgent)
			case "self":
				in.TargetID = rootAgent
			case "nonmember":
				in.TargetID = f.agent(t, "Nonmember")
			case "other project":
				other := pgUUIDFromString(t, f.fx.Project(t, "Other Project"))
				exec("UPDATE project_agent_membership SET project_id = $2 WHERE id = $1", target.ID, other)
			case "foreign workspace":
				foreign := newHierarchyServiceFixture(t, pool)
				in.TargetID = foreign.agent(t, "Foreign")
			case "no project":
				exec("UPDATE issue SET project_id = NULL WHERE id = $1", parent)
			case "moved project":
				other := f.fx.Project(t, "Moved Project")
				exec("UPDATE issue SET project_id = $2 WHERE id = $1", parent, other)
			case "different assignee":
				exec("UPDATE issue SET assignee_id = $2 WHERE id = $1", parent, targetAgent)
			case "squad assignee":
				squad := f.fx.Squad(t, "Squad", f.fx.Agent(t, "Squad leader", ""))
				exec("UPDATE issue SET assignee_type = 'squad', assignee_id = $2 WHERE id = $1", parent, squad)
			case "archived source agent":
				exec("UPDATE agent SET archived_at = now() WHERE id = $1", rootAgent)
			case "archived target agent":
				exec("UPDATE agent SET archived_at = now() WHERE id = $1", targetAgent)
			case "archived target membership":
				exec("UPDATE project_agent_membership SET archived_at = now() WHERE id = $1", target.ID)
			case "missing parent":
				in.ParentID = testUUID(t)
			case "invalid identity":
				in.TargetID = pgtype.UUID{Valid: true}
			}
			// The cross-Project case deliberately moves a membership out of
			// the fixture's original Project cleanup scope.
			t.Cleanup(func() {
				if _, err := pool.Exec(context.Background(), "DELETE FROM project_agent_membership WHERE id = $1", target.ID); err != nil {
					t.Error(err)
				}
			})
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			facts, err := CheckDelegationRelation(ctx, tx, in)
			if name == "direct" {
				if err != nil || facts.ParentID != parent || facts.ProjectID != f.projectID ||
					facts.DelegatorMembershipID != root.ID || facts.TargetMembershipID != target.ID {
					t.Fatalf("direct relation failed: %+v %v", facts, err)
				}
				// Agents deliberately have no runtime: structural facts alone do
				// not certify invocation permission or readiness.
			} else {
				code := CodeDelegationNotAllowed
				if name == "invalid identity" {
					code = CodeInvalidInput
				}
				if !IsCode(err, code) || facts.ParentID.Valid {
					t.Fatalf("unavailable relation yielded facts: %+v %v", facts, err)
				}
			}
		})
	}
}

func TestDelegationRelationRetainsCallerTransactionLocks(t *testing.T) {
	pool := openHierarchyTestPool(t)
	for _, commit := range []bool{false, true} {
		t.Run(map[bool]string{false: "rollback", true: "commit"}[commit], func(t *testing.T) {
			ctx := context.Background()
			f := newHierarchyServiceFixture(t, pool)
			source, targetAgent := f.agent(t, "Manager"), f.agent(t, "Report")
			root := f.createMembership(t, source, pgtype.UUID{}, "Lead")
			target := f.createMembership(t, targetAgent, root.ID, "Report")
			parent := delegationTestParent(t, f, source)
			in := DelegationRelationInput{
				WorkspaceID: f.workspaceID, ProjectID: f.projectID, ParentID: parent,
				DelegatorID: source, TargetID: targetAgent,
			}
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			if _, err := CheckDelegationRelation(ctx, tx, in); err != nil {
				t.Fatal(err)
			}
			locks := []struct {
				sql string
				id  pgtype.UUID
			}{
				{"SELECT id FROM workspace WHERE id = $1 FOR UPDATE NOWAIT", f.workspaceID},
				{"SELECT id FROM project WHERE id = $1 FOR UPDATE NOWAIT", f.projectID},
				{"SELECT id FROM issue WHERE id = $1 FOR UPDATE NOWAIT", parent},
				{"SELECT id FROM agent WHERE id = $1 FOR UPDATE NOWAIT", source},
				{"SELECT id FROM agent WHERE id = $1 FOR UPDATE NOWAIT", targetAgent},
			}
			for _, lock := range locks {
				other, err := pool.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				var id pgtype.UUID
				err = other.QueryRow(ctx, lock.sql, lock.id).Scan(&id)
				_ = other.Rollback(ctx)
				var pgErr *pgconn.PgError
				if !errors.As(err, &pgErr) || pgErr.Code != "55P03" {
					t.Fatalf("accepted relation did not retain lock: %v", err)
				}
			}
			if commit {
				err = tx.Commit(ctx)
			} else {
				err = tx.Rollback(ctx)
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, lock := range locks {
				other, err := pool.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				var id pgtype.UUID
				err = other.QueryRow(ctx, lock.sql, lock.id).Scan(&id)
				_ = other.Rollback(ctx)
				if err != nil {
					t.Fatalf("caller completion did not release lock: %v", err)
				}
			}
			// A real structural writer can proceed after release. The facts
			// from the old transaction must not authorize a new transaction.
			peer := f.createMembership(t, f.agent(t, "New manager"), root.ID, "Peer")
			if _, err := f.service.ReparentMembership(ctx, ReparentMembershipInput{
				WorkspaceID: f.workspaceID, ProjectID: f.projectID,
				MembershipID: target.ID, ManagerMembershipID: peer.ID,
			}); err != nil {
				t.Fatal(err)
			}
			next, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer next.Rollback(ctx)
			if _, err := CheckDelegationRelation(ctx, next, in); !IsCode(err, CodeDelegationNotAllowed) {
				t.Fatal("changed edge reused old relation")
			}
			var issues, tasks int
			if err := pool.QueryRow(ctx, "SELECT count(*) FROM issue WHERE workspace_id = $1", f.workspaceID).Scan(&issues); err != nil {
				t.Fatal(err)
			}
			if err := pool.QueryRow(ctx, "SELECT count(*) FROM agent_task_queue WHERE agent_id = ANY($1::uuid[])", []pgtype.UUID{source, targetAgent}).Scan(&tasks); err != nil {
				t.Fatal(err)
			}
			if issues != 1 || tasks != 0 {
				t.Fatal("relation admission created work")
			}
		})
	}
}

func TestDelegationRelationRequiresTransaction(t *testing.T) {
	_, err := CheckDelegationRelation(context.Background(), nil, DelegationRelationInput{})
	if !IsCode(err, CodeInvalidInput) {
		t.Fatal("nil transaction was accepted")
	}
}

