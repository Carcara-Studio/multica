package projecthierarchy

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type hierarchyServiceFixture struct {
	pool        *pgxpool.Pool
	fx          *testutil.Fixture
	service     *Service
	workspaceID pgtype.UUID
	projectID   pgtype.UUID
}

func openHierarchyTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL required; run through the managed test environment")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("test database is not reachable: %v", err)
	}
	t.Cleanup(pool.Close)

	var exists bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('project_agent_membership') IS NOT NULL`).Scan(&exists); err != nil {
		t.Fatalf("inspect hierarchy schema: %v", err)
	}
	if !exists {
		t.Fatalf("project_agent_membership is missing; apply repository migrations before running hierarchy DB tests")
	}
	return pool
}

func pgUUIDFromString(t *testing.T, value string) pgtype.UUID {
	t.Helper()
	parsed, err := uuid.Parse(value)
	if err != nil {
		t.Fatalf("parse fixture UUID %q: %v", value, err)
	}
	return pgtype.UUID{Bytes: [16]byte(parsed), Valid: true}
}

func newHierarchyServiceFixture(t *testing.T, pool *pgxpool.Pool) *hierarchyServiceFixture {
	t.Helper()
	fx := testutil.New(pool, "", "")
	userID := fx.User(t, "Hierarchy Test", "hierarchy-"+uuid.NewString()+"@example.test")
	workspaceID := fx.Workspace(t, "Hierarchy Test", "hierarchy-"+uuid.NewString())
	fx.WorkspaceID = workspaceID
	fx.UserID = userID
	fx.Member(t, workspaceID, userID, "owner")
	projectID := fx.Project(t, "Hierarchy Project")
	// Memberships are created through the service, outside Fixture.Insert. Remove
	// them before the Fixture deletes the Project row.
	fx.Cleanup(t, "DELETE FROM project_agent_membership WHERE project_id = $1::uuid", projectID)
	return &hierarchyServiceFixture{
		pool:        pool,
		fx:          fx,
		service:     NewService(db.New(pool), pool),
		workspaceID: pgUUIDFromString(t, workspaceID),
		projectID:   pgUUIDFromString(t, projectID),
	}
}

func (f *hierarchyServiceFixture) agent(t *testing.T, name string, overrides ...testutil.Cols) pgtype.UUID {
	t.Helper()
	return pgUUIDFromString(t, f.fx.Agent(t, name, "", overrides...))
}

func (f *hierarchyServiceFixture) createMembership(t *testing.T, agentID, managerID pgtype.UUID, title string) db.ProjectAgentMembership {
	t.Helper()
	membership, err := f.service.CreateMembership(context.Background(), CreateMembershipInput{
		WorkspaceID:         f.workspaceID,
		ProjectID:           f.projectID,
		AgentID:             agentID,
		ManagerMembershipID: managerID,
		Title:               title,
	})
	if err != nil {
		t.Fatalf("create hierarchy membership: %v", err)
	}
	return membership
}

func (f *hierarchyServiceFixture) memberships(t *testing.T) []db.ProjectAgentMembership {
	t.Helper()
	rows, err := db.New(f.pool).ListProjectHierarchyMemberships(context.Background(), db.ListProjectHierarchyMembershipsParams{
		ProjectID:   f.projectID,
		WorkspaceID: f.workspaceID,
	})
	if err != nil {
		t.Fatalf("list hierarchy memberships: %v", err)
	}
	return rows
}

func requireHierarchyCode(t *testing.T, err error, code ErrorCode) {
	t.Helper()
	if !IsCode(err, code) {
		t.Fatalf("error = %v, want hierarchy code %s", err, code)
	}
}

func TestServiceCreateAndReactivateMembership(t *testing.T) {
	pool := openHierarchyTestPool(t)

	t.Run("first membership becomes root", func(t *testing.T) {
		f := newHierarchyServiceFixture(t, pool)
		root := f.createMembership(t, f.agent(t, "root"), pgtype.UUID{}, "Project Lead")
		if root.ManagerMembershipID.Valid || root.ArchivedAt.Valid {
			t.Fatalf("root = %+v, want active membership without manager", root)
		}
	})

	t.Run("first membership cannot name a manager", func(t *testing.T) {
		f := newHierarchyServiceFixture(t, pool)
		_, err := f.service.CreateMembership(context.Background(), CreateMembershipInput{
			WorkspaceID:         f.workspaceID,
			ProjectID:           f.projectID,
			AgentID:             f.agent(t, "first"),
			ManagerMembershipID: testUUID(t),
		})
		requireHierarchyCode(t, err, CodeRootRequired)
	})

	t.Run("non-root can join under active manager", func(t *testing.T) {
		f := newHierarchyServiceFixture(t, pool)
		root := f.createMembership(t, f.agent(t, "root"), pgtype.UUID{}, "Lead")
		child := f.createMembership(t, f.agent(t, "child"), root.ID, "Engineer")
		if child.ManagerMembershipID != root.ID {
			t.Fatalf("manager = %v, want root %v", child.ManagerMembershipID, root.ID)
		}
	})

	t.Run("second root is rejected", func(t *testing.T) {
		f := newHierarchyServiceFixture(t, pool)
		f.createMembership(t, f.agent(t, "root"), pgtype.UUID{}, "Lead")
		_, err := f.service.CreateMembership(context.Background(), CreateMembershipInput{
			WorkspaceID: f.workspaceID,
			ProjectID:   f.projectID,
			AgentID:     f.agent(t, "other"),
		})
		requireHierarchyCode(t, err, CodeInvalidRootCount)
	})

	t.Run("cross-workspace agent is rejected", func(t *testing.T) {
		f := newHierarchyServiceFixture(t, pool)
		root := f.createMembership(t, f.agent(t, "root"), pgtype.UUID{}, "Lead")
		otherWorkspace := f.fx.Workspace(t, "Other Workspace", "other-"+uuid.NewString())
		otherAgent := f.agent(t, "cross-workspace", testutil.Cols{"workspace_id": otherWorkspace})
		_, err := f.service.CreateMembership(context.Background(), CreateMembershipInput{
			WorkspaceID:         f.workspaceID,
			ProjectID:           f.projectID,
			AgentID:             otherAgent,
			ManagerMembershipID: root.ID,
		})
		requireHierarchyCode(t, err, CodeAgentNotEligible)
	})

	t.Run("system agent is rejected", func(t *testing.T) {
		f := newHierarchyServiceFixture(t, pool)
		root := f.createMembership(t, f.agent(t, "root"), pgtype.UUID{}, "Lead")
		systemAgent := f.agent(t, "system", testutil.Cols{
			"kind":       "system",
			"system_key": "hierarchy-test:" + uuid.NewString(),
		})
		_, err := f.service.CreateMembership(context.Background(), CreateMembershipInput{
			WorkspaceID:         f.workspaceID,
			ProjectID:           f.projectID,
			AgentID:             systemAgent,
			ManagerMembershipID: root.ID,
		})
		requireHierarchyCode(t, err, CodeAgentNotEligible)
	})

	t.Run("archived agent is rejected", func(t *testing.T) {
		f := newHierarchyServiceFixture(t, pool)
		root := f.createMembership(t, f.agent(t, "root"), pgtype.UUID{}, "Lead")
		archived := f.agent(t, "archived", testutil.Cols{"archived_at": testutil.Raw("now()")})
		_, err := f.service.CreateMembership(context.Background(), CreateMembershipInput{
			WorkspaceID:         f.workspaceID,
			ProjectID:           f.projectID,
			AgentID:             archived,
			ManagerMembershipID: root.ID,
		})
		requireHierarchyCode(t, err, CodeAgentArchived)
	})

	t.Run("archived membership reuses the same row", func(t *testing.T) {
		f := newHierarchyServiceFixture(t, pool)
		root := f.createMembership(t, f.agent(t, "root"), pgtype.UUID{}, "Lead")
		agentID := f.agent(t, "returning")
		original := f.createMembership(t, agentID, root.ID, "Engineer")
		if _, err := f.service.ArchiveMembership(context.Background(), ArchiveMembershipInput{
			WorkspaceID: f.workspaceID, ProjectID: f.projectID, MembershipID: original.ID,
		}); err != nil {
			t.Fatalf("archive leaf: %v", err)
		}
		reactivated, err := f.service.CreateMembership(context.Background(), CreateMembershipInput{
			WorkspaceID: f.workspaceID, ProjectID: f.projectID, AgentID: agentID,
			ManagerMembershipID: root.ID, Title: "Senior Engineer",
		})
		if err != nil {
			t.Fatalf("reactivate membership: %v", err)
		}
		if reactivated.ID != original.ID {
			t.Fatalf("reactivated id = %v, want existing id %v", reactivated.ID, original.ID)
		}
		if reactivated.ArchivedAt.Valid || reactivated.Title != "Senior Engineer" {
			t.Fatalf("reactivated membership = %+v", reactivated)
		}
	})
}

func TestServiceReparentAndArchiveRules(t *testing.T) {
	pool := openHierarchyTestPool(t)

	t.Run("missing or inactive manager is rejected", func(t *testing.T) {
		f := newHierarchyServiceFixture(t, pool)
		root := f.createMembership(t, f.agent(t, "root"), pgtype.UUID{}, "Lead")
		child := f.createMembership(t, f.agent(t, "child"), root.ID, "Engineer")
		_, err := f.service.ReparentMembership(context.Background(), ReparentMembershipInput{
			WorkspaceID: f.workspaceID, ProjectID: f.projectID,
			MembershipID: child.ID, ManagerMembershipID: testUUID(t),
		})
		requireHierarchyCode(t, err, CodeManagerNotActive)

		inactive := f.createMembership(t, f.agent(t, "inactive-manager"), root.ID, "Lead")
		if _, err := f.service.ArchiveMembership(context.Background(), ArchiveMembershipInput{
			WorkspaceID: f.workspaceID, ProjectID: f.projectID, MembershipID: inactive.ID,
		}); err != nil {
			t.Fatalf("archive inactive manager fixture: %v", err)
		}
		_, err = f.service.ReparentMembership(context.Background(), ReparentMembershipInput{
			WorkspaceID: f.workspaceID, ProjectID: f.projectID,
			MembershipID: child.ID, ManagerMembershipID: inactive.ID,
		})
		requireHierarchyCode(t, err, CodeManagerNotActive)
	})

	t.Run("self manager is rejected", func(t *testing.T) {
		f := newHierarchyServiceFixture(t, pool)
		root := f.createMembership(t, f.agent(t, "root"), pgtype.UUID{}, "Lead")
		child := f.createMembership(t, f.agent(t, "child"), root.ID, "Engineer")
		_, err := f.service.ReparentMembership(context.Background(), ReparentMembershipInput{
			WorkspaceID: f.workspaceID, ProjectID: f.projectID,
			MembershipID: child.ID, ManagerMembershipID: child.ID,
		})
		requireHierarchyCode(t, err, CodeCycleDetected)
	})

	t.Run("direct cycle is rejected", func(t *testing.T) {
		f := newHierarchyServiceFixture(t, pool)
		root := f.createMembership(t, f.agent(t, "root"), pgtype.UUID{}, "Lead")
		a := f.createMembership(t, f.agent(t, "a"), root.ID, "A")
		b := f.createMembership(t, f.agent(t, "b"), root.ID, "B")
		if _, err := f.service.ReparentMembership(context.Background(), ReparentMembershipInput{
			WorkspaceID: f.workspaceID, ProjectID: f.projectID,
			MembershipID: a.ID, ManagerMembershipID: b.ID,
		}); err != nil {
			t.Fatalf("first reparent: %v", err)
		}
		_, err := f.service.ReparentMembership(context.Background(), ReparentMembershipInput{
			WorkspaceID: f.workspaceID, ProjectID: f.projectID,
			MembershipID: b.ID, ManagerMembershipID: a.ID,
		})
		requireHierarchyCode(t, err, CodeCycleDetected)
	})

	t.Run("transitive cycle is rejected", func(t *testing.T) {
		f := newHierarchyServiceFixture(t, pool)
		root := f.createMembership(t, f.agent(t, "root"), pgtype.UUID{}, "Root")
		a := f.createMembership(t, f.agent(t, "a"), root.ID, "A")
		b := f.createMembership(t, f.agent(t, "b"), a.ID, "B")
		c := f.createMembership(t, f.agent(t, "c"), b.ID, "C")
		_, err := f.service.ReparentMembership(context.Background(), ReparentMembershipInput{
			WorkspaceID: f.workspaceID, ProjectID: f.projectID,
			MembershipID: a.ID, ManagerMembershipID: c.ID,
		})
		requireHierarchyCode(t, err, CodeCycleDetected)
	})

	t.Run("valid subtree reparent preserves descendants", func(t *testing.T) {
		f := newHierarchyServiceFixture(t, pool)
		root := f.createMembership(t, f.agent(t, "root"), pgtype.UUID{}, "Root")
		a := f.createMembership(t, f.agent(t, "a"), root.ID, "A")
		b := f.createMembership(t, f.agent(t, "b"), a.ID, "B")
		c := f.createMembership(t, f.agent(t, "c"), root.ID, "C")
		if _, err := f.service.ReparentMembership(context.Background(), ReparentMembershipInput{
			WorkspaceID: f.workspaceID, ProjectID: f.projectID,
			MembershipID: a.ID, ManagerMembershipID: c.ID,
		}); err != nil {
			t.Fatalf("reparent subtree: %v", err)
		}
		rows := f.memberships(t)
		_, gotA, _ := findMembershipByID(rows, a.ID)
		_, gotB, _ := findMembershipByID(rows, b.ID)
		if gotA.ManagerMembershipID != c.ID || gotB.ManagerMembershipID != a.ID {
			t.Fatalf("unexpected subtree after reparent: a=%+v b=%+v", gotA, gotB)
		}
	})

	t.Run("archive leaf succeeds", func(t *testing.T) {
		f := newHierarchyServiceFixture(t, pool)
		root := f.createMembership(t, f.agent(t, "root"), pgtype.UUID{}, "Root")
		leaf := f.createMembership(t, f.agent(t, "leaf"), root.ID, "Leaf")
		archived, err := f.service.ArchiveMembership(context.Background(), ArchiveMembershipInput{
			WorkspaceID: f.workspaceID, ProjectID: f.projectID, MembershipID: leaf.ID,
		})
		if err != nil {
			t.Fatalf("archive leaf: %v", err)
		}
		if !archived.ArchivedAt.Valid {
			t.Fatalf("archive did not set archived_at: %+v", archived)
		}
	})

	t.Run("archive root is rejected", func(t *testing.T) {
		f := newHierarchyServiceFixture(t, pool)
		root := f.createMembership(t, f.agent(t, "root"), pgtype.UUID{}, "Root")
		_, err := f.service.ArchiveMembership(context.Background(), ArchiveMembershipInput{
			WorkspaceID: f.workspaceID, ProjectID: f.projectID, MembershipID: root.ID,
		})
		requireHierarchyCode(t, err, CodeRootChangeRequiresBulk)
	})

	t.Run("archive manager with descendants is rejected", func(t *testing.T) {
		f := newHierarchyServiceFixture(t, pool)
		root := f.createMembership(t, f.agent(t, "root"), pgtype.UUID{}, "Root")
		manager := f.createMembership(t, f.agent(t, "manager"), root.ID, "Manager")
		f.createMembership(t, f.agent(t, "leaf"), manager.ID, "Leaf")
		_, err := f.service.ArchiveMembership(context.Background(), ArchiveMembershipInput{
			WorkspaceID: f.workspaceID, ProjectID: f.projectID, MembershipID: manager.ID,
		})
		requireHierarchyCode(t, err, CodeOrphaningMembership)
	})
}

func TestServiceApplyStructure(t *testing.T) {
	pool := openHierarchyTestPool(t)

	t.Run("explicit root replacement succeeds", func(t *testing.T) {
		f := newHierarchyServiceFixture(t, pool)
		oldRoot := f.createMembership(t, f.agent(t, "old-root"), pgtype.UUID{}, "Old Root")
		newRoot := f.createMembership(t, f.agent(t, "new-root"), oldRoot.ID, "New Root")
		leaf := f.createMembership(t, f.agent(t, "leaf"), newRoot.ID, "Leaf")
		rows, err := f.service.ApplyStructure(context.Background(), ApplyStructureInput{
			WorkspaceID: f.workspaceID,
			ProjectID:   f.projectID,
			Changes: []ManagerChange{
				{MembershipID: oldRoot.ID, ManagerMembershipID: newRoot.ID},
				{MembershipID: newRoot.ID, ManagerMembershipID: pgtype.UUID{}},
			},
		})
		if err != nil {
			t.Fatalf("replace root: %v", err)
		}
		_, gotOld, _ := findMembershipByID(rows, oldRoot.ID)
		_, gotNew, _ := findMembershipByID(rows, newRoot.ID)
		_, gotLeaf, _ := findMembershipByID(rows, leaf.ID)
		if gotOld.ManagerMembershipID != newRoot.ID || gotNew.ManagerMembershipID.Valid || gotLeaf.ManagerMembershipID != newRoot.ID {
			t.Fatalf("unexpected root replacement: old=%+v new=%+v leaf=%+v", gotOld, gotNew, gotLeaf)
		}
		if err := validateActiveTree(rows); err != nil {
			t.Fatalf("persisted tree invalid: %v", err)
		}
	})

	t.Run("invalid reorganization rolls back and releases project lock", func(t *testing.T) {
		f := newHierarchyServiceFixture(t, pool)
		root := f.createMembership(t, f.agent(t, "root"), pgtype.UUID{}, "Root")
		a := f.createMembership(t, f.agent(t, "a"), root.ID, "A")
		b := f.createMembership(t, f.agent(t, "b"), a.ID, "B")
		c := f.createMembership(t, f.agent(t, "c"), root.ID, "C")

		_, err := f.service.ApplyStructure(context.Background(), ApplyStructureInput{
			WorkspaceID: f.workspaceID,
			ProjectID:   f.projectID,
			Changes: []ManagerChange{
				{MembershipID: a.ID, ManagerMembershipID: b.ID},
			},
		})
		requireHierarchyCode(t, err, CodeCycleDetected)

		rows := f.memberships(t)
		_, gotA, _ := findMembershipByID(rows, a.ID)
		_, gotB, _ := findMembershipByID(rows, b.ID)
		if gotA.ManagerMembershipID != root.ID || gotB.ManagerMembershipID != a.ID {
			t.Fatalf("invalid bulk mutation changed persisted tree: a=%+v b=%+v", gotA, gotB)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if _, err := f.service.ReparentMembership(ctx, ReparentMembershipInput{
			WorkspaceID: f.workspaceID, ProjectID: f.projectID,
			MembershipID: c.ID, ManagerMembershipID: a.ID,
		}); err != nil {
			t.Fatalf("valid mutation after rollback failed or lock leaked: %v", err)
		}
	})
}

func TestConcurrentStructuralMutationsSerializeOnProject(t *testing.T) {
	pool := openHierarchyTestPool(t)

	t.Run("opposite reparent operations cannot commit a cycle", func(t *testing.T) {
		f := newHierarchyServiceFixture(t, pool)
		root := f.createMembership(t, f.agent(t, "root"), pgtype.UUID{}, "Root")
		a := f.createMembership(t, f.agent(t, "a"), root.ID, "A")
		b := f.createMembership(t, f.agent(t, "b"), root.ID, "B")

		start := make(chan struct{})
		results := make(chan error, 2)
		go func() {
			<-start
			_, err := f.service.ReparentMembership(context.Background(), ReparentMembershipInput{
				WorkspaceID: f.workspaceID, ProjectID: f.projectID,
				MembershipID: a.ID, ManagerMembershipID: b.ID,
			})
			results <- err
		}()
		go func() {
			<-start
			_, err := f.service.ReparentMembership(context.Background(), ReparentMembershipInput{
				WorkspaceID: f.workspaceID, ProjectID: f.projectID,
				MembershipID: b.ID, ManagerMembershipID: a.ID,
			})
			results <- err
		}()
		close(start)

		errs := []error{<-results, <-results}
		successes := 0
		cycleRejects := 0
		for _, err := range errs {
			if err == nil {
				successes++
				continue
			}
			if IsCode(err, CodeCycleDetected) {
				cycleRejects++
				continue
			}
			t.Fatalf("unexpected concurrent reparent error: %v", err)
		}
		if successes != 1 || cycleRejects != 1 {
			t.Fatalf("concurrent outcomes: successes=%d cycle_rejects=%d errors=%v", successes, cycleRejects, errs)
		}
		rows := f.memberships(t)
		if err := validateActiveTree(rows); err != nil {
			t.Fatalf("committed tree is invalid: %v", err)
		}
	})

	t.Run("competing root replacements revalidate after serialization", func(t *testing.T) {
		f := newHierarchyServiceFixture(t, pool)
		root := f.createMembership(t, f.agent(t, "root"), pgtype.UUID{}, "Root")
		b := f.createMembership(t, f.agent(t, "b"), root.ID, "B")
		c := f.createMembership(t, f.agent(t, "c"), root.ID, "C")

		start := make(chan struct{})
		results := make(chan error, 2)
		run := func(newRoot pgtype.UUID) {
			<-start
			_, err := f.service.ApplyStructure(context.Background(), ApplyStructureInput{
				WorkspaceID: f.workspaceID,
				ProjectID:   f.projectID,
				Changes: []ManagerChange{
					{MembershipID: root.ID, ManagerMembershipID: newRoot},
					{MembershipID: newRoot, ManagerMembershipID: pgtype.UUID{}},
				},
			})
			results <- err
		}
		go run(b.ID)
		go run(c.ID)
		close(start)

		errs := []error{<-results, <-results}
		successes := 0
		rootRejects := 0
		for _, err := range errs {
			if err == nil {
				successes++
				continue
			}
			if IsCode(err, CodeInvalidRootCount) {
				rootRejects++
				continue
			}
			t.Fatalf("unexpected concurrent root change error: %v", err)
		}
		if successes != 1 || rootRejects != 1 {
			t.Fatalf("concurrent root outcomes: successes=%d root_rejects=%d errors=%v", successes, rootRejects, errs)
		}
		rows := f.memberships(t)
		if err := validateActiveTree(rows); err != nil {
			t.Fatalf("committed root tree is invalid: %v", err)
		}
	})
}
