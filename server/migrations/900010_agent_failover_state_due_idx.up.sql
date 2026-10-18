CREATE INDEX CONCURRENTLY IF NOT EXISTS agent_failover_state_due_idx ON agent_failover_state (next_probe_at, agent_id) WHERE state = 'fallback_active' AND suspended_at IS NULL;
