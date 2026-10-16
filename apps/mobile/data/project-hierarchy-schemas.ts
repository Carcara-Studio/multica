import { z } from "zod";
import type {
  ProjectHierarchyMembership,
  ProjectHierarchyResponse,
} from "@multica/core/types/project-hierarchy";

export const ProjectHierarchyMembershipSchema: z.ZodType<ProjectHierarchyMembership> = z
  .object({
    id: z.string(),
    agent_id: z.string(),
    manager_membership_id: z.string().nullable(),
    title: z.string(),
    created_at: z.string(),
    updated_at: z.string(),
  })
  .loose();

export const ProjectHierarchyResponseSchema: z.ZodType<ProjectHierarchyResponse> = z
  .object({
    memberships: z.array(ProjectHierarchyMembershipSchema),
    total: z.number(),
  })
  .loose();

/**
 * A parse fallback must not masquerade as an empty, valid hierarchy. `total=-1`
 * deliberately fails the projection invariant so the UI renders an invalid-state
 * error instead of "Not configured" when a mixed-version server drifts.
 */
export const INVALID_PROJECT_HIERARCHY_RESPONSE: ProjectHierarchyResponse = {
  memberships: [],
  total: -1,
};

/** Write responses are never used to rewrite the hierarchy cache directly. */
export const INVALID_PROJECT_HIERARCHY_MEMBERSHIP: ProjectHierarchyMembership = {
  id: "",
  agent_id: "",
  manager_membership_id: null,
  title: "",
  created_at: "",
  updated_at: "",
};
