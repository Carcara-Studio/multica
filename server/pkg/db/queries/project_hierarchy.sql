-- Project Hierarchy mutation primitives.
-- Every structural write is coordinated by LockProjectForHierarchyMutation in
-- the same transaction. Relationship integrity remains application-owned.

-- name: LockProjectForHierarchyMutation :one
SELECT id
FROM project
WHERE id = sqlc.arg('project_id')::uuid
  AND workspace_id = sqlc.arg('workspace_id')::uuid
FOR UPDATE;

-- name: ListProjectHierarchyMemberships :many
SELECT *
FROM project_agent_membership
WHERE project_id = sqlc.arg('project_id')::uuid
  AND workspace_id = sqlc.arg('workspace_id')::uuid
ORDER BY created_at ASC, id ASC;

-- name: CreateProjectHierarchyMembership :one
INSERT INTO project_agent_membership (
    id,
    workspace_id,
    project_id,
    agent_id,
    manager_membership_id,
    title
) VALUES (
    COALESCE(sqlc.narg('id')::uuid, gen_random_uuid()),
    sqlc.arg('workspace_id')::uuid,
    sqlc.arg('project_id')::uuid,
    sqlc.arg('agent_id')::uuid,
    sqlc.narg('manager_membership_id')::uuid,
    sqlc.arg('title')::text
)
RETURNING *;

-- name: ReactivateProjectHierarchyMembership :one
UPDATE project_agent_membership
SET manager_membership_id = sqlc.narg('manager_membership_id')::uuid,
    title = sqlc.arg('title')::text,
    archived_at = NULL,
    updated_at = now()
WHERE id = sqlc.arg('id')::uuid
  AND project_id = sqlc.arg('project_id')::uuid
  AND workspace_id = sqlc.arg('workspace_id')::uuid
  AND archived_at IS NOT NULL
RETURNING *;

-- name: UpdateProjectHierarchyMembershipManager :one
UPDATE project_agent_membership
SET manager_membership_id = sqlc.narg('manager_membership_id')::uuid,
    updated_at = now()
WHERE id = sqlc.arg('id')::uuid
  AND project_id = sqlc.arg('project_id')::uuid
  AND workspace_id = sqlc.arg('workspace_id')::uuid
  AND archived_at IS NULL
RETURNING *;

-- name: ArchiveProjectHierarchyMembership :one
UPDATE project_agent_membership
SET archived_at = now(),
    updated_at = now()
WHERE id = sqlc.arg('id')::uuid
  AND project_id = sqlc.arg('project_id')::uuid
  AND workspace_id = sqlc.arg('workspace_id')::uuid
  AND archived_at IS NULL
RETURNING *;
