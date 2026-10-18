-- Fork-only control-plane state (ADR-009/011/017). Relationships are
-- application-owned: no foreign keys, cascades, Agent backfill or task writes.
CREATE TABLE IF NOT EXISTS agent_failover_state (
    agent_id uuid NOT NULL,
    workspace_id uuid NOT NULL,
    primary_generation uuid NOT NULL,
    state text NOT NULL DEFAULT 'primary_active',
    version bigint NOT NULL DEFAULT 1,
    primary_failed_at timestamptz,
    primary_failure_task_id uuid,
    next_probe_at timestamptz,
    last_probe_at timestamptz,
    last_probe_reason text,
    recovered_at timestamptz,
    probe_token uuid,
    probe_runtime_id uuid,
    probe_lease_until timestamptz,
    probe_dispatched_at timestamptz,
    suspended_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT agent_failover_state_state_check
        CHECK (state IN ('primary_active', 'fallback_active', 'primary_probing')),
    CONSTRAINT agent_failover_state_version_check CHECK (version > 0),
    CONSTRAINT agent_failover_state_schedule_check CHECK (
        (state = 'primary_active' AND next_probe_at IS NULL)
        OR (state = 'fallback_active' AND next_probe_at IS NOT NULL)
        OR (state = 'primary_probing' AND next_probe_at IS NULL)
    ),
    CONSTRAINT agent_failover_state_claim_check CHECK (
        (state = 'primary_probing' AND suspended_at IS NULL
            AND probe_token IS NOT NULL AND probe_runtime_id IS NOT NULL AND probe_lease_until IS NOT NULL
            AND last_probe_at IS NOT NULL AND probe_lease_until > last_probe_at)
        OR (state <> 'primary_probing' AND probe_token IS NULL AND probe_runtime_id IS NULL
            AND probe_lease_until IS NULL AND probe_dispatched_at IS NULL)
    ),
    CONSTRAINT agent_failover_state_dispatch_check CHECK (
        probe_dispatched_at IS NULL OR
        (probe_dispatched_at >= last_probe_at AND probe_dispatched_at < probe_lease_until)
    )
);
