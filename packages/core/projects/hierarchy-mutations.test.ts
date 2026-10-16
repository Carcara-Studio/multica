import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ProjectHierarchyResponse } from "../types/project-hierarchy";
import {
  useApplyProjectHierarchyStructure,
  useArchiveProjectHierarchyMembership,
  useCreateProjectHierarchyMembership,
  useReparentProjectHierarchyMembership,
} from "./hierarchy-mutations";

type MutationConfig = {
  retry?: boolean;
  mutationFn?: (input: unknown) => Promise<unknown>;
  onMutate?: unknown;
  onSuccess?: (data: unknown) => unknown;
  onSettled?: () => unknown;
};

const mocks = vi.hoisted(() => ({
  configs: [] as MutationConfig[],
  invalidateQueries: vi.fn(),
  setQueryData: vi.fn(),
  createMembership: vi.fn(),
  reparentMembership: vi.fn(),
  archiveMembership: vi.fn(),
  applyStructure: vi.fn(),
}));

vi.mock("@tanstack/react-query", () => ({
  useMutation: (config: MutationConfig) => {
    mocks.configs.push(config);
    return { mutate: vi.fn(), isPending: false };
  },
  useQueryClient: () => ({
    invalidateQueries: mocks.invalidateQueries,
    setQueryData: mocks.setQueryData,
  }),
}));

vi.mock("../hooks", () => ({
  useWorkspaceId: () => "workspace-1",
}));

vi.mock("../api/project-hierarchy", () => ({
  projectHierarchyApi: {
    createMembership: mocks.createMembership,
    reparentMembership: mocks.reparentMembership,
    archiveMembership: mocks.archiveMembership,
    applyStructure: mocks.applyStructure,
  },
}));

const hierarchy: ProjectHierarchyResponse = {
  memberships: [],
  total: 0,
};

function configAt(index: number): MutationConfig {
  const config = mocks.configs[index];
  if (!config) throw new Error(`Missing mutation config ${index}`);
  return config;
}

beforeEach(() => {
  mocks.configs.length = 0;
  mocks.invalidateQueries.mockReset();
  mocks.setQueryData.mockReset();
  mocks.createMembership.mockReset().mockResolvedValue({});
  mocks.reparentMembership.mockReset().mockResolvedValue({});
  mocks.archiveMembership.mockReset().mockResolvedValue(undefined);
  mocks.applyStructure.mockReset().mockResolvedValue(hierarchy);
});

describe("Project hierarchy mutation cache policy", () => {
  it("disables automatic retries and speculative optimistic rewrites for every structural mutation", () => {
    useCreateProjectHierarchyMembership("project-1");
    useReparentProjectHierarchyMembership("project-1");
    useArchiveProjectHierarchyMembership("project-1");
    useApplyProjectHierarchyStructure("project-1");

    expect(mocks.configs).toHaveLength(4);
    for (const config of mocks.configs) {
      expect(config.retry).toBe(false);
      expect("onMutate" in config).toBe(false);
    }
  });

  it("reconciles ordinary structural writes by invalidating the scoped server query", async () => {
    useCreateProjectHierarchyMembership("project-1");
    const create = configAt(0);
    const payload = {
      agent_id: "agent-1",
      manager_membership_id: null,
      title: "Project Lead",
    };

    await create.mutationFn?.(payload);
    create.onSettled?.();

    expect(mocks.createMembership).toHaveBeenCalledWith("project-1", payload);
    expect(mocks.setQueryData).not.toHaveBeenCalled();
    expect(mocks.invalidateQueries).toHaveBeenCalledWith({
      queryKey: ["project-hierarchy", "workspace-1", "detail", "project-1"],
    });
  });

  it("accepts the server-returned atomic structure then refetches authoritative state", async () => {
    useApplyProjectHierarchyStructure("project-1");
    const apply = configAt(0);
    const request = {
      changes: [
        { membership_id: "new-root", manager_membership_id: null },
        { membership_id: "old-root", manager_membership_id: "new-root" },
      ],
    };

    await apply.mutationFn?.(request);
    apply.onSuccess?.(hierarchy);
    apply.onSettled?.();

    expect(mocks.applyStructure).toHaveBeenCalledWith("project-1", request);
    expect(mocks.setQueryData).toHaveBeenCalledWith(
      ["project-hierarchy", "workspace-1", "detail", "project-1"],
      hierarchy,
    );
    expect(mocks.invalidateQueries).toHaveBeenCalledWith({
      queryKey: ["project-hierarchy", "workspace-1", "detail", "project-1"],
    });
  });
});
