-- Native tasks/work survive downgrade; only the receipt pointers are removed.
ALTER TABLE project_hierarchy_escalation DROP COLUMN IF EXISTS initial_task_id;
ALTER TABLE project_hierarchy_delegation DROP COLUMN IF EXISTS initial_task_id;
