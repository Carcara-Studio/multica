import { z } from "zod";

const uuid = z.string().uuid().refine((value) => value !== "00000000-0000-0000-0000-000000000000", "Nonzero UUID required");

export const hierarchyEscalationResponseSchema = z.object({
  issue_id: uuid,
  replayed: z.boolean(),
});
export const hierarchyEscalationPageSchema = z.object({
  items: z.array(z.object({
    request_id: uuid,
    source_issue_id: uuid,
    source_agent_name: z.string().default(""),
    manager_agent_name: z.string().default(""),
    source_agent_id: uuid,
    source_task_id: uuid,
    manager_agent_id: uuid.nullable(),
    human_intervention: z.boolean(),
    decision_issue_id: uuid,
    decision_status: z.string().nullable(),
    reason: z.string(),
    context: z.string(),
    repair: z.string(),
  }).refine((item) => item.human_intervention === (item.manager_agent_id === null), "Escalation route is inconsistent")),
  next_cursor: uuid.nullable(),
});
export type HierarchyEscalationPage = z.infer<typeof hierarchyEscalationPageSchema>;
