CREATE INDEX CONCURRENTLY IF NOT EXISTS agent_failover_state_lease_idx ON agent_failover_state (probe_lease_until, agent_id) WHERE state = 'primary_probing';
