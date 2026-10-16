package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Keep fork-owned concurrent-index registrations isolated from the upstream
// migration registry while still wiring both registries that the migrator uses.
// The fork uses a reserved high-number migration band so upstream can continue
// allocating its normal sequential prefixes without repeated filename collisions.
func init() {
	indexes := map[string]string{
		"900001_project_agent_membership_id_idx":            "project_agent_membership_id_idx",
		"900002_project_agent_membership_project_agent_idx": "project_agent_membership_project_agent_idx",
		"900003_project_agent_membership_active_root_idx":   "project_agent_membership_active_root_idx",
		"900004_project_agent_membership_manager_idx":       "project_agent_membership_manager_idx",
		"900005_project_agent_membership_workspace_idx":     "project_agent_membership_workspace_idx",
	}
	for version, index := range indexes {
		concurrentIndexCleanups[version] = index
		preMigrationHooks[version] = cleanupInvalidConcurrentIndexHook(index)
	}

	// Installations that already applied the hierarchy under its original
	// 573-579 fork-only filenames already have the primary key. The renamed
	// 900001/900006 steps must then be ledger-convergence no-ops instead of
	// building a redundant unique index or attempting a second primary key.
	upMigrationConditions["900001_project_agent_membership_id_idx"] = whenProjectHierarchyPrimaryKeyMissing
	upMigrationConditions["900006_project_agent_membership_primary_key"] = whenProjectHierarchyPrimaryKeyMissing
}

func whenProjectHierarchyPrimaryKeyMissing(ctx context.Context, conn *pgxpool.Conn) (bool, string, error) {
	var name, definition string
	err := conn.QueryRow(ctx, `
SELECT c.conname, pg_get_constraintdef(c.oid)
FROM pg_constraint AS c
WHERE c.conrelid = to_regclass('project_agent_membership')
  AND c.contype = 'p'
`).Scan(&name, &definition)
	if errors.Is(err, pgx.ErrNoRows) {
		// No table or no primary key yet. In both cases the migration should
		// apply normally; to_regclass(NULL) simply matches no constraint row.
		return true, "", nil
	}
	if err != nil {
		return false, "", fmt.Errorf("inspect project hierarchy primary key: %w", err)
	}
	if name != "project_agent_membership_pkey" || definition != "PRIMARY KEY (id)" {
		return false, "", fmt.Errorf("unexpected project hierarchy primary key %q: %s", name, definition)
	}
	return false, "project hierarchy primary key already present from legacy fork migration numbering", nil
}
