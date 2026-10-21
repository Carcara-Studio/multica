import { z } from "zod";

// Read this receipt through the boundary parser before using its Issue identity.
export const hierarchyDelegationResponseSchema = z.object({
  issue_id: z.string().uuid(),
  replayed: z.boolean().default(false),
});

export type HierarchyDelegationResponse = z.infer<
  typeof hierarchyDelegationResponseSchema
>;
