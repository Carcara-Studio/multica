-- Internal persistence only. Callers must authorize management, lock current
-- upstream owners, validate adapter capabilities, and compose WithTx writes.
-- These MVCC relationship checks alone do not fence concurrent owner teardown.

-- name: InitializeAgentFallbackConfig :one
INSERT INTO agent_fallback_config
    (agent_id, workspace_id, runtime_id, generation, model, thinking_level, service_tier, execution_config)
SELECT a.id, a.workspace_id, r.id, @generation, @model, @thinking_level, @service_tier, sqlc.narg(execution_config)::jsonb
FROM agent a JOIN agent_runtime r ON r.workspace_id = a.workspace_id
WHERE a.id = @agent_id AND a.workspace_id = @workspace_id
  AND a.kind = 'user' AND a.archived_at IS NULL AND r.id = @runtime_id
  AND (r.visibility = 'public' OR (r.visibility = 'private' AND r.owner_id = a.owner_id))
ON CONFLICT (agent_id) DO NOTHING
RETURNING *;

-- name: GetAgentFallbackConfig :one
SELECT * FROM agent_fallback_config WHERE agent_id = @agent_id AND workspace_id = @workspace_id;

-- name: UpdateAgentFallbackConfig :one
-- Existing config -> state lock order. Every edit rotates both epochs and
-- invalidates probe authority atomically, without trusting an exhausted primary.
WITH config AS MATERIALIZED (
    SELECT c.* FROM agent_fallback_config c
    WHERE c.agent_id = @agent_id AND c.workspace_id = @workspace_id
      AND c.generation = @expected_generation AND c.generation <> @new_generation
      AND EXISTS (SELECT 1 FROM agent a JOIN agent_runtime r ON r.workspace_id = a.workspace_id
          WHERE a.id = c.agent_id AND a.workspace_id = c.workspace_id AND a.kind = 'user'
            AND a.archived_at IS NULL AND r.id = @runtime_id
            AND (r.visibility = 'public' OR (r.visibility = 'private' AND r.owner_id = a.owner_id)))
    FOR UPDATE OF c
), availability AS MATERIALIZED (
    SELECT s.* FROM agent_failover_state s, config c
    WHERE s.agent_id = c.agent_id AND s.workspace_id = c.workspace_id
      AND s.primary_generation = @expected_primary_generation
      AND s.primary_generation <> @new_primary_generation AND s.version = @expected_version
    FOR UPDATE OF s
), stamp AS MATERIALIZED (SELECT clock_timestamp() AS at FROM availability), rotated AS (
    UPDATE agent_failover_state s
    SET primary_generation = @new_primary_generation, version = s.version + 1,
        state = CASE WHEN s.state = 'primary_probing' THEN 'fallback_active' ELSE s.state END,
        next_probe_at = CASE WHEN s.state <> 'primary_active' THEN stamp.at + interval '30 minutes' ELSE NULL END,
        probe_token = NULL, probe_runtime_id = NULL, probe_lease_until = NULL, probe_dispatched_at = NULL,
        updated_at = stamp.at
    FROM availability a, stamp WHERE s.agent_id = a.agent_id
    RETURNING s.agent_id
)
UPDATE agent_fallback_config c
SET runtime_id = @runtime_id, generation = @new_generation,
    model = @model, thinking_level = @thinking_level, service_tier = @service_tier,
    execution_config = sqlc.narg(execution_config)::jsonb,
    updated_at = stamp.at
FROM rotated, stamp WHERE c.agent_id = rotated.agent_id
RETURNING c.*;

-- name: DeleteAgentFallbackConfig :execrows
-- Removal suspends and rotates policy in the same statement. It neither
-- proves primary health nor erases queued route proof. Owner locks are external.
WITH config AS MATERIALIZED (
    SELECT c.* FROM agent_fallback_config c
    WHERE c.agent_id = @agent_id AND c.workspace_id = @workspace_id AND c.generation = @expected_generation
    FOR UPDATE
), availability AS MATERIALIZED (
    SELECT s.* FROM agent_failover_state s, config c
    WHERE s.agent_id = c.agent_id AND s.workspace_id = c.workspace_id
      AND s.primary_generation = @expected_primary_generation
      AND s.primary_generation <> @new_primary_generation AND s.version = @expected_version
    FOR UPDATE OF s
), stamp AS MATERIALIZED (SELECT clock_timestamp() AS at FROM availability), suspended AS (
    UPDATE agent_failover_state s
    SET primary_generation = @new_primary_generation, version = s.version + 1,
        suspended_at = stamp.at,
        state = CASE WHEN s.state = 'primary_probing' THEN 'fallback_active' ELSE s.state END,
        next_probe_at = CASE WHEN s.state = 'primary_probing' THEN stamp.at + interval '30 minutes' ELSE s.next_probe_at END,
        probe_token = NULL, probe_runtime_id = NULL, probe_lease_until = NULL, probe_dispatched_at = NULL,
        updated_at = stamp.at
    FROM availability a, stamp WHERE s.agent_id = a.agent_id
    RETURNING s.agent_id
)
DELETE FROM agent_fallback_config c USING suspended WHERE c.agent_id = suspended.agent_id;

-- name: RecordAgentTaskExecutionRoute :one
-- Called in the SAME transaction as task insertion. Acquire upstream owner
-- locks before entering: then config -> state, never state -> owner/chat/task.
-- Both epochs/version must come from enqueue selection, not failure time.
WITH config AS MATERIALIZED (
    SELECT c.* FROM agent_fallback_config c
    WHERE c.agent_id = @agent_id AND c.workspace_id = @workspace_id
      AND c.generation = @fallback_generation
    FOR SHARE
), availability AS MATERIALIZED (
    SELECT s.* FROM agent_failover_state s, config c
    WHERE s.agent_id = c.agent_id AND s.workspace_id = c.workspace_id
      AND s.primary_generation = @primary_generation AND s.version = @state_version
      AND s.suspended_at IS NULL
    FOR SHARE OF s
)
INSERT INTO agent_task_execution_route
    (task_id, agent_id, workspace_id, runtime_id, route, primary_generation, state_version, fallback_generation)
SELECT t.id, a.id, a.workspace_id, t.runtime_id, @route::text,
       s.primary_generation, s.version, c.generation
FROM config c, availability s, agent a, agent_task_queue t, agent_runtime r
WHERE a.id = c.agent_id AND a.workspace_id = c.workspace_id
  AND a.archived_at IS NULL AND a.kind = 'user'
  AND t.id = @task_id AND t.agent_id = a.id AND t.status IN ('queued', 'deferred')
  AND r.id = t.runtime_id AND r.workspace_id = a.workspace_id
  AND (r.visibility = 'public' OR (r.visibility = 'private' AND r.owner_id = a.owner_id))
  AND ((@route::text = 'primary' AND s.state = 'primary_active' AND t.runtime_id = a.runtime_id)
    OR (@route::text = 'fallback' AND s.state IN ('fallback_active', 'primary_probing') AND t.runtime_id = c.runtime_id))
ON CONFLICT (task_id) DO NOTHING
RETURNING *;

-- name: GetAgentTaskExecutionRoute :one
SELECT * FROM agent_task_execution_route WHERE task_id = @task_id AND workspace_id = @workspace_id;

-- name: CopyAgentTaskExecutionRouteForRetry :one
-- Retries inherit the parent's proof even after config removal/recovery. This
-- preserves lineage, not launch permission: claim must validate current epochs.
-- No parent marker means upstream behavior; callers must distinguish that from
-- a failed copy and roll back a routed child whose proof could not be inserted.
INSERT INTO agent_task_execution_route
    (task_id, agent_id, workspace_id, runtime_id, route, primary_generation, state_version, fallback_generation)
SELECT child.id, p.agent_id, p.workspace_id, p.runtime_id, p.route,
       p.primary_generation, p.state_version, p.fallback_generation
FROM agent_task_execution_route p
JOIN agent_task_queue parent ON parent.id = p.task_id AND parent.agent_id = p.agent_id AND parent.runtime_id = p.runtime_id
JOIN agent_task_queue child ON child.parent_task_id = parent.id AND child.retry_of_task_id = parent.id
JOIN agent a ON a.id = p.agent_id AND a.workspace_id = p.workspace_id
WHERE p.task_id = @parent_task_id AND p.workspace_id = @workspace_id AND parent.status = 'failed'
  AND child.id = @child_task_id AND child.agent_id = p.agent_id AND child.runtime_id = p.runtime_id
  AND child.status IN ('queued', 'deferred')
ON CONFLICT (task_id) DO NOTHING
RETURNING *;

-- name: DeleteAgentTaskExecutionRoutes :execrows
-- Terminal lifecycle cleanup only; never remove proof to reroute queued work.
DELETE FROM agent_task_execution_route WHERE workspace_id = @workspace_id AND agent_id = @agent_id;

-- name: TryLockExecutionTargetRuntimesForShare :many
-- Stabilize owner/visibility/provider metadata against updates and teardown,
-- while remaining compatible with upstream FK/task-owner KEY SHARE locks.
-- Caller owns Agent first. NOWAIT avoids a blocking Agent -> runtime edge
-- against runtime teardown/metadata writers that lock runtime before Agent.
SELECT id FROM agent_runtime
WHERE id = ANY(@runtime_ids::uuid[])
ORDER BY id
FOR SHARE NOWAIT;
