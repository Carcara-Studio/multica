package main

// Keep fork-owned concurrent-index registrations isolated from the upstream
// migration registry while still wiring both registries that the migrator uses.
// init runs after package variables are initialized, so update preMigrationHooks
// explicitly as well as concurrentIndexCleanups; mutating only the cleanup map
// here would leave the already-built hook map without these migrations.
func init() {
	indexes := map[string]string{
		"574_project_agent_membership_id_idx":            "project_agent_membership_id_idx",
		"575_project_agent_membership_project_agent_idx": "project_agent_membership_project_agent_idx",
		"576_project_agent_membership_active_root_idx":    "project_agent_membership_active_root_idx",
		"577_project_agent_membership_manager_idx":        "project_agent_membership_manager_idx",
		"578_project_agent_membership_workspace_idx":      "project_agent_membership_workspace_idx",
	}
	for version, index := range indexes {
		concurrentIndexCleanups[version] = index
		preMigrationHooks[version] = cleanupInvalidConcurrentIndexHook(index)
	}
}
