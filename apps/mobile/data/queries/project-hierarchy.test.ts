import { describe, expect, it, vi } from "vitest";

vi.mock("@/data/project-hierarchy-api", () => ({
  projectHierarchyApi: { get: vi.fn() },
}));

import {
  projectHierarchyKeys,
  projectHierarchyOptions,
} from "./project-hierarchy";

describe("mobile project hierarchy query", () => {
  it("keys hierarchy state by workspace and project", () => {
    expect(projectHierarchyKeys.detail("ws-1", "project-1")).toEqual([
      "project-hierarchy",
      "ws-1",
      "project-1",
    ]);
    expect(projectHierarchyKeys.detail("ws-2", "project-1")).not.toEqual(
      projectHierarchyKeys.detail("ws-1", "project-1"),
    );
  });

  it("disables reads until workspace and project are both known", () => {
    expect(projectHierarchyOptions(null, "project-1").enabled).toBe(false);
    expect(projectHierarchyOptions("ws-1", "").enabled).toBe(false);
    expect(projectHierarchyOptions("ws-1", "project-1").enabled).toBe(true);
  });
});
