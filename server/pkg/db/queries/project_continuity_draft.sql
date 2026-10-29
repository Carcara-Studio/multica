-- name: GetLatestProjectContinuityDraftSource :one
-- Read-only source selection. A mutable issue.project_id alone cannot prove
-- where historical work ran. Old/missing scope and squad-bound work fail closed.
SELECT t.id AS task_id, t.issue_id, t.result, t.completed_at,
       i.revision AS issue_revision, i.status AS issue_status
FROM agent_task_queue t
JOIN issue i ON i.id = t.issue_id
JOIN agent a ON a.id = t.agent_id
WHERE i.project_id = @project_id::uuid AND i.workspace_id = @workspace_id::uuid
  AND a.workspace_id = @workspace_id::uuid
  AND t.status = 'completed' AND t.completed_at IS NOT NULL
  AND t.squad_id IS NULL
  AND t.issue_snapshot->>'v' = '1'
  AND t.issue_snapshot->'continuity_scope'->>'project_id' = (@project_id::uuid)::text
  AND t.issue_snapshot->'continuity_scope'->>'squad_id' = ''
ORDER BY t.completed_at DESC, t.id DESC
LIMIT 1;
