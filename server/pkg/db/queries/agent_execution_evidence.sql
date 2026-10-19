-- Internal leaf persistence only. Caller holds current upstream owners/runtime
-- authorization before config -> state. No state -> owner/task lock acquisition.
-- Context is a server-built versioned non-secret binding, never a raw report.

-- name: IssueAgentExecutionChallenge :one
-- One slot: issuance supersedes all prior challenges without changing health,
-- probe lease, availability version or its timestamps. Sample time after locks.
WITH config AS MATERIALIZED (
    SELECT c.* FROM agent_fallback_config c
    WHERE c.agent_id = @agent_id AND c.workspace_id = @workspace_id
      AND c.generation = @fallback_generation
    FOR SHARE
), locked AS MATERIALIZED (
    SELECT s.* FROM agent_failover_state s, config c
    WHERE s.agent_id = c.agent_id AND s.workspace_id = c.workspace_id
      AND s.primary_generation = @primary_generation AND s.suspended_at IS NULL
    FOR UPDATE OF s
), stamp AS MATERIALIZED (SELECT clock_timestamp() AS at FROM locked)
UPDATE agent_failover_state s
SET evidence_token = gen_random_uuid(), evidence_context = @evidence_context::jsonb,
    evidence_issued_at = stamp.at, evidence_expires_at = stamp.at + interval '2 minutes'
FROM locked, config c, stamp, agent a, agent_runtime r
WHERE s.agent_id = locked.agent_id AND a.id = s.agent_id AND a.workspace_id = s.workspace_id
  AND a.kind = 'user' AND a.archived_at IS NULL AND a.owner_id = @agent_owner_id
  AND r.id = @runtime_id AND r.workspace_id = s.workspace_id
  AND r.owner_id = @runtime_owner_id AND r.provider = @provider
  AND r.profile_id IS NOT DISTINCT FROM sqlc.narg(runtime_profile_id)::uuid
  AND (r.visibility = 'public' OR (r.visibility = 'private' AND r.owner_id = a.owner_id))
  AND ((@route::text = 'primary' AND r.id = a.runtime_id AND @runtime_mode::text = a.runtime_mode)
    OR (@route::text = 'fallback' AND r.id = c.runtime_id AND @runtime_mode::text = r.runtime_mode))
RETURNING s.evidence_token, s.evidence_issued_at, s.evidence_expires_at;

-- name: ConsumeAgentExecutionChallenge :one
-- Matching current context and generations are necessary, not authorization.
-- Clear atomically before returning the old challenge. Only one consumer wins;
-- invalid capability reports burn their nonce without producing verified proof.
WITH config AS MATERIALIZED (
    SELECT c.* FROM agent_fallback_config c
    WHERE c.agent_id = @agent_id AND c.workspace_id = @workspace_id
      AND c.generation = @fallback_generation
    FOR SHARE
), locked AS MATERIALIZED (
    SELECT s.* FROM agent_failover_state s, config c
    WHERE s.agent_id = c.agent_id AND s.workspace_id = c.workspace_id
      AND s.primary_generation = @primary_generation AND s.suspended_at IS NULL
      AND s.evidence_token = @evidence_token
      AND s.evidence_context = @evidence_context::jsonb
    FOR UPDATE OF s
), stamp AS MATERIALIZED (SELECT clock_timestamp() AS at FROM locked), cleared AS (
    UPDATE agent_failover_state s
    SET evidence_token = NULL, evidence_context = NULL,
        evidence_issued_at = NULL, evidence_expires_at = NULL
    FROM locked, config c, stamp, agent a, agent_runtime r
    WHERE s.agent_id = locked.agent_id AND a.id = s.agent_id AND a.workspace_id = s.workspace_id
      AND locked.evidence_issued_at <= stamp.at AND locked.evidence_expires_at > stamp.at
      AND a.kind = 'user' AND a.archived_at IS NULL AND a.owner_id = @agent_owner_id
      AND r.id = @runtime_id AND r.workspace_id = s.workspace_id
      AND r.owner_id = @runtime_owner_id AND r.provider = @provider
      AND r.profile_id IS NOT DISTINCT FROM sqlc.narg(runtime_profile_id)::uuid
      AND (r.visibility = 'public' OR (r.visibility = 'private' AND r.owner_id = a.owner_id))
      AND ((@route::text = 'primary' AND r.id = a.runtime_id AND @runtime_mode::text = a.runtime_mode)
        OR (@route::text = 'fallback' AND r.id = c.runtime_id AND @runtime_mode::text = r.runtime_mode))
    RETURNING s.agent_id
)
SELECT locked.evidence_token, locked.evidence_issued_at, locked.evidence_expires_at,
       stamp.at::timestamptz AS evaluated_at
FROM cleared, locked, stamp WHERE cleared.agent_id = locked.agent_id;
