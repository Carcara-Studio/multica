import type {
  ApplyProjectHierarchyStructureRequest,
  CreateProjectHierarchyMembershipRequest,
  ProjectHierarchyMembership,
  ProjectHierarchyResponse,
  ReparentProjectHierarchyMembershipRequest,
} from "@multica/core/types/project-hierarchy";
import { api } from "@/data/api";
import { parseWithFallback } from "@/lib/parse-response";
import {
  INVALID_PROJECT_HIERARCHY_MEMBERSHIP,
  INVALID_PROJECT_HIERARCHY_RESPONSE,
  ProjectHierarchyMembershipSchema,
  ProjectHierarchyResponseSchema,
} from "@/data/project-hierarchy-schemas";

type InternalRequest = <T>(
  path: string,
  init?: RequestInit & { signal?: AbortSignal },
) => Promise<T>;

/**
 * Keep hierarchy endpoints out of the large mobile ApiClient while still
 * reusing its bearer auth, workspace header, request IDs, timeout and 401
 * handling. The private transport dependency is intentionally confined here.
 */
function request<T>(
  client: object,
  path: string,
  init?: RequestInit & { signal?: AbortSignal },
): Promise<T> {
  const internalRequest = Reflect.get(client, "fetch") as InternalRequest | undefined;
  if (typeof internalRequest !== "function") {
    throw new Error("Mobile ApiClient request primitive is unavailable");
  }
  return internalRequest.call(client, path, init) as Promise<T>;
}

function projectPath(projectId: string): string {
  return `/api/projects/${encodeURIComponent(projectId)}/hierarchy`;
}

export interface MobileProjectHierarchyApi {
  get(
    projectId: string,
    opts?: { signal?: AbortSignal },
  ): Promise<ProjectHierarchyResponse>;
  createMembership(
    projectId: string,
    data: CreateProjectHierarchyMembershipRequest,
  ): Promise<ProjectHierarchyMembership>;
  reparentMembership(
    projectId: string,
    membershipId: string,
    data: ReparentProjectHierarchyMembershipRequest,
  ): Promise<ProjectHierarchyMembership>;
  archiveMembership(projectId: string, membershipId: string): Promise<void>;
  applyStructure(
    projectId: string,
    data: ApplyProjectHierarchyStructureRequest,
  ): Promise<ProjectHierarchyResponse>;
}

export function createMobileProjectHierarchyApi(
  client: object,
): MobileProjectHierarchyApi {
  return {
    async get(projectId, opts) {
      const raw = await request<unknown>(client, projectPath(projectId), {
        signal: opts?.signal,
      });
      return parseWithFallback(
        raw,
        ProjectHierarchyResponseSchema,
        INVALID_PROJECT_HIERARCHY_RESPONSE,
        { endpoint: "getProjectHierarchy" },
      );
    },

    async createMembership(projectId, data) {
      const raw = await request<unknown>(
        client,
        `${projectPath(projectId)}/memberships`,
        { method: "POST", body: JSON.stringify(data) },
      );
      return parseWithFallback(
        raw,
        ProjectHierarchyMembershipSchema,
        INVALID_PROJECT_HIERARCHY_MEMBERSHIP,
        { endpoint: "createProjectHierarchyMembership" },
      );
    },

    async reparentMembership(projectId, membershipId, data) {
      const raw = await request<unknown>(
        client,
        `${projectPath(projectId)}/memberships/${encodeURIComponent(membershipId)}/manager`,
        { method: "PUT", body: JSON.stringify(data) },
      );
      return parseWithFallback(
        raw,
        ProjectHierarchyMembershipSchema,
        INVALID_PROJECT_HIERARCHY_MEMBERSHIP,
        { endpoint: "reparentProjectHierarchyMembership" },
      );
    },

    archiveMembership(projectId, membershipId) {
      return request<void>(
        client,
        `${projectPath(projectId)}/memberships/${encodeURIComponent(membershipId)}`,
        { method: "DELETE" },
      );
    },

    async applyStructure(projectId, data) {
      const raw = await request<unknown>(client, `${projectPath(projectId)}/structure`, {
        method: "PUT",
        body: JSON.stringify(data),
      });
      return parseWithFallback(
        raw,
        ProjectHierarchyResponseSchema,
        INVALID_PROJECT_HIERARCHY_RESPONSE,
        { endpoint: "applyProjectHierarchyStructure" },
      );
    },
  };
}

export const projectHierarchyApi = createMobileProjectHierarchyApi(api);
