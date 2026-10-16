package main

import (
	"context"
	"math/rand/v2"
	"testing"
	"time"
)

func TestProjectHierarchyLegacyMigrationNumberingConvergesWithoutRebuild(t *testing.T) {
	base := openTestPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	schema := createScratchSchema(t, ctx, base, "project_hierarchy_legacy_")
	pool := openTestPoolWithSearchPath(t, schema)

	currentVersions := []string{
		"900000_project_agent_membership",
		"900001_project_agent_membership_id_idx",
		"900002_project_agent_membership_project_agent_idx",
		"900003_project_agent_membership_active_root_idx",
		"900004_project_agent_membership_manager_idx",
		"900005_project_agent_membership_workspace_idx",
		"900006_project_agent_membership_primary_key",
	}
	legacyVersions := []string{
		"573_project_agent_membership",
		"574_project_agent_membership_id_idx",
		"575_project_agent_membership_project_agent_idx",
		"576_project_agent_membership_active_root_idx",
		"577_project_agent_membership_manager_idx",
		"578_project_agent_membership_workspace_idx",
		"579_project_agent_membership_primary_key",
	}

	options := runOptions{
		Direction:             "up",
		Files:                 realMigrationFiles(t, currentVersions, "up"),
		SchemaMigrationsTable: schema + ".schema_migrations",
		AdvisoryLockKey:       int64(rand.Uint64()&0x7fffffffffffffff) | 1,
		Hooks:                 hooksForDirection("up"),
		Conditions:            conditionsForDirection("up"),
	}
	if err := runMigrations(ctx, pool, options); err != nil {
		t.Fatalf("establish hierarchy schema: %v", err)
	}

	// Simulate an installation that applied the exact same hierarchy schema
	// before MFK-010 moved fork-owned migrations out of upstream's numeric band.
	for i := range currentVersions {
		if _, err := pool.Exec(ctx, `
UPDATE schema_migrations
SET version = $1
WHERE version = $2
`, legacyVersions[i], currentVersions[i]); err != nil {
			t.Fatalf("rewrite migration ledger %s -> %s: %v", currentVersions[i], legacyVersions[i], err)
		}
	}

	// Re-running the current filenames must converge the ledger without
	// rebuilding the promoted id index or attempting to add a second PK.
	if err := runMigrations(ctx, pool, options); err != nil {
		t.Fatalf("converge legacy hierarchy migration ledger: %v", err)
	}

	assertProjectHierarchyTableShape(t, ctx, pool, schema)
	assertProjectHierarchyPrimaryKey(t, ctx, pool, schema)
	assertProjectHierarchyIndex(t, ctx, pool, schema, "project_agent_membership_project_agent_idx", true, []string{"(project_id, agent_id)"})
	assertProjectHierarchyIndex(t, ctx, pool, schema, "project_agent_membership_active_root_idx", true, []string{"(project_id)", "manager_membership_id IS NULL", "archived_at IS NULL"})
	assertProjectHierarchyIndex(t, ctx, pool, schema, "project_agent_membership_manager_idx", false, []string{"(project_id, manager_membership_id)", "archived_at IS NULL", "manager_membership_id IS NOT NULL"})
	assertProjectHierarchyIndex(t, ctx, pool, schema, "project_agent_membership_workspace_idx", false, []string{"(workspace_id)"})

	for _, version := range append(append([]string{}, legacyVersions...), currentVersions...) {
		var recorded bool
		if err := pool.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1
    FROM schema_migrations
    WHERE version = $1
)
`, version).Scan(&recorded); err != nil {
			t.Fatalf("inspect migration ledger %s: %v", version, err)
		}
		if !recorded {
			t.Fatalf("migration ledger missing %s", version)
		}
	}

	var primaryKeys int
	if err := pool.QueryRow(ctx, `
SELECT count(*)
FROM pg_constraint AS c
JOIN pg_class AS r ON r.oid = c.conrelid
JOIN pg_namespace AS n ON n.oid = r.relnamespace
WHERE n.nspname = $1
  AND r.relname = 'project_agent_membership'
  AND c.contype = 'p'
`, schema).Scan(&primaryKeys); err != nil {
		t.Fatalf("count hierarchy primary keys: %v", err)
	}
	if primaryKeys != 1 {
		t.Fatalf("project_agent_membership primary key count=%d, want 1", primaryKeys)
	}

	var redundantIDIndex bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, schema+".project_agent_membership_id_idx").Scan(&redundantIDIndex); err != nil {
		t.Fatalf("inspect redundant hierarchy id index: %v", err)
	}
	if redundantIDIndex {
		t.Fatal("legacy-numbering convergence created redundant project_agent_membership_id_idx")
	}
}
