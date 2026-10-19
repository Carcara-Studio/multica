-- Optional protected execution document. NULL remains metadata-only; no backfill.
ALTER TABLE IF EXISTS agent_fallback_config
ADD COLUMN IF NOT EXISTS execution_config jsonb
CHECK (execution_config IS NULL OR (
    jsonb_typeof(execution_config) = 'object'
    AND execution_config ? 'version'
    AND execution_config->'version' = '1'::jsonb
));
