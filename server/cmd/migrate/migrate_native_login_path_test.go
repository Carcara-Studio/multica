package main

import (
	"math/rand/v2"
	"testing"
)

func TestNativeLoginPathMigrationDefaultsAndGuardedDowngrade(t *testing.T) {
	base := openTestPool(t)
	schema := createScratchSchema(t, t.Context(), base, "native_path_")
	pool := openTestPoolWithSearchPath(t, schema)
	const version = "900029_fallback_native_login_path"
	run := func(direction string, versions ...string) error {
		return runMigrations(t.Context(), pool, runOptions{
			Direction: direction, Files: realMigrationFiles(t, versions, direction),
			SchemaMigrationsTable: schema + ".schema_migrations",
			AdvisoryLockKey:       int64(rand.Uint64()&0x7fffffffffffffff) | 1,
			Hooks:                 hooksForDirection(direction), Conditions: conditionsForDirection(direction),
		})
	}
	if err := run("up", "900013_agent_fallback_config", "900022_agent_fallback_execution_config"); err != nil {
		t.Fatal(err)
	}
	var id, generation string
	if err := pool.QueryRow(t.Context(), `INSERT INTO agent_fallback_config(agent_id,workspace_id,runtime_id,generation) SELECT gen_random_uuid(),gen_random_uuid(),gen_random_uuid(),gen_random_uuid() RETURNING agent_id::text,generation::text`).Scan(&id, &generation); err != nil {
		t.Fatal(err)
	}
	if err := run("up", version); err != nil {
		t.Fatal(err)
	}
	var native bool
	var savedGeneration string
	if err := pool.QueryRow(t.Context(), `SELECT native_login_path,generation::text FROM agent_fallback_config WHERE agent_id=$1`, id).Scan(&native, &savedGeneration); err != nil || native || savedGeneration != generation {
		t.Fatal("migration reinterpreted old policy", err)
	}
	if _, err := pool.Exec(t.Context(), `UPDATE agent_fallback_config SET native_login_path=true WHERE agent_id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if err := run("down", version); err == nil {
		t.Fatal("downgrade silently converted native configuration to strict")
	}
	var recorded bool
	if err := pool.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, version).Scan(&recorded); err != nil || !recorded {
		t.Fatal("rejected downgrade lost migration ledger", err)
	}
	if err := pool.QueryRow(t.Context(), `SELECT native_login_path FROM agent_fallback_config WHERE agent_id=$1`, id).Scan(&native); err != nil || !native {
		t.Fatal("rejected downgrade lost mode", err)
	}
	// Fixture-only change. Protected mode reversion/generation rotation belongs
	// to ConfigManager tests; the migration must not perform it implicitly.
	if _, err := pool.Exec(t.Context(), `UPDATE agent_fallback_config SET native_login_path=false WHERE agent_id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if err := run("down", version); err != nil {
		t.Fatal(err)
	}
	if err := run("up", version); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(t.Context(), `SELECT native_login_path FROM agent_fallback_config WHERE agent_id=$1`, id).Scan(&native); err != nil || native {
		t.Fatal("up/down/up acquired native intent", err)
	}
}
