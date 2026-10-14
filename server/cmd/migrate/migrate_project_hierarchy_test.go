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
		"579_project_agent_membership_primary_key",
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

		assertProjectHierarchyTableShape(t, ctx, pool, schema)
		assertProjectHierarchyPrimaryKey(t, ctx, pool, schema)
		assertProjectHierarchyIndex(t, ctx, pool, schema, "project_agent_membership_project_agent_idx", true, []string{"(project_id, agent_id)"})
		assertProjectHierarchyIndex(t, ctx, pool, schema, "project_agent_membership_active_root_idx", true, []string{"(project_id)", "manager_membership_id IS NULL", "archived_at IS NULL"})
		assertProjectHierarchyIndex(t, ctx, pool, schema, "project_agent_membership_manager_idx", false, []string{"(project_id, manager_membership_id)", "archived_at IS NULL", "manager_membership_id IS NOT NULL"})
		assertProjectHierarchyIndex(t, ctx, pool, schema, "project_agent_membership_workspace_idx", false, []string{"(workspace_id)"})

		assertHierarchyConstraintErrors(t, ctx, pool)
	}
}

func assertProjectHierarchyTableShape(t *testing.T, ctx context.Context, pool queryRower, schema string) {
	t.Helper()

	type columnExpectation struct {
		name            string
		dataType        string
		nullable        bool
		defaultContains string
	}
	for _, want := range []columnExpectation{
		{name: "id", dataType: "uuid", nullable: false, defaultContains: "gen_random_uuid"},
		{name: "workspace_id", dataType: "uuid", nullable: false},
		{name: "project_id", dataType: "uuid", nullable: false},
		{name: "agent_id", dataType: "uuid", nullable: false},
		{name: "manager_membership_id", dataType: "uuid", nullable: true},
		{name: "title", dataType: "text", nullable: false, defaultContains: "''::text"},
		{name: "archived_at", dataType: "timestamp with time zone", nullable: true},
		{name: "created_at", dataType: "timestamp with time zone", nullable: false, defaultContains: "now()"},
		{name: "updated_at", dataType: "timestamp with time zone", nullable: false, defaultContains: "now()"},
	} {
		var dataType, defaultValue string
		var nullable bool
		if err := pool.QueryRow(ctx, `
SELECT data_type, is_nullable = 'YES', COALESCE(column_default, '')
FROM information_schema.columns
WHERE table_schema = $1 AND table_name = 'project_agent_membership' AND column_name = $2
`, schema, want.name).Scan(&dataType, &nullable, &defaultValue); err != nil {
			t.Fatalf("inspect column %s: %v", want.name, err)
		}
		if dataType != want.dataType || nullable != want.nullable {
			t.Fatalf("column %s type/nullable = %s/%v, want %s/%v", want.name, dataType, nullable, want.dataType, want.nullable)
		}
		if want.defaultContains != "" && !strings.Contains(defaultValue, want.defaultContains) {
			t.Fatalf("column %s default %q missing %q", want.name, defaultValue, want.defaultContains)
		}
	}

	var checkDefinition string
	if err := pool.QueryRow(ctx, `
SELECT pg_get_constraintdef(c.oid)
FROM pg_constraint c
JOIN pg_class r ON r.oid = c.conrelid
JOIN pg_namespace n ON n.oid = r.relnamespace
WHERE n.nspname = $1
  AND r.relname = 'project_agent_membership'
  AND c.conname = 'project_agent_membership_manager_not_self'
`, schema).Scan(&checkDefinition); err != nil {
		t.Fatalf("inspect self-manager CHECK: %v", err)
	}
	if !strings.Contains(checkDefinition, "manager_membership_id <> id") {
		t.Fatalf("self-manager CHECK definition = %q", checkDefinition)
	}

	var foreignKeys, userTriggers, rows int
	if err := pool.QueryRow(ctx, `
SELECT count(*)
FROM pg_constraint c
JOIN pg_class r ON r.oid = c.conrelid
JOIN pg_namespace n ON n.oid = r.relnamespace
WHERE n.nspname = $1 AND r.relname = 'project_agent_membership' AND c.contype = 'f'
`, schema).Scan(&foreignKeys); err != nil {
		t.Fatalf("inspect hierarchy foreign keys: %v", err)
	}
	if foreignKeys != 0 {
		t.Fatalf("project_agent_membership has %d foreign keys, want 0 under ADR-014", foreignKeys)
	}
	if err := pool.QueryRow(ctx, `
SELECT count(*)
FROM pg_trigger t
JOIN pg_class r ON r.oid = t.tgrelid
JOIN pg_namespace n ON n.oid = r.relnamespace
WHERE n.nspname = $1 AND r.relname = 'project_agent_membership' AND NOT t.tgisinternal
`, schema).Scan(&userTriggers); err != nil {
		t.Fatalf("inspect hierarchy triggers: %v", err)
	}
	if userTriggers != 0 {
		t.Fatalf("project_agent_membership has %d user triggers, want 0", userTriggers)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM project_agent_membership`).Scan(&rows); err != nil {
		t.Fatalf("count hierarchy rows after migration: %v", err)
	}
	if rows != 0 {
		t.Fatalf("hierarchy migration backfilled %d rows, want 0", rows)
	}
}

func assertProjectHierarchyPrimaryKey(t *testing.T, ctx context.Context, pool queryRower, schema string) {
	t.Helper()
	var definition string
	if err := pool.QueryRow(ctx, `
SELECT pg_get_constraintdef(c.oid)
FROM pg_constraint c
JOIN pg_class r ON r.oid = c.conrelid
JOIN pg_namespace n ON n.oid = r.relnamespace
WHERE n.nspname = $1
  AND r.relname = 'project_agent_membership'
  AND c.conname = 'project_agent_membership_pkey'
  AND c.contype = 'p'
`, schema).Scan(&definition); err != nil {
		t.Fatalf("inspect hierarchy primary key: %v", err)
	}
	if !strings.Contains(definition, "PRIMARY KEY (id)") {
		t.Fatalf("hierarchy primary key definition = %q", definition)
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
