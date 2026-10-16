import type { Agent } from "@multica/core/types";
import type {
  ApplyProjectHierarchyStructureRequest,
  ProjectHierarchyMembership,
} from "@multica/core/types/project-hierarchy";
import {
  projectHierarchyDescendantIds,
  projectHierarchyLeafIds,
} from "@multica/core/projects";

export function availableHierarchyAgents(
  agents: Agent[],
  memberships: ProjectHierarchyMembership[],
): Agent[] {
  const assignedAgentIds = new Set(
    memberships.map((membership) => membership.agent_id),
  );
  return agents.filter(
    (agent) => !agent.archived_at && !assignedAgentIds.has(agent.id),
  );
}

export function hierarchyManagerCandidates(
  memberships: ProjectHierarchyMembership[],
  movingMembershipId: string,
): ProjectHierarchyMembership[] {
  const descendants = projectHierarchyDescendantIds(
    memberships,
    movingMembershipId,
  );
  return memberships.filter(
    (membership) =>
      membership.id !== movingMembershipId && !descendants.has(membership.id),
  );
}

export function canArchiveHierarchyMembership(
  memberships: ProjectHierarchyMembership[],
  membershipId: string,
): boolean {
  const membership = memberships.find((entry) => entry.id === membershipId);
  if (!membership || membership.manager_membership_id === null) return false;
  return projectHierarchyLeafIds(memberships).has(membershipId);
}

export function makeHierarchyRootRequest(
  memberships: ProjectHierarchyMembership[],
  newRootMembershipId: string,
): ApplyProjectHierarchyStructureRequest | null {
  const roots = memberships.filter(
    (membership) => membership.manager_membership_id === null,
  );
  if (roots.length !== 1) return null;

  const oldRoot = roots[0];
  const newRoot = memberships.find(
    (membership) => membership.id === newRootMembershipId,
  );
  if (!oldRoot || !newRoot || oldRoot.id === newRoot.id) return null;

  return {
    changes: [
      {
        membership_id: newRoot.id,
        manager_membership_id: null,
      },
      {
        membership_id: oldRoot.id,
        manager_membership_id: newRoot.id,
      },
    ],
  };
}
