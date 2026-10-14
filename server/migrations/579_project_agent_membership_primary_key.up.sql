-- Promote the concurrently-built unique id index to the table primary key.
-- The index is built in migration 574 so this ALTER does not perform a
-- non-concurrent index build.
ALTER TABLE project_agent_membership
    ADD CONSTRAINT project_agent_membership_pkey
    PRIMARY KEY USING INDEX project_agent_membership_id_idx;
