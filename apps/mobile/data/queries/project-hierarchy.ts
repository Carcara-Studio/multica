import { queryOptions } from "@tanstack/react-query";
import { projectHierarchyApi } from "@/data/project-hierarchy-api";

export const projectHierarchyKeys = {
  all: (wsId: string | null) => ["project-hierarchy", wsId] as const,
  detail: (wsId: string | null, projectId: string) =>
    [...projectHierarchyKeys.all(wsId), projectId] as const,
};

export const projectHierarchyOptions = (
  wsId: string | null,
  projectId: string,
) =>
  queryOptions({
    queryKey: projectHierarchyKeys.detail(wsId, projectId),
    queryFn: ({ signal }) => projectHierarchyApi.get(projectId, { signal }),
    enabled: !!wsId && !!projectId,
  });
