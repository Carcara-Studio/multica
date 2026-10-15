package projecthierarchy

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func testUUID(t *testing.T) pgtype.UUID {
	t.Helper()
	parsed := uuid.New()
	return pgtype.UUID{Bytes: [16]byte(parsed), Valid: true}
}

func testMembership(id, manager pgtype.UUID) db.ProjectAgentMembership {
	return db.ProjectAgentMembership{ID: id, ManagerMembershipID: manager}
}

func TestValidateActiveTree(t *testing.T) {
	t.Run("empty hierarchy is unconfigured", func(t *testing.T) {
		if err := validateActiveTree(nil); err != nil {
			t.Fatalf("validate empty tree: %v", err)
		}
	})

	t.Run("valid deep tree", func(t *testing.T) {
		root := testUUID(t)
		manager := testUUID(t)
		leaf := testUUID(t)
		rows := []db.ProjectAgentMembership{
			testMembership(root, pgtype.UUID{}),
			testMembership(manager, root),
			testMembership(leaf, manager),
		}
		if err := validateActiveTree(rows); err != nil {
			t.Fatalf("validate tree: %v", err)
		}
	})

	t.Run("missing active manager", func(t *testing.T) {
		root := testUUID(t)
		leaf := testUUID(t)
		missing := testUUID(t)
		err := validateActiveTree([]db.ProjectAgentMembership{
			testMembership(root, pgtype.UUID{}),
			testMembership(leaf, missing),
		})
		if !IsCode(err, CodeManagerNotActive) {
			t.Fatalf("error = %v, want %s", err, CodeManagerNotActive)
		}
	})

	t.Run("self cycle", func(t *testing.T) {
		root := testUUID(t)
		leaf := testUUID(t)
		err := validateActiveTree([]db.ProjectAgentMembership{
			testMembership(root, pgtype.UUID{}),
			testMembership(leaf, leaf),
		})
		if !IsCode(err, CodeCycleDetected) {
			t.Fatalf("error = %v, want %s", err, CodeCycleDetected)
		}
	})

	t.Run("transitive cycle", func(t *testing.T) {
		a := testUUID(t)
		b := testUUID(t)
		c := testUUID(t)
		err := validateActiveTree([]db.ProjectAgentMembership{
			testMembership(a, b),
			testMembership(b, c),
			testMembership(c, a),
		})
		if !IsCode(err, CodeCycleDetected) {
			t.Fatalf("error = %v, want %s", err, CodeCycleDetected)
		}
	})

	t.Run("multiple roots", func(t *testing.T) {
		a := testUUID(t)
		b := testUUID(t)
		err := validateActiveTree([]db.ProjectAgentMembership{
			testMembership(a, pgtype.UUID{}),
			testMembership(b, pgtype.UUID{}),
		})
		if !IsCode(err, CodeInvalidRootCount) {
			t.Fatalf("error = %v, want %s", err, CodeInvalidRootCount)
		}
	})

	t.Run("archived rows do not participate", func(t *testing.T) {
		root := testUUID(t)
		archived := testMembership(testUUID(t), pgtype.UUID{})
		archived.ArchivedAt.Valid = true
		if err := validateActiveTree([]db.ProjectAgentMembership{
			testMembership(root, pgtype.UUID{}),
			archived,
		}); err != nil {
			t.Fatalf("archived row affected active tree: %v", err)
		}
	})
}
