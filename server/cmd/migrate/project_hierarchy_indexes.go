package main

// Keep fork-owned concurrent-index registrations isolated from the upstream
// migration registry. The shared migrator/test machinery still consumes the
// same map, so interrupted CREATE INDEX CONCURRENTLY builds are repaired using
// the normal Multica invalid-index cleanup path.
func init() {
	concurrentIndexCleanups["574_project_agent_membership_id_idx"] = "project_agent_membership_id_idx"
	concurrentIndexCleanups["575_project_agent_membership_project_agent_idx"] = "project_agent_membership_project_agent_idx"
	concurrentIndexCleanups["576_project_agent_membership_active_root_idx"] = "project_agent_membership_active_root_idx"
	concurrentIndexCleanups["577_project_agent_membership_manager_idx"] = "project_agent_membership_manager_idx"
	concurrentIndexCleanups["578_project_agent_membership_workspace_idx"] = "project_agent_membership_workspace_idx"
}
