import type { ApiClient } from "./client";
import { api } from "./index";
import type {
  ApplyProjectHierarchyStructureRequest,
  CreateProjectHierarchyMembershipRequest,
  ProjectHierarchyMembership,
  ProjectHierarchyResponse,
  ReparentProjectHierarchyMembershipRequest,
} from "../types/project-hierarchy";

type InternalRequest = <T>(path: string, init?: RequestInit) => Promise<T>;

/**
 * HIER-005 deliberately keeps the fork-specific hierarchy API adapter isolated
 * from the very large upstream-owned ApiClient implementation. ApiClient's
 * internal request path remains the single auth/CSRF/error boundary; this
 * adapter only supplies hierarchy paths, methods, and typed payloads.
 *
 * The cast is intentionally confined here. If upstream ever replaces the
 * internal request primitive, this module and its route-contract tests are the
 * only fork surface that needs reconciliation.
 */
function request<T>(client: ApiClient, path: string, init?: RequestInit): Promise<T> {
  const internalRequest = Reflect.get(client as object, "fetch") as InternalRequest | undefined;
  if (typeof internalRequest !== "function") {
    throw new Error("ApiClient request primitive is unavailable");
  }
  return internalRequest.call(client, path, init) as Promise<T>;
}

function projectPath(projectId: string): string {
  return `/api/projects/${encodeURIComponent(projectId)}/hierarchy`;
}

export interface ProjectHierarchyApi {
  get(projectId: string): Promise<ProjectHierarchyResponse>;
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

export function createProjectHierarchyApi(client: ApiClient): ProjectHierarchyApi {
  return {
    get(projectId) {
      return request<ProjectHierarchyResponse>(client, projectPath(projectId));
    },

    createMembership(projectId, data) {
      return request<ProjectHierarchyMembership>(
        client,
        `${projectPath(projectId)}/memberships`,
        {
          method: "POST",
          body: JSON.stringify(data),
        },
      );
    },

    reparentMembership(projectId, membershipId, data) {
      return request<ProjectHierarchyMembership>(
        client,
        `${projectPath(projectId)}/memberships/${encodeURIComponent(membershipId)}/manager`,
        {
          method: "PUT",
          body: JSON.stringify(data),
        },
      );
    },

    archiveMembership(projectId, membershipId) {
      return request<void>(
        client,
        `${projectPath(projectId)}/memberships/${encodeURIComponent(membershipId)}`,
        { method: "DELETE" },
      );
    },

    applyStructure(projectId, data) {
      return request<ProjectHierarchyResponse>(
        client,
        `${projectPath(projectId)}/structure`,
        {
          method: "PUT",
          body: JSON.stringify(data),
        },
      );
    },
  };
}

export const projectHierarchyApi = createProjectHierarchyApi(api);
