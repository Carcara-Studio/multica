CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS project_agent_membership_project_agent_idx ON project_agent_membership (project_id, agent_id);
