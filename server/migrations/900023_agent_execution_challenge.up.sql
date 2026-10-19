-- One bounded, non-secret pending evidence challenge per optional policy.
-- No new relationships, indexes, backfill or automatic execution.
ALTER TABLE IF EXISTS agent_failover_state
ADD COLUMN IF NOT EXISTS evidence_context jsonb,
ADD COLUMN IF NOT EXISTS evidence_token uuid,
ADD COLUMN IF NOT EXISTS evidence_issued_at timestamptz,
ADD COLUMN IF NOT EXISTS evidence_expires_at timestamptz
CHECK (
    (evidence_context IS NULL AND evidence_token IS NULL
        AND evidence_issued_at IS NULL AND evidence_expires_at IS NULL)
    OR (evidence_context IS NOT NULL AND evidence_token IS NOT NULL
        AND evidence_issued_at IS NOT NULL AND evidence_expires_at IS NOT NULL
        AND jsonb_typeof(evidence_context) = 'object'
        AND evidence_context ? 'version' AND evidence_context->'version' = '1'::jsonb
        AND octet_length(evidence_context::text) <= 16384
        AND evidence_token <> '00000000-0000-0000-0000-000000000000'::uuid
        AND evidence_expires_at > evidence_issued_at
        AND evidence_expires_at <= evidence_issued_at + interval '2 minutes')
);
