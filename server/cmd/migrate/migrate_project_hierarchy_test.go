package main

import (
	"context"
	"errors"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestProjectHierarchyMigrationsUpDownUpInIsolatedSchema(t *testing.T) {
	base := openTestPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	schema := createScratchSchema(t, ctx, base, "project_hierarchy_")
	pool := openTestPoolWithSearchPath(t, schema)

	baseVersions := []string{
		"573_project_agent_membership",
		"574_project_agent_membership_id_idx",
		"575_project_agent_membership_project_agent_idx",
		"576_project_agent_membership_active_root_idx",
		"577_project_agent_membership_manager_idx",
		"578_project_agent_membership_workspace_idx",
	}

	for _, direction := range []string{"up", "down", "up"} {
		versions := slices.Clone(baseVersions)
		if direction == "down" {
			slices.Reverse(versions)
		}
		if err := runMigrations(ctx, pool, runOptions{
			Direction:             direction,
			Files:                 realMigrationFiles(t, versions, direction),
			SchemaMigrationsTable: schema + ".schema_migrations",
			AdvisoryLockKey:       int64(rand.Uint64()&0x7fffffffffffffff) | 1,
			Hooks:                 hooksForDirection(direction),
			Conditions:            conditionsForDirection(direction),
		}); err != nil {
			t.Fatalf("migrate %s: %v", direction, err)
		}

		var tableExists bool
		if err := pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, schema+".project_agent_membership").Scan(&tableExists); err != nil {
			t.Fatalf("after %s, inspect hierarchy table: %v", direction, err)
		}
		if tableExists != (direction == "up") {
			t.Fatalf("after %s, project_agent_membership exists=%v", direction, tableExists)
		}
		if direction != "up" {
			continue
		}

		assertProjectHierarchyIndex(t, ctx, pool, schema, "project_agent_membership_id_idx", true, []string{"(id)"})
		assertProjectHierarchyIndex(t, ctx, pool, schema, "project_agent_membership_project_agent_idx", true, []string{"(project_id, agent_id)"})
		assertProjectHierarchyIndex(t, ctx, pool, schema, "project_agent_membership_active_root_idx", true, []string{"(project_id)", "manager_membership_id IS NULL", "archived_at IS NULL"})
		assertProjectHierarchyIndex(t, ctx, pool, schema, "project_agent_membership_manager_idx", false, []string{"(project_id, manager_membership_id)", "archived_at IS NULL", "manager_membership_id IS NOT NULL"})
		assertProjectHierarchyIndex(t, ctx, pool, schema, "project_agent_membership_workspace_idx", false, []string{"(workspace_id)"})

		assertHierarchyConstraintErrors(t, ctx, pool)
	}
}

func assertProjectHierarchyIndex(t *testing.T, ctx context.Context, pool queryRower, schema, name string, wantUnique bool, fragments []string) {
	t.Helper()
	var valid, unique bool
	var definition string
	if err := pool.QueryRow(ctx, `
SELECT i.indisvalid, i.indisunique, pg_get_indexdef(i.indexrelid)
FROM pg_index i
JOIN pg_class c ON c.oid = i.indexrelid
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = $1 AND c.relname = $2
`, schema, name).Scan(&valid, &unique, &definition); err != nil {
		t.Fatalf("inspect index %s: %v", name, err)
	}
	if !valid {
		t.Fatalf("index %s is INVALID", name)
	}
	if unique != wantUnique {
		t.Fatalf("index %s unique=%v, want %v", name, unique, wantUnique)
	}
	for _, fragment := range fragments {
		if !strings.Contains(definition, fragment) {
			t.Fatalf("index %s definition %q missing %q", name, definition, fragment)
		}
	}
}

// queryRower is the exact small pgx surface needed by the index assertion.
// pgxpool.Pool and pgx.Tx both return pgx.Row from QueryRow.
type queryRower interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func assertHierarchyConstraintErrors(t *testing.T, ctx context.Context, pool execQuerier) {
	t.Helper()

	const (
		ws1      = "00000000-0000-0000-0000-000000000101"
		ws2      = "00000000-0000-0000-0000-000000000102"
		project1 = "00000000-0000-0000-0000-000000000201"
		project2 = "00000000-0000-0000-0000-000000000202"
		agent1   = "00000000-0000-0000-0000-000000000301"
		agent2   = "00000000-0000-0000-0000-000000000302"
		id1      = "00000000-0000-0000-0000-000000000401"
		id2      = "00000000-0000-0000-0000-000000000402"
	)

	if _, err := pool.Exec(ctx, `TRUNCATE project_agent_membership`); err != nil {
		t.Fatal(err)
	}

	_, err := pool.Exec(ctx, `
INSERT INTO project_agent_membership (id, workspace_id, project_id, agent_id, manager_membership_id, archived_at)
VALUES ($1, $2, $3, $4, $1, now())
`, id1, ws1, project1, agent1)
	assertPostgresCode(t, err, "23514", "self-manager CHECK")

	if _, err := pool.Exec(ctx, `
INSERT INTO project_agent_membership (id, workspace_id, project_id, agent_id, archived_at)
VALUES ($1, $2, $3, $4, now())
`, id1, ws1, project1, agent1); err != nil {
		t.Fatalf("seed id uniqueness row: %v", err)
	}
	_, err = pool.Exec(ctx, `
INSERT INTO project_agent_membership (id, workspace_id, project_id, agent_id, archived_at)
VALUES ($1, $2, $3, $4, now())
`, id1, ws2, project2, agent2)
	assertPostgresCode(t, err, "23505", "membership id uniqueness")

	_, err = pool.Exec(ctx, `
INSERT INTO project_agent_membership (id, workspace_id, project_id, agent_id, archived_at)
VALUES ($1, $2, $3, $4, now())
`, id2, ws1, project1, agent1)
	assertPostgresCode(t, err, "23505", "project-agent uniqueness")

	if _, err := pool.Exec(ctx, `TRUNCATE project_agent_membership`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO project_agent_membership (workspace_id, project_id, agent_id)
VALUES ($1, $2, $3)
`, ws1, project1, agent1); err != nil {
		t.Fatalf("seed active root: %v", err)
	}
	_, err = pool.Exec(ctx, `
INSERT INTO project_agent_membership (workspace_id, project_id, agent_id)
VALUES ($1, $2, $3)
`, ws1, project1, agent2)
	assertPostgresCode(t, err, "23505", "active-root uniqueness")

	// Historical/archived membership rows are not active roots and may coexist
	// with the one active root. The Project/Agent uniqueness still applies, so use
	// a third Agent identity here.
	if _, err := pool.Exec(ctx, `
INSERT INTO project_agent_membership (workspace_id, project_id, agent_id, archived_at)
VALUES ($1, $2, '00000000-0000-0000-0000-000000000303', now())
`, ws1, project1); err != nil {
		t.Fatalf("archived root-shaped row should coexist with active root: %v", err)
	}
}

func assertPostgresCode(t *testing.T, err error, wantCode, label string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s unexpectedly succeeded", label)
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != wantCode {
		t.Fatalf("%s error=%v, want PostgreSQL code %s", label, err, wantCode)
	}
}

type execQuerier interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}
