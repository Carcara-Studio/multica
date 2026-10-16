// @vitest-environment jsdom

import React from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ApiError } from "@multica/core/api";
import { renderWithI18n } from "../../test/i18n";
import { ProjectHierarchySection } from "./project-hierarchy-section";

const mocks = vi.hoisted(() => ({
  hierarchy: { memberships: [], total: 0 } as any,
  agents: [] as any[],
  refetch: vi.fn(),
  createMembership: { mutate: vi.fn(), isPending: false },
  reparentMembership: { mutate: vi.fn(), isPending: false },
  archiveMembership: { mutate: vi.fn(), isPending: false },
  applyStructure: { mutate: vi.fn(), isPending: false },
  mutationError: null as unknown,
}));

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return {
    ...actual,
    useQuery: (options: { queryKey?: readonly unknown[] }) => {
      if (options.queryKey?.[0] === "project-hierarchy") {
        return {
          data: mocks.hierarchy,
          isLoading: false,
          isError: false,
          refetch: mocks.refetch,
        };
      }
      if (options.queryKey?.[0] === "agents") {
        return { data: mocks.agents, isLoading: false, isError: false };
      }
      return { data: undefined, isLoading: false, isError: false };
    },
  };
});

vi.mock("@multica/core/projects", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/projects")>();
  return {
    ...actual,
    useCreateProjectHierarchyMembership: () => mocks.createMembership,
    useReparentProjectHierarchyMembership: () => mocks.reparentMembership,
    useArchiveProjectHierarchyMembership: () => mocks.archiveMembership,
    useApplyProjectHierarchyStructure: () => mocks.applyStructure,
  };
});

vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: () => ({ queryKey: ["agents"] }),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "workspace-1",
}));

vi.mock("@multica/ui/components/ui/dialog", () => ({
  Dialog: ({ open, children }: { open: boolean; children: React.ReactNode }) =>
    open ? <div>{children}</div> : null,
  DialogContent: ({ children }: { children: React.ReactNode }) => (
    <div role="dialog">{children}</div>
  ),
  DialogDescription: ({ children }: { children: React.ReactNode }) => <p>{children}</p>,
  DialogHeader: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DialogTitle: ({ children }: { children: React.ReactNode }) => <h2>{children}</h2>,
}));

vi.mock("@multica/ui/components/ui/dropdown-menu", () => ({
  DropdownMenu: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  DropdownMenuTrigger: ({ render }: { render: React.ReactNode }) => <>{render}</>,
  DropdownMenuContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DropdownMenuItem: ({
    children,
    onClick,
  }: {
    children: React.ReactNode;
    onClick?: () => void;
  }) => (
    <button type="button" onClick={onClick}>
      {children}
    </button>
  ),
  DropdownMenuSeparator: () => <hr />,
}));

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: ({ actorId }: { actorId: string }) => <span data-testid={`avatar-${actorId}`} />,
}));

const timestamp = "2026-10-16T00:00:00Z";

function member(
  id: string,
  agentId: string,
  managerMembershipId: string | null,
  title: string,
) {
  return {
    id,
    agent_id: agentId,
    manager_membership_id: managerMembershipId,
    title,
    created_at: timestamp,
    updated_at: timestamp,
  };
}

function setHierarchy(memberships: ReturnType<typeof member>[]) {
  mocks.hierarchy = { memberships, total: memberships.length };
}

function openHierarchy() {
  renderWithI18n(<ProjectHierarchySection projectId="project-1" />);
  fireEvent.click(screen.getByRole("button", { name: /set up|manage/i }));
}

function installMutationBehavior(mutation: { mutate: ReturnType<typeof vi.fn> }) {
  mutation.mutate.mockImplementation(
    (_payload: unknown, options?: { onError?: (error: unknown) => void; onSuccess?: () => void }) => {
      if (mocks.mutationError) options?.onError?.(mocks.mutationError);
      else options?.onSuccess?.();
    },
  );
}

beforeEach(() => {
  setHierarchy([]);
  mocks.agents = [];
  mocks.mutationError = null;
  mocks.refetch.mockReset().mockResolvedValue(undefined);
  for (const mutation of [
    mocks.createMembership,
    mocks.reparentMembership,
    mocks.archiveMembership,
    mocks.applyStructure,
  ]) {
    mutation.mutate.mockReset();
    mutation.isPending = false;
    installMutationBehavior(mutation);
  }
});

describe("ProjectHierarchySection", () => {
  it("shows the empty summary and opens first-root setup", () => {
    openHierarchy();

    expect(screen.getByText("Not configured")).toBeTruthy();
    expect(screen.getByRole("dialog")).toBeTruthy();
    expect(screen.getByText("No hierarchy yet")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Add root" })).toBeTruthy();
  });

  it("renders arbitrary depth without depending on Agent metadata for node identity", () => {
    setHierarchy([
      member("root", "agent-root", null, "Project Lead"),
      member("lead", "agent-lead", "root", "Backend Lead"),
      member("engineer", "missing-agent", "lead", "Backend Engineer"),
    ]);
    mocks.agents = [
      { id: "agent-root", name: "Root Agent", archived_at: null },
      { id: "agent-lead", name: "Lead Agent", archived_at: null },
    ];

    openHierarchy();

    expect(screen.getByText("3 hierarchy members")).toBeTruthy();
    expect(screen.getByText("Project Lead")).toBeTruthy();
    expect(screen.getByText("Backend Lead")).toBeTruthy();
    expect(screen.getByText("Backend Engineer")).toBeTruthy();
    expect(screen.getByText("Agent unavailable")).toBeTruthy();
  });

  it("creates the first root with a Project-scoped role", async () => {
    const user = userEvent.setup();
    mocks.agents = [{ id: "agent-1", name: "Agent One", archived_at: null }];
    openHierarchy();

    await user.click(screen.getByRole("button", { name: "Add root" }));
    await user.click(screen.getByRole("button", { name: "Agent One" }));
    fireEvent.change(screen.getByPlaceholderText("e.g. Backend Lead"), {
      target: { value: "Project Lead" },
    });
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(mocks.createMembership.mutate).toHaveBeenCalledWith(
      {
        agent_id: "agent-1",
        manager_membership_id: null,
        title: "Project Lead",
      },
      expect.any(Object),
    );
  });

  it("adds a direct report under the selected membership", async () => {
    const user = userEvent.setup();
    setHierarchy([member("root", "agent-root", null, "Project Lead")]);
    mocks.agents = [
      { id: "agent-root", name: "Root Agent", archived_at: null },
      { id: "agent-child", name: "Child Agent", archived_at: null },
    ];
    openHierarchy();

    await user.click(screen.getByRole("button", { name: "Add report" }));
    await user.click(screen.getByRole("button", { name: "Child Agent" }));
    fireEvent.change(screen.getByPlaceholderText("e.g. Backend Lead"), {
      target: { value: "Backend Lead" },
    });
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(mocks.createMembership.mutate).toHaveBeenCalledWith(
      {
        agent_id: "agent-child",
        manager_membership_id: "root",
        title: "Backend Lead",
      },
      expect.any(Object),
    );
  });

  it("reparents a non-root membership through the manager mutation", async () => {
    const user = userEvent.setup();
    setHierarchy([
      member("root", "agent-root", null, "Project Lead"),
      member("moving", "agent-moving", "root", "Backend Lead"),
      member("peer", "agent-peer", "root", "Product Lead"),
    ]);
    mocks.agents = [
      { id: "agent-root", name: "Root Agent", archived_at: null },
      { id: "agent-moving", name: "Moving Agent", archived_at: null },
      { id: "agent-peer", name: "Peer Agent", archived_at: null },
    ];
    openHierarchy();

    await user.click(screen.getAllByRole("button", { name: "Move under…" })[0]!);
    await user.click(screen.getByRole("button", { name: "Product Lead" }));
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(mocks.reparentMembership.mutate).toHaveBeenCalledWith(
      {
        membershipId: "moving",
        data: { manager_membership_id: "peer" },
      },
      expect.any(Object),
    );
  });

  it("promotes a new root only through one atomic structure mutation", async () => {
    const user = userEvent.setup();
    setHierarchy([
      member("root", "agent-root", null, "Project Lead"),
      member("child", "agent-child", "root", "CTO"),
    ]);
    mocks.agents = [
      { id: "agent-root", name: "Root Agent", archived_at: null },
      { id: "agent-child", name: "Child Agent", archived_at: null },
    ];
    openHierarchy();

    await user.click(screen.getByRole("button", { name: "Make root" }));

    expect(mocks.applyStructure.mutate).toHaveBeenCalledWith(
      {
        changes: [
          { membership_id: "child", manager_membership_id: null },
          { membership_id: "root", manager_membership_id: "child" },
        ],
      },
      expect.any(Object),
    );
    expect(mocks.reparentMembership.mutate).not.toHaveBeenCalled();
  });

  it("offers Archive only for an active non-root leaf", async () => {
    const user = userEvent.setup();
    setHierarchy([
      member("root", "agent-root", null, "Project Lead"),
      member("branch", "agent-branch", "root", "Backend Lead"),
      member("leaf", "agent-leaf", "branch", "Backend Engineer"),
    ]);
    mocks.agents = [
      { id: "agent-root", name: "Root Agent", archived_at: null },
      { id: "agent-branch", name: "Branch Agent", archived_at: null },
      { id: "agent-leaf", name: "Leaf Agent", archived_at: null },
    ];
    openHierarchy();

    const archiveButtons = screen.getAllByRole("button", { name: "Archive" });
    expect(archiveButtons).toHaveLength(1);
    await user.click(archiveButtons[0]!);
    expect(mocks.archiveMembership.mutate).toHaveBeenCalledWith(
      "leaf",
      expect.any(Object),
    );
    expect(screen.queryByPlaceholderText("e.g. Backend Lead")).toBeNull();
  });

  it("shows a 409 conflict and refreshes before the user retries", async () => {
    const user = userEvent.setup();
    setHierarchy([
      member("root", "agent-root", null, "Project Lead"),
      member("leaf", "agent-leaf", "root", "Backend Engineer"),
    ]);
    mocks.agents = [
      { id: "agent-root", name: "Root Agent", archived_at: null },
      { id: "agent-leaf", name: "Leaf Agent", archived_at: null },
    ];
    mocks.mutationError = new ApiError(
      "Hierarchy changed concurrently",
      409,
      "Conflict",
      { error: "Hierarchy changed concurrently" },
    );
    openHierarchy();

    await user.click(screen.getByRole("button", { name: "Archive" }));

    expect(screen.getByText("Hierarchy changed concurrently")).toBeTruthy();
    expect(mocks.refetch).toHaveBeenCalledTimes(1);
  });
});
