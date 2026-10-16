import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/data/api", () => ({ api: {} }));

import { createMobileProjectHierarchyApi } from "./project-hierarchy-api";

const membership = {
  id: "membership-1",
  agent_id: "agent-1",
  manager_membership_id: null,
  title: "Project Lead",
  created_at: "2026-10-16T00:00:00Z",
  updated_at: "2026-10-16T00:00:00Z",
};

describe("mobile project hierarchy API", () => {
  const request = vi.fn();
  const client = { fetch: request };
  const hierarchyApi = createMobileProjectHierarchyApi(client);

  beforeEach(() => {
    request.mockReset();
  });

  it("uses the five HIER-004 routes and payloads", async () => {
    request.mockResolvedValueOnce({ memberships: [membership], total: 1 });
    await hierarchyApi.get("project/a", { signal: undefined });
    expect(request).toHaveBeenLastCalledWith(
      "/api/projects/project%2Fa/hierarchy",
      { signal: undefined },
    );

    request.mockResolvedValueOnce(membership);
    await hierarchyApi.createMembership("project/a", {
      agent_id: "agent-1",
      manager_membership_id: null,
      title: "Project Lead",
    });
    expect(request).toHaveBeenLastCalledWith(
      "/api/projects/project%2Fa/hierarchy/memberships",
      {
        method: "POST",
        body: JSON.stringify({
          agent_id: "agent-1",
          manager_membership_id: null,
          title: "Project Lead",
        }),
      },
    );

    request.mockResolvedValueOnce({ ...membership, manager_membership_id: "manager-1" });
    await hierarchyApi.reparentMembership("project/a", "membership/1", {
      manager_membership_id: "manager-1",
    });
    expect(request).toHaveBeenLastCalledWith(
      "/api/projects/project%2Fa/hierarchy/memberships/membership%2F1/manager",
      {
        method: "PUT",
        body: JSON.stringify({ manager_membership_id: "manager-1" }),
      },
    );

    request.mockResolvedValueOnce(undefined);
    await hierarchyApi.archiveMembership("project/a", "membership/1");
    expect(request).toHaveBeenLastCalledWith(
      "/api/projects/project%2Fa/hierarchy/memberships/membership%2F1",
      { method: "DELETE" },
    );

    const structure = {
      changes: [
        { membership_id: "membership-1", manager_membership_id: null },
      ],
    };
    request.mockResolvedValueOnce({ memberships: [membership], total: 1 });
    await hierarchyApi.applyStructure("project/a", structure);
    expect(request).toHaveBeenLastCalledWith(
      "/api/projects/project%2Fa/hierarchy/structure",
      { method: "PUT", body: JSON.stringify(structure) },
    );
  });

  it("turns a malformed read into an explicitly invalid projection sentinel", async () => {
    request.mockResolvedValueOnce({ memberships: "not-an-array", total: 0 });
    await expect(hierarchyApi.get("project-1")).resolves.toEqual({
      memberships: [],
      total: -1,
    });
  });
});
