package main

import (
	"context"
	"math/rand/v2"
	"slices"
	"testing"
	"time"
)

func TestFailoverMigrationsUpDownUp(t *testing.T) {
	base := openTestPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	schema := createScratchSchema(t, ctx, base, "failover_")
	pool := openTestPoolWithSearchPath(t, schema)
	versions := []string{
		"900007_agent_failover_state", "900008_agent_failover_state_agent_idx",
		"900009_agent_failover_state_primary_key", "900010_agent_failover_state_due_idx",
		"900011_agent_failover_state_lease_idx", "900012_agent_failover_state_workspace_idx",
	}
	run := func(direction string, selected []string) {
		t.Helper()
		if err := runMigrations(ctx, pool, runOptions{
			Direction: direction, Files: realMigrationFiles(t, selected, direction),
			SchemaMigrationsTable: schema + ".schema_migrations",
			AdvisoryLockKey:       int64(rand.Uint64()&0x7fffffffffffffff) | 1,
			Hooks:                 hooksForDirection(direction), Conditions: conditionsForDirection(direction),
		}); err != nil {
			t.Fatalf("migrate %s: %v", direction, err)
		}
	}
	for _, direction := range []string{"up", "down", "up"} {
		selected := slices.Clone(versions)
		if direction == "down" {
			slices.Reverse(selected)
		}
		run(direction, selected)
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, schema+".agent_failover_state").Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if exists != (direction == "up") {
			t.Fatalf("after %s table exists = %v", direction, exists)
		}
		if !exists {
			continue
		}
		var pk string
		if err := pool.QueryRow(ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint
WHERE conrelid = 'agent_failover_state'::regclass AND contype = 'p'`).Scan(&pk); err != nil {
			t.Fatal(err)
		}
		if pk != "PRIMARY KEY (agent_id)" {
			t.Fatalf("primary key = %q", pk)
		}
		var foreignKeys, invalidIndexes, indexCount, rowCount int
		if err := pool.QueryRow(ctx, `SELECT
(SELECT count(*) FROM pg_constraint WHERE conrelid='agent_failover_state'::regclass AND contype='f'),
(SELECT count(*) FROM pg_index WHERE indrelid='agent_failover_state'::regclass AND NOT indisvalid),
(SELECT count(*) FROM pg_index WHERE indrelid='agent_failover_state'::regclass),
(SELECT count(*) FROM agent_failover_state)`).Scan(&foreignKeys, &invalidIndexes, &indexCount, &rowCount); err != nil {
			t.Fatal(err)
		}
		if foreignKeys != 0 || invalidIndexes != 0 || indexCount != 4 || rowCount != 0 {
			t.Fatalf("FKs/invalid indexes/indexes/backfill = %d/%d/%d/%d", foreignKeys, invalidIndexes, indexCount, rowCount)
		}
		// Simulate a crash after attaching the key but before writing its ledger.
		if _, err := pool.Exec(ctx, `DELETE FROM schema_migrations WHERE version IN
('900008_agent_failover_state_agent_idx','900009_agent_failover_state_primary_key')`); err != nil {
			t.Fatal(err)
		}
		run("up", versions[1:3])
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_index WHERE indrelid='agent_failover_state'::regclass`).Scan(&indexCount); err != nil || indexCount != 4 {
			t.Fatalf("key recovery produced extra indexes: count=%d err=%v", indexCount, err)
		}
		// No owner tables exist in this isolated schema: no FK is intentionally required.
		if _, err := pool.Exec(ctx, `INSERT INTO agent_failover_state(agent_id,workspace_id,primary_generation)
VALUES ('00000000-0000-0000-0000-000000000001','00000000-0000-0000-0000-000000000002','00000000-0000-0000-0000-000000000003')`); err != nil {
			t.Fatal(err)
		}
		for _, sql := range []string{
			`UPDATE agent_failover_state SET state='invalid'`,
			`UPDATE agent_failover_state SET version=0`,
			`UPDATE agent_failover_state SET state='fallback_active'`,
			`UPDATE agent_failover_state SET state='primary_probing'`,
			`UPDATE agent_failover_state SET probe_token=gen_random_uuid()`,
		} {
			_, err := pool.Exec(ctx, sql)
			assertPostgresCode(t, err, "23514", sql)
		}
		if _, err := pool.Exec(ctx, `DELETE FROM agent_failover_state`); err != nil {
			t.Fatal(err)
		}
	}
}
