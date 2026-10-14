-- name: DeleteProjectHierarchyMembershipsByProject :exec
-- Project deletion cleanup. workspace_id is a SQL-layer tenant guard; the
-- caller runs this inside the existing locked DeleteProject transaction.
DELETE FROM project_agent_membership
WHERE project_id = $1 AND workspace_id = $2;

-- name: DeleteProjectHierarchyMembershipsByWorkspace :exec
-- Workspace teardown cleanup. The caller holds the existing workspace delete
-- lock for the surrounding application-owned relationship graph transaction.
DELETE FROM project_agent_membership
WHERE workspace_id = $1;
