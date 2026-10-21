import { z } from "zod";

export const hierarchyEscalationResponseSchema = z.object({
  issue_id: z.string().uuid(),
  replayed: z.boolean(),
});
export const hierarchyEscalationPageSchema = z.object({
  items: z.array(z.object({
    request_id: z.string().uuid(),
    source_issue_id: z.string().uuid(),
    source_agent_name: z.string().default(""),
    manager_agent_name: z.string().default(""),
    source_agent_id: z.string().uuid(),
    source_task_id: z.string().uuid(),
    manager_agent_id: z.string().uuid().nullable(),
    human_intervention: z.boolean(),
    decision_issue_id: z.string().uuid(),
    decision_status: z.string().nullable(),
    reason: z.string(),
    context: z.string(),
    repair: z.string(),
  })),
  next_cursor: z.string().uuid().nullable(),
});
export type HierarchyEscalationPage = z.infer<typeof hierarchyEscalationPageSchema>;
