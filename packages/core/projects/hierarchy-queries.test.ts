import { describe, expect, it } from "vitest";
import { projectHierarchyKeys, projectHierarchyOptions } from "./hierarchy-queries";

describe("projectHierarchyKeys", () => {
  it("scopes hierarchy cache identity by workspace and project", () => {
    expect(projectHierarchyKeys.detail("workspace-a", "project-1")).toEqual([
      "project-hierarchy",
      "workspace-a",
      "detail",
      "project-1",
    ]);
    expect(projectHierarchyKeys.detail("workspace-b", "project-1")).not.toEqual(
      projectHierarchyKeys.detail("workspace-a", "project-1"),
    );
    expect(projectHierarchyKeys.detail("workspace-a", "project-2")).not.toEqual(
      projectHierarchyKeys.detail("workspace-a", "project-1"),
    );
  });

  it("uses the scoped key in query options", () => {
    expect(projectHierarchyOptions("workspace-a", "project-1").queryKey).toEqual(
      projectHierarchyKeys.detail("workspace-a", "project-1"),
    );
  });
});
