-- Durable request identity/lineage; execution and decision lifecycle stay native.
CREATE TABLE project_hierarchy_escalation (
 workspace_id uuid NOT NULL,
 source_issue_id uuid NOT NULL,
 source_agent_id uuid NOT NULL,
 request_id uuid NOT NULL,
 project_id uuid NOT NULL,
 source_membership_id uuid NOT NULL,
 manager_membership_id uuid,
 manager_agent_id uuid,
 human_intervention boolean NOT NULL,
 decision_issue_id uuid NOT NULL,
 source_task_id uuid NOT NULL,
 reason text NOT NULL,
 context text NOT NULL DEFAULT '',
 request_hash bytea NOT NULL CHECK (octet_length(request_hash)=32),
 created_at timestamptz NOT NULL DEFAULT now(),
 CHECK ((human_intervention AND manager_membership_id IS NULL AND manager_agent_id IS NULL)
 OR (NOT human_intervention AND manager_membership_id IS NOT NULL AND manager_agent_id IS NOT NULL))
);
