package main

import (
	"context"
	"math/rand/v2"
	"testing"
	"time"
)

func TestHierarchyInitialTaskMigrationUpDownUp(t *testing.T) {
	base := openTestPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	schema := createScratchSchema(t, ctx, base, "hierarchy_dispatch_")
	pool := openTestPoolWithSearchPath(t, schema)
	versions := []string{"900024_project_hierarchy_delegation", "900025_project_hierarchy_delegation_identity_idx", "900026_project_hierarchy_escalation", "900027_project_hierarchy_escalation_identity_idx"}
	run := func(direction string, selected []string) {
		t.Helper()
		if err := runMigrations(ctx, pool, runOptions{Direction: direction, Files: realMigrationFiles(t, selected, direction), SchemaMigrationsTable: schema + ".schema_migrations", AdvisoryLockKey: int64(rand.Uint64()&0x7fffffffffffffff) | 1, Hooks: hooksForDirection(direction), Conditions: conditionsForDirection(direction)}); err != nil {
			t.Fatalf("%s: %v", direction, err)
		}
	}
	run("up", versions)
	for _, direction := range []string{"up", "down", "up"} {
		run(direction, []string{"900028_hierarchy_initial_task"})
		for _, table := range []string{"project_hierarchy_delegation", "project_hierarchy_escalation"} {
			var exists bool
			if err := pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema=$1 AND table_name=$2 AND column_name='initial_task_id' AND data_type='uuid' AND is_nullable='YES')", schema, table).Scan(&exists); err != nil || exists != (direction == "up") {
				t.Fatalf("%s %s task pointer=%v / %v", direction, table, exists, err)
			}
			var fks int
			if err := pool.QueryRow(ctx, "SELECT count(*) FROM pg_constraint WHERE conrelid=to_regclass($1) AND contype='f'", schema+"."+table).Scan(&fks); err != nil || fks != 0 {
				t.Fatalf("receipt foreign keys=%d / %v", fks, err)
			}
		}
	}
}
