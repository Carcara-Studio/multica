import { queryOptions } from "@tanstack/react-query";
import { projectHierarchyApi } from "../api/project-hierarchy";

export const projectHierarchyKeys = {
  all: (wsId: string) => ["project-hierarchy", wsId] as const,
  detail: (wsId: string, projectId: string) =>
    [...projectHierarchyKeys.all(wsId), "detail", projectId] as const,
};

export function projectHierarchyOptions(wsId: string, projectId: string) {
  return queryOptions({
    queryKey: projectHierarchyKeys.detail(wsId, projectId),
    queryFn: () => projectHierarchyApi.get(projectId),
  });
}
