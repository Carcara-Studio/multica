package main

import (
	"context"
	"math/rand/v2"
	"slices"
	"testing"
	"time"
)

func TestExecutionTargetMigrationsUpDownUp(t *testing.T) {
	base := openTestPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	schema := createScratchSchema(t, ctx, base, "target_")
	pool := openTestPoolWithSearchPath(t, schema)
	versions := []string{
		"900013_agent_fallback_config", "900014_agent_fallback_config_agent_idx",
		"900015_agent_fallback_config_primary_key", "900016_agent_fallback_config_workspace_idx",
		"900017_agent_fallback_config_runtime_idx", "900018_agent_task_execution_route",
		"900019_agent_task_execution_route_task_idx", "900020_agent_task_execution_route_primary_key",
		"900021_agent_task_execution_route_workspace_agent_idx",
	}
	run := func(direction string, selected []string) {
		t.Helper()
		if err := runMigrations(ctx, pool, runOptions{
			Direction: direction, Files: realMigrationFiles(t, selected, direction),
			SchemaMigrationsTable: schema + ".schema_migrations",
			AdvisoryLockKey:       int64(rand.Uint64()&0x7fffffffffffffff) | 1,
			Hooks:                 hooksForDirection(direction), Conditions: conditionsForDirection(direction),
		}); err != nil {
			t.Fatal(err)
		}
	}
	for _, direction := range []string{"up", "down", "up"} {
		selected := slices.Clone(versions)
		if direction == "down" {
			slices.Reverse(selected)
		}
		run(direction, selected)
		for _, tc := range []struct {
			table, key string
			indexes    int
			recovery   []string
		}{
			{"agent_fallback_config", "agent_id", 3, versions[1:3]},
			{"agent_task_execution_route", "task_id", 2, versions[6:8]},
		} {
			var exists bool
			if err := pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, schema+"."+tc.table).Scan(&exists); err != nil {
				t.Fatal(err)
			}
			if exists != (direction == "up") {
				t.Fatalf("%s after %s exists=%v", tc.table, direction, exists)
			}
			if !exists {
				continue
			}
			var pk string
			var fks, invalid, indexes int
			if err := pool.QueryRow(ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid=to_regclass($1) AND contype='p'`, tc.table).Scan(&pk); err != nil {
				t.Fatal(err)
			}
			if pk != "PRIMARY KEY ("+tc.key+")" {
				t.Fatalf("key=%s", pk)
			}
			if err := pool.QueryRow(ctx, `SELECT
                (SELECT count(*) FROM pg_constraint WHERE conrelid=to_regclass($1) AND contype='f'),
                (SELECT count(*) FROM pg_index WHERE indrelid=to_regclass($1) AND NOT indisvalid),
                (SELECT count(*) FROM pg_index WHERE indrelid=to_regclass($1))`, tc.table).Scan(&fks, &invalid, &indexes); err != nil {
				t.Fatal(err)
			}
			if fks != 0 || invalid != 0 || indexes != tc.indexes {
				t.Fatalf("%s FK/invalid/index=%d/%d/%d", tc.table, fks, invalid, indexes)
			}
			var rows int
			if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+tc.table).Scan(&rows); err != nil || rows != 0 {
				t.Fatalf("backfill=%d err=%v", rows, err)
			}
			if _, err := pool.Exec(ctx, `DELETE FROM schema_migrations WHERE version=ANY($1::text[])`, tc.recovery); err != nil {
				t.Fatal(err)
			}
			run("up", tc.recovery)
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_index WHERE indrelid=to_regclass($1)`, tc.table).Scan(&indexes); err != nil || indexes != tc.indexes {
				t.Fatalf("recovery index=%d err=%v", indexes, err)
			}
		}
	}
	for _, sql := range []string{
		`INSERT INTO agent_task_execution_route(task_id,agent_id,workspace_id,runtime_id,route,primary_generation,state_version,fallback_generation) SELECT gen_random_uuid(),gen_random_uuid(),gen_random_uuid(),gen_random_uuid(),'unknown',gen_random_uuid(),1,gen_random_uuid()`,
		`INSERT INTO agent_task_execution_route(task_id,agent_id,workspace_id,runtime_id,route,primary_generation,state_version,fallback_generation) SELECT gen_random_uuid(),gen_random_uuid(),gen_random_uuid(),gen_random_uuid(),'primary',gen_random_uuid(),0,gen_random_uuid()`,
	} {
		_, err := pool.Exec(context.Background(), sql)
		assertPostgresCode(t, err, "23514", sql)
	}
}
