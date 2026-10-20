CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS project_hierarchy_delegation_identity_idx ON project_hierarchy_delegation (workspace_id, parent_issue_id, delegator_agent_id, request_id);
