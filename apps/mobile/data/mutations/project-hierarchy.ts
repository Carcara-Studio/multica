import { useMutation, useQueryClient } from "@tanstack/react-query";
import type {
  ApplyProjectHierarchyStructureRequest,
  CreateProjectHierarchyMembershipRequest,
  ProjectHierarchyResponse,
  ReparentProjectHierarchyMembershipRequest,
} from "@multica/core/types/project-hierarchy";
import { projectHierarchyApi } from "@/data/project-hierarchy-api";
import { projectHierarchyKeys } from "@/data/queries/project-hierarchy";
import { useWorkspaceStore } from "@/data/workspace-store";

export function useCreateProjectHierarchyMembership(projectId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  return useMutation({
    retry: false,
    mutationFn: (data: CreateProjectHierarchyMembershipRequest) =>
      projectHierarchyApi.createMembership(projectId, data),
    onSettled: () =>
      qc.invalidateQueries({ queryKey: projectHierarchyKeys.detail(wsId, projectId) }),
  });
}

export function useReparentProjectHierarchyMembership(projectId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  return useMutation({
    retry: false,
    mutationFn: ({
      membershipId,
      data,
    }: {
      membershipId: string;
      data: ReparentProjectHierarchyMembershipRequest;
    }) => projectHierarchyApi.reparentMembership(projectId, membershipId, data),
    onSettled: () =>
      qc.invalidateQueries({ queryKey: projectHierarchyKeys.detail(wsId, projectId) }),
  });
}

export function useArchiveProjectHierarchyMembership(projectId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  return useMutation({
    retry: false,
    mutationFn: (membershipId: string) =>
      projectHierarchyApi.archiveMembership(projectId, membershipId),
    onSettled: () =>
      qc.invalidateQueries({ queryKey: projectHierarchyKeys.detail(wsId, projectId) }),
  });
}

export function useApplyProjectHierarchyStructure(projectId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  return useMutation({
    retry: false,
    mutationFn: (data: ApplyProjectHierarchyStructureRequest) =>
      projectHierarchyApi.applyStructure(projectId, data),
    onSuccess: (hierarchy: ProjectHierarchyResponse) => {
      qc.setQueryData(projectHierarchyKeys.detail(wsId, projectId), hierarchy);
    },
    onSettled: () =>
      qc.invalidateQueries({ queryKey: projectHierarchyKeys.detail(wsId, projectId) }),
  });
}
