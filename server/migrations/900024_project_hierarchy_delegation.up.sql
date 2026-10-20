-- Durable operation identity and lineage only; execution remains native Issue/task.
CREATE TABLE project_hierarchy_delegation (
    workspace_id uuid NOT NULL,
    parent_issue_id uuid NOT NULL,
    delegator_agent_id uuid NOT NULL,
    request_id uuid NOT NULL,
    project_id uuid NOT NULL,
    target_agent_id uuid NOT NULL,
    child_issue_id uuid NOT NULL,
    source_task_id uuid NOT NULL,
    request_hash bytea NOT NULL CHECK (octet_length(request_hash) = 32),
    created_at timestamptz NOT NULL DEFAULT now()
);
