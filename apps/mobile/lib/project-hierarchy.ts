import type {
  ApplyProjectHierarchyStructureRequest,
  ProjectHierarchyMembership,
  ProjectHierarchyResponse,
} from "@multica/core/types/project-hierarchy";

export type ProjectHierarchyProjectionErrorCode =
  | "total_mismatch"
  | "duplicate_membership"
  | "missing_root"
  | "multiple_roots"
  | "orphan_manager"
  | "cycle";

export interface ProjectHierarchyTreeNode {
  membership: ProjectHierarchyMembership;
  children: ProjectHierarchyTreeNode[];
}

export type ProjectHierarchyProjection =
  | { ok: true; root: ProjectHierarchyTreeNode | null; count: number }
  | { ok: false; code: ProjectHierarchyProjectionErrorCode; message: string };

export interface FlatProjectHierarchyNode {
  membership: ProjectHierarchyMembership;
  depth: number;
}

function compareMemberships(
  left: ProjectHierarchyMembership,
  right: ProjectHierarchyMembership,
): number {
  return (
    left.title.localeCompare(right.title) ||
    left.agent_id.localeCompare(right.agent_id) ||
    left.id.localeCompare(right.id)
  );
}

export function projectHierarchyLeafIds(
  memberships: ProjectHierarchyMembership[],
): Set<string> {
  const nonLeaves = new Set<string>();
  for (const membership of memberships) {
    if (membership.manager_membership_id) nonLeaves.add(membership.manager_membership_id);
  }
  return new Set(
    memberships
      .filter((membership) => !nonLeaves.has(membership.id))
      .map((membership) => membership.id),
  );
}

export function projectHierarchyDescendantIds(
  memberships: ProjectHierarchyMembership[],
  membershipId: string,
): Set<string> {
  const childrenByManager = new Map<string, string[]>();
  for (const membership of memberships) {
    const managerId = membership.manager_membership_id;
    if (!managerId) continue;
    const children = childrenByManager.get(managerId) ?? [];
    children.push(membership.id);
    childrenByManager.set(managerId, children);
  }

  const descendants = new Set<string>();
  const pending = [...(childrenByManager.get(membershipId) ?? [])];
  while (pending.length > 0) {
    const id = pending.pop()!;
    if (descendants.has(id)) continue;
    descendants.add(id);
    pending.push(...(childrenByManager.get(id) ?? []));
  }
  return descendants;
}

export function projectHierarchyToTree(
  response: ProjectHierarchyResponse,
): ProjectHierarchyProjection {
  const { memberships, total } = response;
  if (total !== memberships.length) {
    return {
      ok: false,
      code: "total_mismatch",
      message: `Hierarchy total ${total} does not match ${memberships.length} memberships.`,
    };
  }
  if (memberships.length === 0) return { ok: true, root: null, count: 0 };

  const byId = new Map<string, ProjectHierarchyMembership>();
  for (const membership of memberships) {
    if (byId.has(membership.id)) {
      return {
        ok: false,
        code: "duplicate_membership",
        message: `Hierarchy contains duplicate membership ${membership.id}.`,
      };
    }
    byId.set(membership.id, membership);
  }

  const roots = memberships.filter((membership) => membership.manager_membership_id === null);
  if (roots.length === 0) {
    return { ok: false, code: "missing_root", message: "Hierarchy has memberships but no root." };
  }
  if (roots.length > 1) {
    return {
      ok: false,
      code: "multiple_roots",
      message: `Hierarchy contains ${roots.length} roots.`,
    };
  }
  const root = roots[0]!;

  for (const membership of memberships) {
    const managerId = membership.manager_membership_id;
    if (managerId && !byId.has(managerId)) {
      return {
        ok: false,
        code: "orphan_manager",
        message: `Membership ${membership.id} references missing manager ${managerId}.`,
      };
    }
  }

  const visitState = new Map<string, "visiting" | "visited">();
  const hasCycleFrom = (membershipId: string): boolean => {
    const state = visitState.get(membershipId);
    if (state === "visiting") return true;
    if (state === "visited") return false;
    visitState.set(membershipId, "visiting");
    const managerId = byId.get(membershipId)?.manager_membership_id;
    if (managerId && hasCycleFrom(managerId)) return true;
    visitState.set(membershipId, "visited");
    return false;
  };
  for (const membership of memberships) {
    if (hasCycleFrom(membership.id)) {
      return {
        ok: false,
        code: "cycle",
        message: `Hierarchy contains a cycle involving membership ${membership.id}.`,
      };
    }
  }

  const childrenByManager = new Map<string, ProjectHierarchyMembership[]>();
  for (const membership of memberships) {
    const managerId = membership.manager_membership_id;
    if (!managerId) continue;
    const children = childrenByManager.get(managerId) ?? [];
    children.push(membership);
    childrenByManager.set(managerId, children);
  }
  for (const children of childrenByManager.values()) children.sort(compareMemberships);

  const buildNode = (membership: ProjectHierarchyMembership): ProjectHierarchyTreeNode => ({
    membership,
    children: (childrenByManager.get(membership.id) ?? []).map(buildNode),
  });

  return { ok: true, root: buildNode(root), count: memberships.length };
}

export function flattenProjectHierarchy(
  root: ProjectHierarchyTreeNode | null,
): FlatProjectHierarchyNode[] {
  if (!root) return [];
  const result: FlatProjectHierarchyNode[] = [];
  const visit = (node: ProjectHierarchyTreeNode, depth: number) => {
    result.push({ membership: node.membership, depth });
    for (const child of node.children) visit(child, depth + 1);
  };
  visit(root, 0);
  return result;
}

export function buildProjectHierarchyRootPromotion(
  memberships: ProjectHierarchyMembership[],
  nextRootMembershipId: string,
): ApplyProjectHierarchyStructureRequest {
  const oldRoot = memberships.find((membership) => membership.manager_membership_id === null);
  const nextRoot = memberships.find((membership) => membership.id === nextRootMembershipId);
  if (!oldRoot || !nextRoot) throw new Error("Cannot promote root from an incomplete hierarchy");
  if (oldRoot.id === nextRoot.id) return { changes: [] };
  return {
    changes: [
      { membership_id: nextRoot.id, manager_membership_id: null },
      { membership_id: oldRoot.id, manager_membership_id: nextRoot.id },
    ],
  };
}
