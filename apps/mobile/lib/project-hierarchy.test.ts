import { describe, expect, it } from "vitest";
import type { ProjectHierarchyMembership } from "@multica/core/types/project-hierarchy";
import {
  buildProjectHierarchyRootPromotion,
  flattenProjectHierarchy,
  projectHierarchyDescendantIds,
  projectHierarchyLeafIds,
  projectHierarchyToTree,
} from "./project-hierarchy";

const ts = "2026-10-16T00:00:00Z";
const member = (
  id: string,
  agentId: string,
  managerMembershipId: string | null,
  title: string,
): ProjectHierarchyMembership => ({
  id,
  agent_id: agentId,
  manager_membership_id: managerMembershipId,
  title,
  created_at: ts,
  updated_at: ts,
});

describe("mobile project hierarchy projection", () => {
  it("supports empty and arbitrary-depth hierarchies", () => {
    expect(projectHierarchyToTree({ memberships: [], total: 0 })).toEqual({
      ok: true,
      root: null,
      count: 0,
    });

    const memberships = [
      member("root", "a1", null, "Project Lead"),
      member("lead", "a2", "root", "Backend Lead"),
      member("engineer", "a3", "lead", "Backend Engineer"),
    ];
    const projection = projectHierarchyToTree({ memberships, total: 3 });
    expect(projection.ok).toBe(true);
    if (!projection.ok) return;
    expect(flattenProjectHierarchy(projection.root).map((row) => [row.membership.id, row.depth])).toEqual([
      ["root", 0],
      ["lead", 1],
      ["engineer", 2],
    ]);
  });

  it("rejects malformed state instead of repairing it", () => {
    expect(projectHierarchyToTree({ memberships: [], total: -1 })).toMatchObject({
      ok: false,
      code: "total_mismatch",
    });
    expect(
      projectHierarchyToTree({
        memberships: [member("a", "a1", "missing", "Engineer")],
        total: 1,
      }),
    ).toMatchObject({ ok: false, code: "missing_root" });
    expect(
      projectHierarchyToTree({
        memberships: [
          member("a", "a1", null, "A"),
          member("b", "a2", null, "B"),
        ],
        total: 2,
      }),
    ).toMatchObject({ ok: false, code: "multiple_roots" });
  });

  it("derives descendants, leaves, and atomic root promotion by membership id", () => {
    const memberships = [
      member("root", "shared-agent", null, "Lead"),
      member("branch", "shared-agent", "root", "CTO"),
      member("leaf", "a3", "branch", "Engineer"),
    ];

    expect([...projectHierarchyDescendantIds(memberships, "root")].sort()).toEqual([
      "branch",
      "leaf",
    ]);
    expect([...projectHierarchyLeafIds(memberships)]).toEqual(["leaf"]);
    expect(buildProjectHierarchyRootPromotion(memberships, "branch")).toEqual({
      changes: [
        { membership_id: "branch", manager_membership_id: null },
        { membership_id: "root", manager_membership_id: "branch" },
      ],
    });
  });
});
