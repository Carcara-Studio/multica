-- name: SetProjectContinuity :one
-- Caller holds the workspace membership fence and has resolved project access.
-- Clearing retains a new revision; an old create cannot revive removed text.
UPDATE project SET
    continuity_revision = @new_revision::uuid,
    continuity_content = sqlc.narg('content')::text,
    continuity_reviewed_by = @reviewed_by::uuid,
    continuity_reviewed_at = clock_timestamp()
WHERE id = @project_id::uuid AND workspace_id = @workspace_id::uuid
  AND continuity_revision IS NOT DISTINCT FROM sqlc.narg('expected_revision')::uuid
RETURNING *;
