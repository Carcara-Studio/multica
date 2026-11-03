import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { ProjectContinuity } from "../api";

export const continuityKey = (wsId: string, projectId: string) => ["project-continuity", wsId, projectId] as const;

export function projectContinuityOptions(wsId: string, projectId: string) {
  return queryOptions({
    queryKey: continuityKey(wsId, projectId),
    queryFn: () => api.getProjectContinuity(projectId),
    retry: false,
    staleTime: 0,
  });
}

export function usePublishProjectContinuity(wsId: string, projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (data: { expected_revision: string; content: string }) => api.publishProjectContinuity(projectId, data),
    retry: false,
    onSuccess: (receipt: ProjectContinuity) => { qc.setQueryData(continuityKey(wsId, projectId), receipt); },
  });
}

export function useProjectContinuityDraft(wsId: string, projectId: string) {
  return useMutation({
    mutationKey: ["project-continuity-draft", wsId, projectId],
    mutationFn: () => api.getProjectContinuityDraft(projectId),
    retry: false,
  });
}
