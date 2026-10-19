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
		"900008_agent_failover_state_agent_idx":                 "agent_failover_state_agent_idx",
		"900010_agent_failover_state_due_idx":                   "agent_failover_state_due_idx",
		"900011_agent_failover_state_lease_idx":                 "agent_failover_state_lease_idx",
		"900012_agent_failover_state_workspace_idx":             "agent_failover_state_workspace_idx",
		"900014_agent_fallback_config_agent_idx":                "agent_fallback_config_agent_idx",
		"900016_agent_fallback_config_workspace_idx":            "agent_fallback_config_workspace_idx",
		"900017_agent_fallback_config_runtime_idx":              "agent_fallback_config_runtime_idx",
		"900019_agent_task_execution_route_task_idx":            "agent_task_execution_route_task_idx",
		"900021_agent_task_execution_route_workspace_agent_idx": "agent_task_execution_route_workspace_agent_idx",
	} {
		concurrentIndexCleanups[version] = index
		preMigrationHooks[version] = cleanupInvalidConcurrentIndexHook(index)
	}
	upMigrationConditions["900008_agent_failover_state_agent_idx"] = whenFailoverPrimaryKeyMissing
	upMigrationConditions["900009_agent_failover_state_primary_key"] = whenFailoverPrimaryKeyMissing
	for _, version := range []string{"900014_agent_fallback_config_agent_idx", "900015_agent_fallback_config_primary_key"} {
		upMigrationConditions[version] = whenTargetPrimaryKeyMissing("agent_fallback_config", "agent_id")
	}
	for _, version := range []string{"900019_agent_task_execution_route_task_idx", "900020_agent_task_execution_route_primary_key"} {
		upMigrationConditions[version] = whenTargetPrimaryKeyMissing("agent_task_execution_route", "task_id")
	}
}

func whenFailoverPrimaryKeyMissing(ctx context.Context, conn *pgxpool.Conn) (bool, string, error) {
	return whenTargetPrimaryKeyMissing("agent_failover_state", "agent_id")(ctx, conn)
}

func whenTargetPrimaryKeyMissing(table, column string) func(context.Context, *pgxpool.Conn) (bool, string, error) {
	return func(ctx context.Context, conn *pgxpool.Conn) (bool, string, error) {
		var name, definition string
		err := conn.QueryRow(ctx, `SELECT conname, pg_get_constraintdef(oid)
FROM pg_constraint WHERE conrelid = to_regclass($1) AND contype = 'p'`, table).Scan(&name, &definition)
		if errors.Is(err, pgx.ErrNoRows) {
			return true, "", nil
		}
		if err != nil {
			return false, "", fmt.Errorf("inspect failover primary key: %w", err)
		}
		if name != table+"_pkey" || definition != "PRIMARY KEY ("+column+")" {
			return false, "", fmt.Errorf("unexpected failover primary key %q: %s", name, definition)
		}
		return false, "failover primary key already attached", nil
	}
}
