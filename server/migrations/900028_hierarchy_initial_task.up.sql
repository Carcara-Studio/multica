-- NULL retains unverified historical registration, never evidence of no execution.
ALTER TABLE project_hierarchy_delegation ADD COLUMN initial_task_id uuid;
ALTER TABLE project_hierarchy_escalation ADD COLUMN initial_task_id uuid;
