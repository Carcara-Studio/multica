import { describe, expect, it } from "vitest";
import type { Agent } from "@multica/core/types";
import type { ProjectHierarchyMembership } from "@multica/core/types/project-hierarchy";
import {
  availableHierarchyAgents,
  canArchiveHierarchyMembership,
  hierarchyManagerCandidates,
  makeHierarchyRootRequest,
} from "./project-hierarchy-view-model";

function membership(
  id: string,
  managerId: string | null,
  agentId = `agent-${id}`,
): ProjectHierarchyMembership {
  return {
    id,
    agent_id: agentId,
    manager_membership_id: managerId,
    title: id,
    created_at: "2026-10-16T00:00:00Z",
    updated_at: "2026-10-16T00:00:00Z",
  };
}

function agent(id: string, archived = false): Agent {
  return {
    id,
    name: id,
    archived_at: archived ? "2026-10-16T00:00:00Z" : null,
  } as Agent;
}

const hierarchy = [
  membership("root", null),
  membership("a", "root"),
  membership("b", "root"),
  membership("a1", "a"),
];

describe("project hierarchy view model", () => {
  it("offers only active agents that are not already members", () => {
    expect(
      availableHierarchyAgents(
        [agent("agent-root"), agent("new"), agent("archived", true)],
        hierarchy,
      ).map((entry) => entry.id),
    ).toEqual(["new"]);
  });

  it("excludes self and descendants from reparent targets", () => {
    expect(
      hierarchyManagerCandidates(hierarchy, "a").map((entry) => entry.id),
    ).toEqual(["root", "b"]);
  });

  it("allows archive only for active non-root leaves", () => {
    expect(canArchiveHierarchyMembership(hierarchy, "root")).toBe(false);
    expect(canArchiveHierarchyMembership(hierarchy, "a")).toBe(false);
    expect(canArchiveHierarchyMembership(hierarchy, "a1")).toBe(true);
    expect(canArchiveHierarchyMembership(hierarchy, "missing")).toBe(false);
  });

  it("builds the atomic root replacement payload", () => {
    expect(makeHierarchyRootRequest(hierarchy, "a1")).toEqual({
      changes: [
        { membership_id: "a1", manager_membership_id: null },
        { membership_id: "root", manager_membership_id: "a1" },
      ],
    });
  });

  it("does not build root replacement for malformed or no-op state", () => {
    expect(makeHierarchyRootRequest(hierarchy, "root")).toBeNull();
    expect(
      makeHierarchyRootRequest(
        [membership("a", null), membership("b", null)],
        "b",
      ),
    ).toBeNull();
  });
});
