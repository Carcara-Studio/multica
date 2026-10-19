-- These are persistence primitives, not authorization or target validation.
-- Bind them to the caller's transaction when composing configuration/lifecycle
-- writes. No ordinary task enqueue/settlement path calls them yet (ADR-017).

-- name: InitializeAgentFailoverState :one
INSERT INTO agent_failover_state (agent_id, workspace_id, primary_generation)
SELECT a.id, a.workspace_id, @primary_generation::uuid
FROM agent a
WHERE a.id = @agent_id AND a.workspace_id = @workspace_id AND a.archived_at IS NULL
ON CONFLICT (agent_id) DO NOTHING
RETURNING *;

-- name: GetAgentFailoverState :one
SELECT * FROM agent_failover_state
WHERE agent_id = @agent_id AND workspace_id = @workspace_id;

-- name: ActivateAgentFallbackState :one
-- Caller must prove reliable primary quota, compatible fallback and that the
-- failed task belongs to this generation. CAS forbids duplicate/stale signals.
WITH locked AS (
    SELECT s.* FROM agent_failover_state s
    WHERE s.agent_id = @agent_id AND s.workspace_id = @workspace_id
      AND s.primary_generation = @primary_generation AND s.version = @expected_version
      AND s.state = 'primary_active' AND s.suspended_at IS NULL
    FOR UPDATE
), stamp AS MATERIALIZED (SELECT clock_timestamp() AS at FROM locked), failure AS (
    SELECT t.id, t.completed_at FROM agent_task_queue t
    WHERE t.id = @failure_task_id AND t.agent_id = @agent_id
      AND t.status = 'failed' AND t.failure_reason = 'agent_error.provider_quota_limit'
      AND t.completed_at IS NOT NULL
)
UPDATE agent_failover_state f
SET state = 'fallback_active', version = f.version + 1,
    primary_failed_at = failure.completed_at, primary_failure_task_id = failure.id,
    next_probe_at = failure.completed_at + interval '30 minutes',
    recovered_at = NULL, updated_at = stamp.at
FROM locked, stamp, failure WHERE f.agent_id = locked.agent_id AND failure.completed_at <= stamp.at
  AND EXISTS (SELECT 1 FROM agent a WHERE a.id = f.agent_id
      AND a.workspace_id = f.workspace_id AND a.archived_at IS NULL)
RETURNING f.*;

-- name: ClaimDueAgentPrimaryProbe :one
-- Claim only for an authenticated, probe-capable primary runtime. Tokens and
-- schedule are durable; no user task or model-list result is a recovery proof.
WITH candidate AS (
    SELECT f.agent_id
    FROM agent_failover_state f
    JOIN agent a ON a.id = f.agent_id AND a.workspace_id = f.workspace_id
    WHERE f.workspace_id = @workspace_id AND a.runtime_id = @primary_runtime_id
      AND a.archived_at IS NULL AND f.suspended_at IS NULL
      AND f.state = 'fallback_active' AND f.next_probe_at <= clock_timestamp()
    ORDER BY f.next_probe_at, f.agent_id
    LIMIT 1 FOR UPDATE OF f SKIP LOCKED
), stamp AS MATERIALIZED (SELECT clock_timestamp() AS at FROM candidate)
UPDATE agent_failover_state f
SET state = 'primary_probing', version = version + 1,
    probe_token = gen_random_uuid(), probe_runtime_id = @primary_runtime_id,
    probe_lease_until = stamp.at + interval '2 minutes',
    last_probe_at = stamp.at, next_probe_at = NULL, probe_dispatched_at = NULL,
    updated_at = stamp.at
FROM candidate, stamp
WHERE f.agent_id = candidate.agent_id
RETURNING f.*;

-- name: MarkAgentPrimaryProbeDispatched :one
WITH locked AS (
    SELECT s.* FROM agent_failover_state s
    WHERE s.agent_id = @agent_id AND s.workspace_id = @workspace_id
      AND s.primary_generation = @primary_generation AND s.probe_token = @probe_token
      AND s.probe_runtime_id = @primary_runtime_id
      AND s.state = 'primary_probing' AND s.suspended_at IS NULL AND s.probe_dispatched_at IS NULL
    FOR UPDATE
), stamp AS MATERIALIZED (SELECT clock_timestamp() AS at FROM locked)
UPDATE agent_failover_state f
SET probe_dispatched_at = stamp.at, version = f.version + 1, updated_at = stamp.at
FROM locked, stamp
WHERE f.agent_id = locked.agent_id AND locked.probe_lease_until > stamp.at
  AND EXISTS (SELECT 1 FROM agent a WHERE a.id = f.agent_id
      AND a.workspace_id = f.workspace_id AND a.runtime_id = @primary_runtime_id
      AND a.archived_at IS NULL)
RETURNING f.*;

-- name: SettleAgentPrimaryProbe :one
-- Exact success only; unknown and canonical provider errors retain fallback.
-- No result may settle before dispatch or after lease expiry.
WITH locked AS (
    SELECT s.* FROM agent_failover_state s
    WHERE s.agent_id = @agent_id AND s.workspace_id = @workspace_id
      AND s.primary_generation = @primary_generation AND s.probe_token = @probe_token
      AND s.probe_runtime_id = @primary_runtime_id
      AND s.state = 'primary_probing' AND s.suspended_at IS NULL AND s.probe_dispatched_at IS NOT NULL
    FOR UPDATE
), stamp AS MATERIALIZED (SELECT clock_timestamp() AS at FROM locked)
UPDATE agent_failover_state f
SET state = CASE WHEN @outcome::text = 'success' THEN 'primary_active' ELSE 'fallback_active' END,
    version = f.version + 1,
    next_probe_at = CASE WHEN @outcome::text = 'success' THEN NULL ELSE stamp.at + interval '30 minutes' END,
    recovered_at = CASE WHEN @outcome::text = 'success' THEN stamp.at ELSE f.recovered_at END,
    last_probe_reason = @outcome::text,
    probe_token = NULL, probe_runtime_id = NULL, probe_lease_until = NULL, probe_dispatched_at = NULL,
    updated_at = stamp.at
FROM locked, stamp
WHERE f.agent_id = locked.agent_id AND locked.probe_lease_until > stamp.at
  AND EXISTS (SELECT 1 FROM agent a WHERE a.id = f.agent_id
      AND a.workspace_id = f.workspace_id AND a.runtime_id = @primary_runtime_id
      AND a.archived_at IS NULL)
RETURNING f.*;

-- name: ExpireAgentPrimaryProbeLeases :many
WITH expired AS (
    SELECT agent_id FROM agent_failover_state
    WHERE state = 'primary_probing' AND probe_lease_until <= clock_timestamp()
    ORDER BY probe_lease_until, agent_id
    LIMIT 100 FOR UPDATE SKIP LOCKED
), stamp AS MATERIALIZED (SELECT clock_timestamp() AS at FROM expired LIMIT 1)
UPDATE agent_failover_state f
SET state = 'fallback_active', version = version + 1,
    next_probe_at = stamp.at + interval '30 minutes', last_probe_reason = 'probe_timeout',
    probe_token = NULL, probe_runtime_id = NULL, probe_lease_until = NULL, probe_dispatched_at = NULL,
    updated_at = stamp.at
FROM expired, stamp WHERE f.agent_id = expired.agent_id
RETURNING f.*;

-- name: SuspendAgentFailoverState :one
UPDATE agent_failover_state f
SET suspended_at = clock_timestamp(), version = version + 1,
    state = CASE WHEN state = 'primary_probing' THEN 'fallback_active' ELSE state END,
    next_probe_at = CASE WHEN state = 'primary_probing' THEN clock_timestamp() + interval '30 minutes' ELSE next_probe_at END,
    probe_token = NULL, probe_runtime_id = NULL, probe_lease_until = NULL, probe_dispatched_at = NULL,
    updated_at = clock_timestamp()
WHERE f.agent_id = @agent_id AND f.workspace_id = @workspace_id AND f.suspended_at IS NULL
RETURNING f.*;

-- name: RotateAgentFailoverGeneration :one
-- Configuration change invalidates claims but does not implicitly trust primary.
WITH locked AS (
    SELECT s.* FROM agent_failover_state s
    WHERE s.agent_id = @agent_id AND s.workspace_id = @workspace_id
      AND s.primary_generation = @expected_generation AND s.primary_generation <> @new_generation
    FOR UPDATE
), stamp AS MATERIALIZED (SELECT clock_timestamp() AS at FROM locked)
UPDATE agent_failover_state f
SET primary_generation = @new_generation, version = f.version + 1,
    state = CASE WHEN f.state = 'primary_probing' THEN 'fallback_active' ELSE f.state END,
    next_probe_at = CASE WHEN f.state <> 'primary_active' THEN stamp.at + interval '30 minutes' ELSE NULL END,
    probe_token = NULL, probe_runtime_id = NULL, probe_lease_until = NULL, probe_dispatched_at = NULL,
    updated_at = stamp.at
FROM locked, stamp WHERE f.agent_id = locked.agent_id
RETURNING f.*;

-- name: DeleteAgentFailoverState :execrows
-- Terminal Agent deletion only; deleting state is not an availability reset.
DELETE FROM agent_failover_state WHERE agent_id = @agent_id AND workspace_id = @workspace_id;

-- name: DeleteWorkspaceFailoverState :execrows
DELETE FROM agent_failover_state WHERE workspace_id = @workspace_id;
