package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func init() {
	for version, index := range map[string]string{
		"900008_agent_failover_state_agent_idx":     "agent_failover_state_agent_idx",
		"900010_agent_failover_state_due_idx":       "agent_failover_state_due_idx",
		"900011_agent_failover_state_lease_idx":     "agent_failover_state_lease_idx",
		"900012_agent_failover_state_workspace_idx": "agent_failover_state_workspace_idx",
	} {
		concurrentIndexCleanups[version] = index
		preMigrationHooks[version] = cleanupInvalidConcurrentIndexHook(index)
	}
	upMigrationConditions["900008_agent_failover_state_agent_idx"] = whenFailoverPrimaryKeyMissing
	upMigrationConditions["900009_agent_failover_state_primary_key"] = whenFailoverPrimaryKeyMissing
}

func whenFailoverPrimaryKeyMissing(ctx context.Context, conn *pgxpool.Conn) (bool, string, error) {
	var name, definition string
	err := conn.QueryRow(ctx, `SELECT conname, pg_get_constraintdef(oid)
FROM pg_constraint WHERE conrelid = to_regclass('agent_failover_state') AND contype = 'p'`).Scan(&name, &definition)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, "", nil
	}
	if err != nil {
		return false, "", fmt.Errorf("inspect failover primary key: %w", err)
	}
	if name != "agent_failover_state_pkey" || definition != "PRIMARY KEY (agent_id)" {
		return false, "", fmt.Errorf("unexpected failover primary key %q: %s", name, definition)
	}
	return false, "failover primary key already attached", nil
}
