package projecthierarchy

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func archiveInTx(t *testing.T, f *hierarchyServiceFixture, tx pgx.Tx, id pgtype.UUID) error {
	t.Helper()
	a, err := db.New(tx).ArchiveAgent(t.Context(), db.ArchiveAgentParams{ID: id, ArchivedBy: pgUUIDFromString(t, f.fx.UserID)})
	if err != nil {
		return err
	}
	return GuardArchivedAgents(t.Context(), tx, f.workspaceID, []db.Agent{a})
}

func TestHierarchyArchiveFencesNewProjectMembershipAndReactivation(t *testing.T) {
	pool := openHierarchyTestPool(t)
	for _, reactivate := range []bool{false, true} {
		t.Run(map[bool]string{false: "new Project", true: "reactivation"}[reactivate], func(t *testing.T) {
			f := newHierarchyServiceFixture(t, pool)
			root := f.createMembership(t, f.agent(t, "root"), pgtype.UUID{}, "Root")
			victim := f.agent(t, "victim")
			if reactivate {
				m := f.createMembership(t, victim, root.ID, "Leaf")
				if _, err := f.service.ArchiveMembership(t.Context(), ArchiveMembershipInput{WorkspaceID: f.workspaceID, ProjectID: f.projectID, MembershipID: m.ID}); err != nil {
					t.Fatal(err)
				}
			}
			owner, err := pool.Begin(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer owner.Rollback(context.Background())
			if err := archiveInTx(t, f, owner, victim); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			_, err = f.service.CreateMembership(ctx, CreateMembershipInput{WorkspaceID: f.workspaceID, ProjectID: f.projectID, AgentID: victim, ManagerMembershipID: root.ID})
			requireHierarchyCode(t, err, CodeHierarchyBusy)
			if err := owner.Commit(t.Context()); err != nil {
				t.Fatal(err)
			}
			_, err = f.service.CreateMembership(ctx, CreateMembershipInput{WorkspaceID: f.workspaceID, ProjectID: f.projectID, AgentID: victim, ManagerMembershipID: root.ID})
			requireHierarchyCode(t, err, CodeAgentArchived)
			if n := f.fx.Count(t, `SELECT count(*) FROM project_agent_membership WHERE agent_id=$1 AND archived_at IS NULL`, victim); n != 0 {
				t.Fatal("archive left active membership")
			}
		})
	}
}

func TestHierarchyMutationRetainsArchiveFenceUntilCallerFinishes(t *testing.T) {
	pool := openHierarchyTestPool(t)
	f := newHierarchyServiceFixture(t, pool)
	root := f.createMembership(t, f.agent(t, "root"), pgtype.UUID{}, "Root")
	victim := f.agent(t, "leaf")
	f.createMembership(t, victim, root.ID, "Leaf")
	tx, _, _, err := f.service.beginProjectMutation(t.Context(), f.workspaceID, f.projectID)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	// The real native archive row UPDATE must conflict while structural locks
	// are held. NOWAIT is only the test probe; the native archive normally waits.
	other, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	_, err = other.Exec(t.Context(), `SELECT id FROM agent WHERE id=$1 FOR NO KEY UPDATE NOWAIT`, victim)
	_ = other.Rollback(t.Context())
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "55P03" {
		t.Fatalf("structural mutation did not fence archive: %v", err)
	}
	if err := tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	other, err = pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer other.Rollback(context.Background())
	if err := archiveInTx(t, f, other, victim); err != nil {
		t.Fatal(err)
	}
	if err := other.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	if n := f.fx.Count(t, `SELECT count(*) FROM project_agent_membership WHERE agent_id=$1 AND archived_at IS NULL`, victim); n != 0 {
		t.Fatal("leaf remained active")
	}
}

func TestHierarchyArchiveRejectsReparentRootChangeRemovalAndDelegationWhileChanging(t *testing.T) {
	pool := openHierarchyTestPool(t)
	f := newHierarchyServiceFixture(t, pool)
	rootAgent := f.agent(t, "root")
	root := f.createMembership(t, rootAgent, pgtype.UUID{}, "Root")
	victim := f.agent(t, "victim")
	leaf := f.createMembership(t, victim, root.ID, "Leaf")
	peer := f.createMembership(t, f.agent(t, "peer"), root.ID, "Peer")
	parent := delegationTestParent(t, f, rootAgent)
	owner, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Rollback(context.Background())
	if err := archiveInTx(t, f, owner, victim); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	_, err = f.service.ReparentMembership(ctx, ReparentMembershipInput{WorkspaceID: f.workspaceID, ProjectID: f.projectID, MembershipID: leaf.ID, ManagerMembershipID: peer.ID})
	requireHierarchyCode(t, err, CodeHierarchyBusy)
	_, err = f.service.ArchiveMembership(ctx, ArchiveMembershipInput{WorkspaceID: f.workspaceID, ProjectID: f.projectID, MembershipID: leaf.ID})
	requireHierarchyCode(t, err, CodeHierarchyBusy)
	_, err = f.service.ApplyStructure(ctx, ApplyStructureInput{WorkspaceID: f.workspaceID, ProjectID: f.projectID, Changes: []ManagerChange{{MembershipID: root.ID, ManagerMembershipID: leaf.ID}, {MembershipID: leaf.ID}}})
	requireHierarchyCode(t, err, CodeHierarchyBusy)
	delegate, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = CheckDelegationRelation(ctx, delegate, DelegationRelationInput{WorkspaceID: f.workspaceID, ProjectID: f.projectID, ParentID: parent, DelegatorID: rootAgent, TargetID: victim})
	_ = delegate.Rollback(context.Background())
	requireHierarchyCode(t, err, CodeDelegationNotAllowed)
	if err := owner.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	// A failed archive transaction restores the original leaf and authority.
	if _, err := f.service.ReparentMembership(ctx, ReparentMembershipInput{WorkspaceID: f.workspaceID, ProjectID: f.projectID, MembershipID: leaf.ID, ManagerMembershipID: peer.ID}); err != nil {
		t.Fatal(err)
	}
}

func TestHierarchyArchiveWaitsForConcurrentNewMembershipThenDeactivatesIt(t *testing.T) {
	pool := openHierarchyTestPool(t)
	f := newHierarchyServiceFixture(t, pool)
	root := f.createMembership(t, f.agent(t, "root"), pgtype.UUID{}, "Root")
	victim := f.agent(t, "new report")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	owner, q, rows, err := f.service.beginProjectMutation(ctx, f.workspaceID, f.projectID)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Rollback(context.Background())
	candidate := db.ProjectAgentMembership{AgentID: victim}
	if err := validateActiveAgents(ctx, owner, q, f.workspaceID, append(rows, candidate)); err != nil {
		t.Fatal(err)
	}
	// Persist the same native membership write as CreateMembership but retain
	// its transaction to place the archive precisely before membership commit.
	_, err = q.CreateProjectHierarchyMembership(ctx, db.CreateProjectHierarchyMembershipParams{
		ID: pgUUIDFromString(t, uuid.NewString()), WorkspaceID: f.workspaceID, ProjectID: f.projectID, AgentID: victim, ManagerMembershipID: root.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	waitingPID := int32(conn.Conn().PgConn().PID())
	ownerPID := int32(owner.Conn().PgConn().PID())
	actor := pgUUIDFromString(t, f.fx.UserID)
	done := make(chan error, 1)
	finished := make(chan struct{})
	defer func() {
		cancel()
		_ = owner.Rollback(context.Background())
		<-finished
		conn.Release()
	}()
	go func() {
		defer close(finished)
		tx, err := conn.Begin(ctx)
		if err != nil {
			done <- err
			return
		}
		defer tx.Rollback(context.Background())
		a, err := db.New(tx).ArchiveAgent(ctx, db.ArchiveAgentParams{ID: victim, ArchivedBy: actor})
		if err == nil {
			err = GuardArchivedAgents(ctx, tx, f.workspaceID, []db.Agent{a})
		}
		if err == nil {
			err = tx.Commit(ctx)
		}
		done <- err
	}()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var blocked bool
		if err := pool.QueryRow(ctx, `SELECT $1::int=ANY(pg_blocking_pids($2::int))`, ownerPID, waitingPID).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("archive bypassed uncommitted membership: %v", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-ticker.C:
		}
	}
	if err := owner.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if n := f.fx.Count(t, `SELECT count(*) FROM project_agent_membership WHERE agent_id=$1 AND archived_at IS NOT NULL`, victim); n != 1 {
		t.Fatal("archive missed concurrent membership")
	}
}
