-- Optional execution target metadata only; no secrets, FKs or backfill.
CREATE TABLE IF NOT EXISTS agent_fallback_config (
    agent_id uuid NOT NULL,
    workspace_id uuid NOT NULL,
    runtime_id uuid NOT NULL,
    generation uuid NOT NULL,
    model text NOT NULL DEFAULT '',
    thinking_level text NOT NULL DEFAULT '',
    service_tier text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
