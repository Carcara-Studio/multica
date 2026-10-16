"use client";

import { useEffect, useMemo, useState } from "react";
import {
  Crown,
  GitBranch,
  MoreHorizontal,
  Plus,
  RefreshCw,
  TriangleAlert,
  Trash2,
} from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { ApiError } from "@multica/core/api";
import {
  projectHierarchyOptions,
  projectHierarchyToTree,
  useApplyProjectHierarchyStructure,
  useArchiveProjectHierarchyMembership,
  useCreateProjectHierarchyMembership,
  useReparentProjectHierarchyMembership,
  type ProjectHierarchyTreeNode,
} from "@multica/core/projects";
import type { ProjectHierarchyMembership } from "@multica/core/types/project-hierarchy";
import { agentListOptions } from "@multica/core/workspace/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { Input } from "@multica/ui/components/ui/input";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { ActorAvatar } from "../../common/actor-avatar";
import { useT } from "../../i18n";
import {
  availableHierarchyAgents,
  canArchiveHierarchyMembership,
  hierarchyManagerCandidates,
  makeHierarchyRootRequest,
} from "./project-hierarchy-view-model";

type HierarchyAction =
  | { kind: "add"; managerMembershipId: string | null }
  | { kind: "move"; membershipId: string }
  | null;

function hierarchyMutationMessage(
  error: unknown,
  fallback: string,
  stale: string,
): string {
  if (error instanceof ApiError) {
    if (error.status === 404) return stale;
    if ((error.status === 400 || error.status === 409) && error.message) {
      return error.message;
    }
  }
  return fallback;
}

export function ProjectHierarchySection({ projectId }: { projectId: string }) {
  const { t } = useT("projects");
  const wsId = useWorkspaceId();
  const [open, setOpen] = useState(false);
  const hierarchyQuery = useQuery(projectHierarchyOptions(wsId, projectId));
  const projection = useMemo(
    () =>
      hierarchyQuery.data
        ? projectHierarchyToTree(hierarchyQuery.data)
        : null,
    [hierarchyQuery.data],
  );

  if (hierarchyQuery.isLoading) {
    return (
      <div className="space-y-2 px-2">
        <div className="text-caption font-medium">
          {t(($) => $.hierarchy.section_header)}
        </div>
        <Skeleton className="h-8 w-full" />
      </div>
    );
  }

  if (hierarchyQuery.isError) {
    return (
      <div className="space-y-2 px-2">
        <div className="text-caption font-medium">
          {t(($) => $.hierarchy.section_header)}
        </div>
        <div className="rounded-md border border-destructive/30 bg-destructive/5 p-2 text-caption text-destructive">
          {t(($) => $.hierarchy.load_failed)}
        </div>
        <Button
          variant="ghost"
          size="sm"
          onClick={() => void hierarchyQuery.refetch()}
        >
          <RefreshCw className="size-3.5" />
          {t(($) => $.hierarchy.retry)}
        </Button>
      </div>
    );
  }

  const malformed = projection && !projection.ok;
  const root = projection?.ok ? projection.root : null;
  const count = projection?.ok ? projection.count : hierarchyQuery.data?.total ?? 0;

  return (
    <div className="px-2">
      <div className="mb-2 flex items-center justify-between gap-2">
        <span className="text-caption font-medium">
          {t(($) => $.hierarchy.section_header)}
        </span>
        <Button variant="ghost" size="sm" onClick={() => setOpen(true)}>
          {root
            ? t(($) => $.hierarchy.manage)
            : t(($) => $.hierarchy.setup)}
        </Button>
      </div>

      <button
        type="button"
        onClick={() => setOpen(true)}
        className="flex w-full items-center gap-2 rounded-md px-2 py-2 text-left transition-colors hover:bg-accent/60"
      >
        {malformed ? (
          <TriangleAlert className="size-4 shrink-0 text-warning" />
        ) : (
          <GitBranch className="size-4 shrink-0 text-muted-foreground" />
        )}
        <span className="min-w-0 flex-1">
          {malformed ? (
            <>
              <span className="block truncate text-caption font-medium text-warning">
                {t(($) => $.hierarchy.invalid_state)}
              </span>
              <span className="block truncate text-micro text-muted-foreground">
                {t(($) => $.hierarchy.open_to_refresh)}
              </span>
            </>
          ) : root ? (
            <>
              <span className="block truncate text-caption font-medium">
                {root.membership.title}
              </span>
              <span className="block truncate text-micro text-muted-foreground">
                {t(($) => $.hierarchy.member_count, { count })}
              </span>
            </>
          ) : (
            <>
              <span className="block truncate text-caption font-medium text-muted-foreground">
                {t(($) => $.hierarchy.not_configured)}
              </span>
              <span className="block truncate text-micro text-muted-foreground">
                {t(($) => $.hierarchy.empty_hint)}
              </span>
            </>
          )}
        </span>
      </button>

      <ProjectHierarchyDialog
        projectId={projectId}
        open={open}
        onOpenChange={setOpen}
      />
    </div>
  );
}

function ProjectHierarchyDialog({
  projectId,
  open,
  onOpenChange,
}: {
  projectId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useT("projects");
  const wsId = useWorkspaceId();
  const hierarchyQuery = useQuery(projectHierarchyOptions(wsId, projectId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const createMembership = useCreateProjectHierarchyMembership(projectId);
  const reparentMembership = useReparentProjectHierarchyMembership(projectId);
  const archiveMembership = useArchiveProjectHierarchyMembership(projectId);
  const applyStructure = useApplyProjectHierarchyStructure(projectId);
  const [action, setAction] = useState<HierarchyAction>(null);
  const [selectedAgentId, setSelectedAgentId] = useState("");
  const [title, setTitle] = useState("");
  const [selectedManagerId, setSelectedManagerId] = useState("");

  const response = hierarchyQuery.data;
  const projection = useMemo(
    () => (response ? projectHierarchyToTree(response) : null),
    [response],
  );
  const memberships = response?.memberships ?? [];
  const availableAgents = useMemo(
    () => availableHierarchyAgents(agents, memberships),
    [agents, memberships],
  );

  useEffect(() => {
    if (!open) {
      setAction(null);
      setSelectedAgentId("");
      setTitle("");
      setSelectedManagerId("");
    }
  }, [open]);

  useEffect(() => {
    if (!action) return;
    setSelectedAgentId("");
    setTitle("");
    setSelectedManagerId("");
  }, [action]);

  const selectedAgent = availableAgents.find(
    (agent) => agent.id === selectedAgentId,
  );

  const showMutationError = (error: unknown) => {
    const message = hierarchyMutationMessage(
      error,
      t(($) => $.hierarchy.write_failed),
      t(($) => $.hierarchy.stale_state),
    );
    // Structural writes are server-authoritative. Always reconcile after an
    // error because a network/5xx response does not prove whether the server
    // committed the mutation.
    void hierarchyQuery.refetch();
    return message;
  };

  const [inlineError, setInlineError] = useState<string | null>(null);

  useEffect(() => {
    if (!action) setInlineError(null);
  }, [action]);

  const handleCreate = () => {
    if (!action || action.kind !== "add" || !selectedAgent || !title.trim()) {
      return;
    }
    setInlineError(null);
    createMembership.mutate(
      {
        agent_id: selectedAgent.id,
        manager_membership_id: action.managerMembershipId,
        title: title.trim(),
      },
      {
        onSuccess: () => setAction(null),
        onError: (error) => setInlineError(showMutationError(error)),
      },
    );
  };

  const handleMove = () => {
    if (!action || action.kind !== "move" || !selectedManagerId) return;
    const moving = memberships.find(
      (membership) => membership.id === action.membershipId,
    );
    if (!moving || moving.manager_membership_id === selectedManagerId) return;

    setInlineError(null);
    reparentMembership.mutate(
      {
        membershipId: moving.id,
        data: { manager_membership_id: selectedManagerId },
      },
      {
        onSuccess: () => setAction(null),
        onError: (error) => setInlineError(showMutationError(error)),
      },
    );
  };

  const handleMakeRoot = (membershipId: string) => {
    const request = makeHierarchyRootRequest(memberships, membershipId);
    if (!request) return;
    setInlineError(null);
    applyStructure.mutate(request, {
      onError: (error) => setInlineError(showMutationError(error)),
    });
  };

  const handleArchive = (membershipId: string) => {
    if (!canArchiveHierarchyMembership(memberships, membershipId)) return;
    setInlineError(null);
    archiveMembership.mutate(membershipId, {
      onError: (error) => setInlineError(showMutationError(error)),
    });
  };

  const pending =
    createMembership.isPending ||
    reparentMembership.isPending ||
    archiveMembership.isPending ||
    applyStructure.isPending;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t(($) => $.hierarchy.dialog_title)}</DialogTitle>
          <DialogDescription>
            {t(($) => $.hierarchy.dialog_description)}
          </DialogDescription>
        </DialogHeader>

        {hierarchyQuery.isLoading ? (
          <div className="space-y-2 py-3">
            <Skeleton className="h-10 w-full" />
            <Skeleton className="h-10 w-4/5" />
          </div>
        ) : hierarchyQuery.isError ? (
          <div className="space-y-3 rounded-lg border border-destructive/30 bg-destructive/5 p-3">
            <div className="flex items-start gap-2 text-caption text-destructive">
              <TriangleAlert className="mt-0.5 size-4 shrink-0" />
              <span>{t(($) => $.hierarchy.load_failed)}</span>
            </div>
            <Button
              variant="outline"
              size="sm"
              onClick={() => void hierarchyQuery.refetch()}
            >
              <RefreshCw className="size-3.5" />
              {t(($) => $.hierarchy.retry)}
            </Button>
          </div>
        ) : projection && !projection.ok ? (
          <div className="space-y-3 rounded-lg border border-warning/30 bg-warning/5 p-3">
            <div className="flex items-start gap-2 text-caption text-warning">
              <TriangleAlert className="mt-0.5 size-4 shrink-0" />
              <div>
                <div className="font-medium">
                  {t(($) => $.hierarchy.invalid_state)}
                </div>
                <div className="mt-1 text-muted-foreground">
                  {projection.message}
                </div>
              </div>
            </div>
            <Button
              variant="outline"
              size="sm"
              onClick={() => void hierarchyQuery.refetch()}
            >
              <RefreshCw className="size-3.5" />
              {t(($) => $.hierarchy.refresh)}
            </Button>
          </div>
        ) : projection?.ok && projection.root ? (
          <div className="max-h-[50vh] overflow-y-auto rounded-lg border p-2">
            <HierarchyNode
              node={projection.root}
              memberships={memberships}
              agents={agents}
              pending={pending}
              onAdd={(managerMembershipId) =>
                setAction({ kind: "add", managerMembershipId })
              }
              onMove={(membershipId) =>
                setAction({ kind: "move", membershipId })
              }
              onMakeRoot={handleMakeRoot}
              onArchive={handleArchive}
            />
          </div>
        ) : (
          <div className="rounded-lg border border-dashed p-5 text-center">
            <GitBranch className="mx-auto size-5 text-muted-foreground" />
            <div className="mt-2 text-body font-medium">
              {t(($) => $.hierarchy.empty_title)}
            </div>
            <div className="mt-1 text-caption text-muted-foreground">
              {t(($) => $.hierarchy.empty_description)}
            </div>
            <Button
              className="mt-3"
              size="sm"
              onClick={() =>
                setAction({ kind: "add", managerMembershipId: null })
              }
            >
              <Plus className="size-3.5" />
              {t(($) => $.hierarchy.add_root)}
            </Button>
          </div>
        )}

        {action?.kind === "add" && (
          <div className="space-y-3 rounded-lg border bg-muted/20 p-3">
            <div>
              <div className="text-body font-medium">
                {action.managerMembershipId
                  ? t(($) => $.hierarchy.add_report)
                  : t(($) => $.hierarchy.add_root)}
              </div>
              <div className="text-caption text-muted-foreground">
                {t(($) => $.hierarchy.choose_agent)}
              </div>
            </div>

            {availableAgents.length === 0 ? (
              <div className="text-caption text-muted-foreground">
                {t(($) => $.hierarchy.no_available_agents)}
              </div>
            ) : (
              <div className="grid max-h-36 grid-cols-1 gap-1 overflow-y-auto sm:grid-cols-2">
                {availableAgents.map((agent) => (
                  <button
                    type="button"
                    key={agent.id}
                    onClick={() => {
                      setSelectedAgentId(agent.id);
                      setTitle(agent.name);
                    }}
                    className={`flex items-center gap-2 rounded-md border px-2 py-1.5 text-left text-caption transition-colors ${
                      selectedAgentId === agent.id
                        ? "border-primary bg-primary/5"
                        : "hover:bg-accent"
                    }`}
                  >
                    <ActorAvatar actorType="agent" actorId={agent.id} size="sm" />
                    <span className="truncate">{agent.name}</span>
                  </button>
                ))}
              </div>
            )}

            <div>
              <label className="mb-1 block text-caption font-medium">
                {t(($) => $.hierarchy.role_title)}
              </label>
              <Input
                value={title}
                onChange={(event) => setTitle(event.target.value)}
                placeholder={t(($) => $.hierarchy.role_title_placeholder)}
                disabled={!selectedAgent || pending}
              />
              <div className="mt-1 text-micro text-muted-foreground">
                {t(($) => $.hierarchy.role_title_hint)}
              </div>
            </div>

            {inlineError && (
              <div className="flex items-start gap-2 text-caption text-destructive">
                <TriangleAlert className="mt-0.5 size-3.5 shrink-0" />
                <span>{inlineError}</span>
              </div>
            )}

            <div className="flex justify-end gap-2">
              <Button
                variant="ghost"
                size="sm"
                disabled={pending}
                onClick={() => setAction(null)}
              >
                {t(($) => $.hierarchy.cancel)}
              </Button>
              <Button
                size="sm"
                disabled={!selectedAgent || !title.trim() || pending}
                onClick={handleCreate}
              >
                {t(($) => $.hierarchy.save)}
              </Button>
            </div>
          </div>
        )}

        {action?.kind === "move" && (
          <MoveMembershipPanel
            membershipId={action.membershipId}
            memberships={memberships}
            selectedManagerId={selectedManagerId}
            onSelectManager={setSelectedManagerId}
            pending={pending}
            error={inlineError}
            onCancel={() => setAction(null)}
            onSave={handleMove}
          />
        )}

        {!action && inlineError && (
          <div className="flex items-start gap-2 rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-caption text-destructive">
            <TriangleAlert className="mt-0.5 size-3.5 shrink-0" />
            <span>{inlineError}</span>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}

function HierarchyNode({
  node,
  memberships,
  agents,
  pending,
  depth = 0,
  onAdd,
  onMove,
  onMakeRoot,
  onArchive,
}: {
  node: ProjectHierarchyTreeNode;
  memberships: ProjectHierarchyMembership[];
  agents: Array<{ id: string; name: string; archived_at?: string | null }>;
  pending: boolean;
  depth?: number;
  onAdd: (managerMembershipId: string) => void;
  onMove: (membershipId: string) => void;
  onMakeRoot: (membershipId: string) => void;
  onArchive: (membershipId: string) => void;
}) {
  const { t } = useT("projects");
  const { membership } = node;
  const agent = agents.find((entry) => entry.id === membership.agent_id);
  const isRoot = membership.manager_membership_id === null;
  const canArchive = canArchiveHierarchyMembership(memberships, membership.id);

  return (
    <div>
      <div
        className="flex min-h-10 items-center gap-2 rounded-md px-2 py-1.5 hover:bg-accent/50"
        style={{ paddingInlineStart: `${8 + depth * 20}px` }}
      >
        <div className="relative shrink-0">
          {agent ? (
            <ActorAvatar actorType="agent" actorId={agent.id} size="sm" />
          ) : (
            <div className="flex size-6 items-center justify-center rounded-full bg-muted text-micro text-muted-foreground">
              ?
            </div>
          )}
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-1.5">
            <span className="truncate text-caption font-medium">
              {membership.title}
            </span>
            {isRoot && (
              <span className="rounded bg-muted px-1.5 py-0.5 text-micro text-muted-foreground">
                {t(($) => $.hierarchy.root_badge)}
              </span>
            )}
          </div>
          <div className="truncate text-micro text-muted-foreground">
            {agent?.name ?? t(($) => $.hierarchy.agent_unavailable)}
          </div>
        </div>
        <DropdownMenu>
          <DropdownMenuTrigger
            disabled={pending}
            render={
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label={t(($) => $.hierarchy.actions)}
              >
                <MoreHorizontal className="size-4" />
              </Button>
            }
          />
          <DropdownMenuContent align="end">
            <DropdownMenuItem onClick={() => onAdd(membership.id)}>
              <Plus className="size-3.5" />
              {t(($) => $.hierarchy.add_report)}
            </DropdownMenuItem>
            {!isRoot && (
              <>
                <DropdownMenuItem onClick={() => onMove(membership.id)}>
                  <GitBranch className="size-3.5" />
                  {t(($) => $.hierarchy.move_under)}
                </DropdownMenuItem>
                <DropdownMenuItem onClick={() => onMakeRoot(membership.id)}>
                  <Crown className="size-3.5" />
                  {t(($) => $.hierarchy.make_root)}
                </DropdownMenuItem>
                {canArchive && (
                  <>
                    <DropdownMenuSeparator />
                    <DropdownMenuItem
                      variant="destructive"
                      onClick={() => onArchive(membership.id)}
                    >
                      <Trash2 className="size-3.5" />
                      {t(($) => $.hierarchy.archive)}
                    </DropdownMenuItem>
                  </>
                )}
              </>
            )}
          </DropdownMenuContent>
        </DropdownMenu>
      </div>

      {node.children.map((child) => (
        <HierarchyNode
          key={child.membership.id}
          node={child}
          memberships={memberships}
          agents={agents}
          pending={pending}
          depth={depth + 1}
          onAdd={onAdd}
          onMove={onMove}
          onMakeRoot={onMakeRoot}
          onArchive={onArchive}
        />
      ))}
    </div>
  );
}

function MoveMembershipPanel({
  membershipId,
  memberships,
  selectedManagerId,
  onSelectManager,
  pending,
  error,
  onCancel,
  onSave,
}: {
  membershipId: string;
  memberships: ProjectHierarchyMembership[];
  selectedManagerId: string;
  onSelectManager: (id: string) => void;
  pending: boolean;
  error: string | null;
  onCancel: () => void;
  onSave: () => void;
}) {
  const { t } = useT("projects");
  const moving = memberships.find((entry) => entry.id === membershipId);
  const candidates = hierarchyManagerCandidates(memberships, membershipId).filter(
    (candidate) => candidate.id !== moving?.manager_membership_id,
  );

  return (
    <div className="space-y-3 rounded-lg border bg-muted/20 p-3">
      <div>
        <div className="text-body font-medium">
          {t(($) => $.hierarchy.move_under)}
        </div>
        <div className="text-caption text-muted-foreground">
          {t(($) => $.hierarchy.choose_manager)}
        </div>
      </div>

      {candidates.length === 0 ? (
        <div className="text-caption text-muted-foreground">
          {t(($) => $.hierarchy.no_move_targets)}
        </div>
      ) : (
        <div className="grid max-h-40 grid-cols-1 gap-1 overflow-y-auto sm:grid-cols-2">
          {candidates.map((candidate) => (
            <button
              type="button"
              key={candidate.id}
              onClick={() => onSelectManager(candidate.id)}
              className={`rounded-md border px-2 py-1.5 text-left text-caption transition-colors ${
                selectedManagerId === candidate.id
                  ? "border-primary bg-primary/5"
                  : "hover:bg-accent"
              }`}
            >
              <span className="block truncate font-medium">
                {candidate.title}
              </span>
            </button>
          ))}
        </div>
      )}

      {error && (
        <div className="flex items-start gap-2 text-caption text-destructive">
          <TriangleAlert className="mt-0.5 size-3.5 shrink-0" />
          <span>{error}</span>
        </div>
      )}

      <div className="flex justify-end gap-2">
        <Button variant="ghost" size="sm" disabled={pending} onClick={onCancel}>
          {t(($) => $.hierarchy.cancel)}
        </Button>
        <Button
          size="sm"
          disabled={!selectedManagerId || pending}
          onClick={onSave}
        >
          {t(($) => $.hierarchy.save)}
        </Button>
      </div>
    </div>
  );
}
