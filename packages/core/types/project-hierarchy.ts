export interface ProjectHierarchyMembership {
  id: string;
  agent_id: string;
  manager_membership_id: string | null;
  title: string;
  created_at: string;
  updated_at: string;
}

export interface ProjectHierarchyResponse {
  memberships: ProjectHierarchyMembership[];
  total: number;
}

export interface CreateProjectHierarchyMembershipRequest {
  agent_id: string;
  manager_membership_id: string | null;
  title: string;
}

export interface ReparentProjectHierarchyMembershipRequest {
  manager_membership_id: string;
}

export interface ProjectHierarchyStructureChange {
  membership_id: string;
  manager_membership_id: string | null;
}

export interface ApplyProjectHierarchyStructureRequest {
  changes: ProjectHierarchyStructureChange[];
}
