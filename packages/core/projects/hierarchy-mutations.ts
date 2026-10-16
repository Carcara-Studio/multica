import { useMutation, useQueryClient } from "@tanstack/react-query";
import { projectHierarchyApi } from "../api/project-hierarchy";
import { useWorkspaceId } from "../hooks";
import type {
  ApplyProjectHierarchyStructureRequest,
  CreateProjectHierarchyMembershipRequest,
  ProjectHierarchyResponse,
  ReparentProjectHierarchyMembershipRequest,
} from "../types/project-hierarchy";
import { projectHierarchyKeys } from "./hierarchy-queries";

export function useCreateProjectHierarchyMembership(projectId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (data: CreateProjectHierarchyMembershipRequest) =>
      projectHierarchyApi.createMembership(projectId, data),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: projectHierarchyKeys.detail(wsId, projectId) });
    },
  });
}

export function useReparentProjectHierarchyMembership(projectId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({
      membershipId,
      data,
    }: {
      membershipId: string;
      data: ReparentProjectHierarchyMembershipRequest;
    }) => projectHierarchyApi.reparentMembership(projectId, membershipId, data),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: projectHierarchyKeys.detail(wsId, projectId) });
    },
  });
}

export function useArchiveProjectHierarchyMembership(projectId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (membershipId: string) =>
      projectHierarchyApi.archiveMembership(projectId, membershipId),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: projectHierarchyKeys.detail(wsId, projectId) });
    },
  });
}

export function useApplyProjectHierarchyStructure(projectId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (data: ApplyProjectHierarchyStructureRequest) =>
      projectHierarchyApi.applyStructure(projectId, data),
    onSuccess: (hierarchy: ProjectHierarchyResponse) => {
      qc.setQueryData(
        projectHierarchyKeys.detail(wsId, projectId),
        hierarchy,
      );
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: projectHierarchyKeys.detail(wsId, projectId) });
    },
  });
}
