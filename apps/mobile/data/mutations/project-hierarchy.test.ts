import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  options: [] as Array<Record<string, unknown>>,
  invalidateQueries: vi.fn(),
  setQueryData: vi.fn(),
}));

vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({
    invalidateQueries: mocks.invalidateQueries,
    setQueryData: mocks.setQueryData,
  }),
  useMutation: (options: Record<string, unknown>) => {
    mocks.options.push(options);
    return options;
  },
}));

vi.mock("@/data/workspace-store", () => ({
  useWorkspaceStore: (selector: (state: { currentWorkspaceId: string }) => unknown) =>
    selector({ currentWorkspaceId: "ws-1" }),
}));

vi.mock("@/data/project-hierarchy-api", () => ({
  projectHierarchyApi: {
    createMembership: vi.fn(),
    reparentMembership: vi.fn(),
    archiveMembership: vi.fn(),
    applyStructure: vi.fn(),
  },
}));

import {
  useApplyProjectHierarchyStructure,
  useArchiveProjectHierarchyMembership,
  useCreateProjectHierarchyMembership,
  useReparentProjectHierarchyMembership,
} from "./project-hierarchy";

beforeEach(() => {
  mocks.options.length = 0;
  mocks.invalidateQueries.mockReset();
  mocks.setQueryData.mockReset();
});

describe("mobile project hierarchy mutations", () => {
  it("never retries or performs speculative optimistic graph rewrites", () => {
    useCreateProjectHierarchyMembership("project-1");
    useReparentProjectHierarchyMembership("project-1");
    useArchiveProjectHierarchyMembership("project-1");
    useApplyProjectHierarchyStructure("project-1");

    expect(mocks.options).toHaveLength(4);
    for (const options of mocks.options) {
      expect(options.retry).toBe(false);
      expect(options).not.toHaveProperty("onMutate");
    }
  });

  it("reconciles every write with the workspace/project-scoped server state", async () => {
    useArchiveProjectHierarchyMembership("project-1");
    const archive = mocks.options[0]!;
    await (archive.onSettled as () => unknown)();
    expect(mocks.invalidateQueries).toHaveBeenCalledWith({
      queryKey: ["project-hierarchy", "ws-1", "project-1"],
    });

    mocks.options.length = 0;
    useApplyProjectHierarchyStructure("project-1");
    const structure = mocks.options[0]!;
    const authoritative = { memberships: [], total: 0 };
    (structure.onSuccess as (data: typeof authoritative) => void)(authoritative);
    expect(mocks.setQueryData).toHaveBeenCalledWith(
      ["project-hierarchy", "ws-1", "project-1"],
      authoritative,
    );
  });
});
