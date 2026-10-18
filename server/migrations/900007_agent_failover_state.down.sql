-- Rollback is safe only before failover integration is enabled, or after
-- explicitly disabling it and draining routed work. Never use this as recovery.
DROP TABLE IF EXISTS agent_failover_state;
