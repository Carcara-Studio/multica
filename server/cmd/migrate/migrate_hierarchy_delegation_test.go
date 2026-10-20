package main

import (
	"context"
	"math/rand/v2"
	"slices"
	"testing"
	"time"
)

func TestHierarchyDelegationMigrationsUpDownUp(t *testing.T) {
	base := openTestPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	schema := createScratchSchema(t, ctx, base, "delegation_")
	pool := openTestPoolWithSearchPath(t, schema)
	versions := []string{
		"900024_project_hierarchy_delegation",
		"900025_project_hierarchy_delegation_identity_idx",
	}
	for _, direction := range []string{"up", "down", "up"} {
		selected := slices.Clone(versions)
		if direction == "down" {
			slices.Reverse(selected)
		}
		if err := runMigrations(ctx, pool, runOptions{
			Direction: direction, Files: realMigrationFiles(t, selected, direction),
			SchemaMigrationsTable: schema + ".schema_migrations",
			AdvisoryLockKey: int64(rand.Uint64()&0x7fffffffffffffff) | 1,
			Hooks: hooksForDirection(direction), Conditions: conditionsForDirection(direction),
		}); err != nil {
			t.Fatalf("migrate %s: %v", direction, err)
		}
		var exists bool
		if err := pool.QueryRow(ctx, "SELECT to_regclass($1) IS NOT NULL", schema+".project_hierarchy_delegation").Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if exists != (direction == "up") {
			t.Fatalf("after %s table exists=%v", direction, exists)
		}
		if !exists {
			continue
		}
		assertProjectHierarchyIndex(t, ctx, pool, schema, "project_hierarchy_delegation_identity_idx", true,
			[]string{"(workspace_id, parent_issue_id, delegator_agent_id, request_id)"})
		var fks int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM pg_constraint WHERE conrelid=to_regclass($1) AND contype='f'", schema+".project_hierarchy_delegation").Scan(&fks); err != nil || fks != 0 {
			t.Fatalf("foreign keys = %d, %v", fks, err)
		}
	}
}
