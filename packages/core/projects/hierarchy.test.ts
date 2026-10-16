import { describe, expect, it } from "vitest";
import type {
  ProjectHierarchyMembership,
  ProjectHierarchyResponse,
} from "../types/project-hierarchy";
import {
  projectHierarchyDescendantIds,
  projectHierarchyLeafIds,
  projectHierarchyToTree,
} from "./hierarchy";

function membership(
  id: string,
  managerId: string | null,
  title = id,
  agentId = `agent-${id}`,
): ProjectHierarchyMembership {
  return {
    id,
    agent_id: agentId,
    manager_membership_id: managerId,
    title,
    created_at: "2026-10-16T00:00:00Z",
    updated_at: "2026-10-16T00:00:00Z",
  };
}

function response(
  memberships: ProjectHierarchyMembership[],
  total = memberships.length,
): ProjectHierarchyResponse {
  return { memberships, total };
}

describe("projectHierarchyToTree", () => {
  it("projects an empty hierarchy", () => {
    expect(projectHierarchyToTree(response([]))).toEqual({
      ok: true,
      root: null,
      count: 0,
    });
  });

  it("projects a root-only hierarchy", () => {
    const result = projectHierarchyToTree(response([membership("root", null)]));
    expect(result).toEqual({
      ok: true,
      count: 1,
      root: {
        membership: membership("root", null),
        children: [],
      },
    });
  });

  it("projects arbitrary depth with deterministic sibling ordering", () => {
    const result = projectHierarchyToTree(
      response([
        membership("root", null, "Root"),
        membership("z", "root", "Zeta"),
        membership("b", "root", "Backend"),
        membership("b2", "b", "Engineer"),
        membership("b1", "b", "Engineer", "agent-a"),
        membership("deep", "b1", "Deep"),
      ]),
    );
    expect(result.ok).toBe(true);
    if (!result.ok || !result.root) return;

    expect(result.root.children.map((node) => node.membership.id)).toEqual([
      "b",
      "z",
    ]);
    const backend = result.root.children[0]!;
    expect(backend.children.map((node) => node.membership.id)).toEqual([
      "b1",
      "b2",
    ]);
    const backendLead = backend.children[0]!;
    expect(backendLead.children[0]!.membership.id).toBe("deep");
  });

  it("uses membership id rather than agent id as structural identity", () => {
    const result = projectHierarchyToTree(
      response([
        membership("root", null, "Root", "same-agent"),
        membership("child", "root", "Child", "same-agent"),
      ]),
    );
    expect(result.ok).toBe(true);
    if (!result.ok || !result.root) return;
    expect(result.root.membership.id).toBe("root");
    expect(result.root.children[0]!.membership.id).toBe("child");
  });

  it("rejects a total mismatch", () => {
    expect(projectHierarchyToTree(response([membership("root", null)], 2))).toMatchObject({
      ok: false,
      code: "total_mismatch",
    });
  });

  it("rejects duplicate membership ids", () => {
    expect(
      projectHierarchyToTree(
        response([
          membership("root", null),
          membership("root", "root"),
        ]),
      ),
    ).toMatchObject({ ok: false, code: "duplicate_membership" });
  });

  it("rejects a non-empty hierarchy without a root", () => {
    expect(
      projectHierarchyToTree(
        response([
          membership("a", "b"),
          membership("b", "a"),
        ]),
      ),
    ).toMatchObject({ ok: false, code: "missing_root" });
  });

  it("rejects multiple roots", () => {
    expect(
      projectHierarchyToTree(
        response([membership("a", null), membership("b", null)]),
      ),
    ).toMatchObject({ ok: false, code: "multiple_roots" });
  });

  it("rejects orphan manager references", () => {
    expect(
      projectHierarchyToTree(
        response([
          membership("root", null),
          membership("child", "missing"),
        ]),
      ),
    ).toMatchObject({ ok: false, code: "orphan_manager" });
  });

  it("rejects a cycle even when another valid root exists", () => {
    expect(
      projectHierarchyToTree(
        response([
          membership("root", null),
          membership("a", "b"),
          membership("b", "a"),
        ]),
      ),
    ).toMatchObject({ ok: false, code: "cycle" });
  });
});

describe("project hierarchy mutation helpers", () => {
  const memberships = [
    membership("root", null),
    membership("a", "root"),
    membership("b", "root"),
    membership("a1", "a"),
  ];

  it("identifies active leaves", () => {
    expect([...projectHierarchyLeafIds(memberships)].sort()).toEqual(["a1", "b"]);
  });

  it("identifies descendants without including the starting membership", () => {
    expect([...projectHierarchyDescendantIds(memberships, "root")].sort()).toEqual([
      "a",
      "a1",
      "b",
    ]);
    expect([...projectHierarchyDescendantIds(memberships, "a")]).toEqual(["a1"]);
  });
});
