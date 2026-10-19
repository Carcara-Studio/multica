-- Immutable opt-in enqueue proof, including primary work (ADR-018).
CREATE TABLE IF NOT EXISTS agent_task_execution_route (
    task_id uuid NOT NULL,
    agent_id uuid NOT NULL,
    workspace_id uuid NOT NULL,
    runtime_id uuid NOT NULL,
    route text NOT NULL CHECK (route IN ('primary', 'fallback')),
    primary_generation uuid NOT NULL,
    state_version bigint NOT NULL CHECK (state_version > 0),
    fallback_generation uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
