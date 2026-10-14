CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS project_agent_membership_active_root_idx ON project_agent_membership (project_id) WHERE manager_membership_id IS NULL AND archived_at IS NULL;
