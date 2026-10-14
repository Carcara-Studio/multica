-- Project-scoped organizational membership for Agents.
-- Relationships are application-owned under ADR-014/current upstream policy:
-- no foreign keys or cascades. Cross-row/cross-table invariants are validated
-- by the hierarchy service and lifecycle cleanup paths in transactions.
CREATE TABLE IF NOT EXISTS project_agent_membership (
 id uuid NOT NULL DEFAULT gen_random_uuid(),
 workspace_id uuid NOT NULL,
 project_id uuid NOT NULL,
 agent_id uuid NOT NULL,
 manager_membership_id uuid,
 title text NOT NULL DEFAULT '',
 archived_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 CONSTRAINT project_agent_membership_manager_not_self
  CHECK (manager_membership_id IS NULL OR manager_membership_id <> id)
);
