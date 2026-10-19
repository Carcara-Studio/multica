ALTER TABLE IF EXISTS agent_failover_state
DROP COLUMN IF EXISTS evidence_expires_at,
DROP COLUMN IF EXISTS evidence_issued_at,
DROP COLUMN IF EXISTS evidence_token,
DROP COLUMN IF EXISTS evidence_context;
