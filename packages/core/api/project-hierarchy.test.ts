import { describe, expect, it, vi } from "vitest";
import type { ApiClient } from "./client";
import { createProjectHierarchyApi } from "./project-hierarchy";

function fakeClient() {
  const fetch = vi.fn().mockResolvedValue({ memberships: [], total: 0 });
  return {
    client: { fetch } as unknown as ApiClient,
    fetch,
  };
}

describe("ProjectHierarchyApi", () => {
  it("loads the active hierarchy", async () => {
    const { client, fetch } = fakeClient();
    await createProjectHierarchyApi(client).get("project/unsafe");
    expect(fetch).toHaveBeenCalledWith(
      "/api/projects/project%2Funsafe/hierarchy",
      undefined,
    );
  });

  it("creates a membership", async () => {
    const { client, fetch } = fakeClient();
    const data = {
      agent_id: "agent-1",
      manager_membership_id: "manager-1",
      title: "Backend Engineer",
    };
    await createProjectHierarchyApi(client).createMembership("project-1", data);
    expect(fetch).toHaveBeenCalledWith(
      "/api/projects/project-1/hierarchy/memberships",
      { method: "POST", body: JSON.stringify(data) },
    );
  });

  it("reparents a membership", async () => {
    const { client, fetch } = fakeClient();
    const data = { manager_membership_id: "manager-2" };
    await createProjectHierarchyApi(client).reparentMembership(
      "project-1",
      "membership/unsafe",
      data,
    );
    expect(fetch).toHaveBeenCalledWith(
      "/api/projects/project-1/hierarchy/memberships/membership%2Funsafe/manager",
      { method: "PUT", body: JSON.stringify(data) },
    );
  });

  it("archives a membership", async () => {
    const { client, fetch } = fakeClient();
    await createProjectHierarchyApi(client).archiveMembership(
      "project-1",
      "membership-1",
    );
    expect(fetch).toHaveBeenCalledWith(
      "/api/projects/project-1/hierarchy/memberships/membership-1",
      { method: "DELETE" },
    );
  });

  it("applies an atomic structure change", async () => {
    const { client, fetch } = fakeClient();
    const data = {
      changes: [
        { membership_id: "new-root", manager_membership_id: null },
        { membership_id: "old-root", manager_membership_id: "new-root" },
      ],
    };
    await createProjectHierarchyApi(client).applyStructure("project-1", data);
    expect(fetch).toHaveBeenCalledWith(
      "/api/projects/project-1/hierarchy/structure",
      { method: "PUT", body: JSON.stringify(data) },
    );
  });
});
