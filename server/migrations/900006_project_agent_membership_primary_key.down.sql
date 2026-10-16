-- Dropping the constraint drops the index it owns; migration 574 down then
-- becomes a safe no-op via IF EXISTS.
ALTER TABLE project_agent_membership
    DROP CONSTRAINT IF EXISTS project_agent_membership_pkey;
