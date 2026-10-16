import { useMemo, useState } from "react";
import {
  ActionSheetIOS,
  ActivityIndicator,
  Alert,
  Pressable,
  ScrollView,
  View,
} from "react-native";
import { Stack, useLocalSearchParams } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import type { Agent } from "@multica/core/types";
import type { ProjectHierarchyMembership } from "@multica/core/types/project-hierarchy";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { agentListOptions } from "@/data/queries/agents";
import { projectHierarchyOptions } from "@/data/queries/project-hierarchy";
import {
  useApplyProjectHierarchyStructure,
  useArchiveProjectHierarchyMembership,
  useCreateProjectHierarchyMembership,
  useReparentProjectHierarchyMembership,
} from "@/data/mutations/project-hierarchy";
import { ApiError } from "@/data/api";
import { useWorkspaceStore } from "@/data/workspace-store";
import {
  buildProjectHierarchyRootPromotion,
  flattenProjectHierarchy,
  projectHierarchyDescendantIds,
  projectHierarchyLeafIds,
  projectHierarchyToTree,
} from "@/lib/project-hierarchy";
import { useT } from "@/lib/i18n";

type EditMode =
  | {
      kind: "add";
      parentId: string | null;
      agentId: string | null;
      title: string;
    }
  | {
      kind: "move";
      membershipId: string;
      managerId: string | null;
    }
  | null;

export default function ProjectHierarchyRoute() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { t } = useT("projects");
  const hierarchy = useQuery(projectHierarchyOptions(wsId, id));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const createMembership = useCreateProjectHierarchyMembership(id);
  const reparentMembership = useReparentProjectHierarchyMembership(id);
  const archiveMembership = useArchiveProjectHierarchyMembership(id);
  const applyStructure = useApplyProjectHierarchyStructure(id);
  const [mode, setMode] = useState<EditMode>(null);
  const [writeError, setWriteError] = useState<string | null>(null);

  const projection = useMemo(
    () => (hierarchy.data ? projectHierarchyToTree(hierarchy.data) : null),
    [hierarchy.data],
  );
  const memberships = hierarchy.data?.memberships ?? [];
  const rows = useMemo(
    () => (projection?.ok ? flattenProjectHierarchy(projection.root) : []),
    [projection],
  );
  const leafIds = useMemo(
    () => projectHierarchyLeafIds(memberships),
    [memberships],
  );
  const agentById = useMemo(
    () => new Map(agents.map((agent) => [agent.id, agent])),
    [agents],
  );
  const usedAgentIds = useMemo(
    () => new Set(memberships.map((membership) => membership.agent_id)),
    [memberships],
  );
  const availableAgents = useMemo(
    () =>
      agents
        .filter((agent) => !agent.archived_at && !usedAgentIds.has(agent.id))
        .sort((a, b) => a.name.localeCompare(b.name)),
    [agents, usedAgentIds],
  );

  const busy =
    createMembership.isPending ||
    reparentMembership.isPending ||
    archiveMembership.isPending ||
    applyStructure.isPending;

  const handleWriteError = (error: unknown) => {
    void hierarchy.refetch();
    if (error instanceof ApiError && error.status === 409) {
      setWriteError(t("hierarchy.conflict"));
      return;
    }
    if (error instanceof ApiError && error.status === 404) {
      setWriteError(t("hierarchy.stale_state"));
      return;
    }
    setWriteError(
      error instanceof Error && error.message
        ? error.message
        : t("hierarchy.write_failed"),
    );
  };

  const startAdd = (parentId: string | null) => {
    setWriteError(null);
    setMode({ kind: "add", parentId, agentId: null, title: "" });
  };

  const saveAdd = () => {
    if (mode?.kind !== "add" || !mode.agentId || !mode.title.trim()) return;
    createMembership.mutate(
      {
        agent_id: mode.agentId,
        manager_membership_id: mode.parentId,
        title: mode.title.trim(),
      },
      {
        onSuccess: () => {
          setMode(null);
          setWriteError(null);
        },
        onError: handleWriteError,
      },
    );
  };

  const saveMove = () => {
    if (mode?.kind !== "move" || !mode.managerId) return;
    reparentMembership.mutate(
      {
        membershipId: mode.membershipId,
        data: { manager_membership_id: mode.managerId },
      },
      {
        onSuccess: () => {
          setMode(null);
          setWriteError(null);
        },
        onError: handleWriteError,
      },
    );
  };

  const makeRoot = (membershipId: string) => {
    const request = buildProjectHierarchyRootPromotion(memberships, membershipId);
    if (request.changes.length === 0) return;
    applyStructure.mutate(request, {
      onSuccess: () => setWriteError(null),
      onError: handleWriteError,
    });
  };

  const archive = (membership: ProjectHierarchyMembership) => {
    Alert.alert(
      t("hierarchy.archive_title"),
      t("hierarchy.archive_message", { title: membership.title }),
      [
        { text: t("common:actions.cancel"), style: "cancel" },
        {
          text: t("hierarchy.archive"),
          style: "destructive",
          onPress: () =>
            archiveMembership.mutate(membership.id, {
              onSuccess: () => setWriteError(null),
              onError: handleWriteError,
            }),
        },
      ],
    );
  };

  const openActions = (membership: ProjectHierarchyMembership) => {
    const isRoot = membership.manager_membership_id === null;
    const actions: Array<
      | "cancel"
      | "add"
      | "move"
      | "root"
      | "archive"
    > = ["cancel", "add"];
    if (!isRoot) {
      actions.push("move", "root");
      if (leafIds.has(membership.id)) actions.push("archive");
    }

    const labels = actions.map((action) =>
      action === "cancel"
        ? t("common:actions.cancel")
        : action === "add"
          ? t("hierarchy.add_report")
          : action === "move"
            ? t("hierarchy.move_under")
            : action === "root"
              ? t("hierarchy.make_root")
              : t("hierarchy.archive"),
    );
    const destructiveButtonIndex = actions.indexOf("archive");
    ActionSheetIOS.showActionSheetWithOptions(
      {
        options: labels,
        cancelButtonIndex: 0,
        destructiveButtonIndex:
          destructiveButtonIndex >= 0 ? destructiveButtonIndex : undefined,
      },
      (index) => {
        const action = actions[index];
        if (action === "add") startAdd(membership.id);
        if (action === "move") {
          setWriteError(null);
          setMode({
            kind: "move",
            membershipId: membership.id,
            managerId: membership.manager_membership_id,
          });
        }
        if (action === "root") makeRoot(membership.id);
        if (action === "archive") archive(membership);
      },
    );
  };

  const managerCandidates = useMemo(() => {
    if (mode?.kind !== "move") return [];
    const descendants = projectHierarchyDescendantIds(
      memberships,
      mode.membershipId,
    );
    return memberships.filter(
      (membership) =>
        membership.id !== mode.membershipId && !descendants.has(membership.id),
    );
  }, [memberships, mode]);

  return (
    <View className="flex-1 bg-background">
      <Stack.Screen
        options={{
          title: t("hierarchy.screen_title"),
          headerBackTitle: t("common:actions.back"),
        }}
      />

      {hierarchy.isLoading ? (
        <View className="flex-1 items-center justify-center">
          <ActivityIndicator />
        </View>
      ) : hierarchy.error ? (
        <View className="flex-1 items-center justify-center px-6 gap-3">
          <Text className="text-sm text-destructive text-center">
            {t("hierarchy.load_failed")}
          </Text>
          <Button variant="outline" onPress={() => hierarchy.refetch()}>
            <Text>{t("hierarchy.retry")}</Text>
          </Button>
        </View>
      ) : projection && !projection.ok ? (
        <View className="flex-1 items-center justify-center px-6 gap-3">
          <Text className="text-base font-medium text-destructive text-center">
            {t("hierarchy.invalid_state")}
          </Text>
          <Text className="text-sm text-muted-foreground text-center">
            {t("hierarchy.invalid_hint")}
          </Text>
          <Button variant="outline" onPress={() => hierarchy.refetch()}>
            <Text>{t("hierarchy.refresh")}</Text>
          </Button>
        </View>
      ) : (
        <ScrollView
          contentContainerClassName="pb-10"
          keyboardShouldPersistTaps="handled"
        >
          <View className="px-4 py-4 gap-1">
            <Text className="text-sm text-muted-foreground">
              {t("hierarchy.description")}
            </Text>
            {writeError ? (
              <Text className="text-sm text-destructive mt-2">{writeError}</Text>
            ) : null}
          </View>

          {rows.length === 0 ? (
            <View className="px-4 py-6 gap-3 items-start border-t border-border">
              <Text className="text-sm text-muted-foreground">
                {t("hierarchy.not_configured")}
              </Text>
              <Button onPress={() => startAdd(null)} disabled={busy}>
                <Text>{t("hierarchy.add_root")}</Text>
              </Button>
            </View>
          ) : (
            <View className="border-t border-border">
              {rows.map(({ membership, depth }) => (
                <HierarchyRow
                  key={membership.id}
                  membership={membership}
                  depth={depth}
                  agent={agentById.get(membership.agent_id)}
                  onActions={() => openActions(membership)}
                />
              ))}
            </View>
          )}

          {mode?.kind === "add" ? (
            <AddMembershipForm
              title={mode.title}
              selectedAgentId={mode.agentId}
              agents={availableAgents}
              busy={busy}
              onTitleChange={(title) => setMode({ ...mode, title })}
              onAgentChange={(agentId) => setMode({ ...mode, agentId })}
              onCancel={() => setMode(null)}
              onSave={saveAdd}
            />
          ) : null}

          {mode?.kind === "move" ? (
            <MoveMembershipForm
              selectedManagerId={mode.managerId}
              memberships={managerCandidates}
              agentById={agentById}
              busy={busy}
              onManagerChange={(managerId) => setMode({ ...mode, managerId })}
              onCancel={() => setMode(null)}
              onSave={saveMove}
            />
          ) : null}
        </ScrollView>
      )}
    </View>
  );
}

function HierarchyRow({
  membership,
  depth,
  agent,
  onActions,
}: {
  membership: ProjectHierarchyMembership;
  depth: number;
  agent: Agent | undefined;
  onActions: () => void;
}) {
  const { t } = useT("projects");
  return (
    <Pressable
      onPress={onActions}
      className="flex-row items-center gap-3 py-3 pr-4 border-b border-border active:bg-secondary"
      style={{ paddingLeft: 16 + depth * 20 }}
    >
      <View className="flex-1 min-w-0">
        <View className="flex-row items-center gap-2">
          <Text className="text-sm font-medium text-foreground" numberOfLines={1}>
            {membership.title}
          </Text>
          {membership.manager_membership_id === null ? (
            <Text className="text-[11px] text-brand">{t("hierarchy.root")}</Text>
          ) : null}
        </View>
        <Text className="text-xs text-muted-foreground" numberOfLines={1}>
          {agent?.name ?? t("hierarchy.agent_unavailable")}
        </Text>
      </View>
      <Text className="text-xs text-brand">{t("hierarchy.actions")}</Text>
    </Pressable>
  );
}

function AddMembershipForm({
  title,
  selectedAgentId,
  agents,
  busy,
  onTitleChange,
  onAgentChange,
  onCancel,
  onSave,
}: {
  title: string;
  selectedAgentId: string | null;
  agents: Agent[];
  busy: boolean;
  onTitleChange: (title: string) => void;
  onAgentChange: (agentId: string) => void;
  onCancel: () => void;
  onSave: () => void;
}) {
  const { t } = useT("projects");
  return (
    <View className="mt-4 border-y border-border px-4 py-4 gap-3">
      <Text className="text-base font-medium">{t("hierarchy.add_position")}</Text>
      <Input
        value={title}
        onChangeText={onTitleChange}
        placeholder={t("hierarchy.role_placeholder")}
        autoCapitalize="words"
      />
      <Text className="text-xs uppercase tracking-wider text-muted-foreground font-medium">
        {t("hierarchy.choose_agent")}
      </Text>
      {agents.length === 0 ? (
        <Text className="text-sm text-muted-foreground">
          {t("hierarchy.no_available_agents")}
        </Text>
      ) : (
        agents.map((agent) => (
          <Pressable
            key={agent.id}
            onPress={() => onAgentChange(agent.id)}
            className="flex-row items-center justify-between py-2"
          >
            <Text className="text-sm text-foreground">{agent.name}</Text>
            <Text className="text-xs text-brand">
              {selectedAgentId === agent.id ? t("hierarchy.selected") : ""}
            </Text>
          </Pressable>
        ))
      )}
      <View className="flex-row gap-2 justify-end">
        <Button variant="outline" onPress={onCancel} disabled={busy}>
          <Text>{t("common:actions.cancel")}</Text>
        </Button>
        <Button
          onPress={onSave}
          disabled={busy || !selectedAgentId || !title.trim()}
        >
          <Text>{t("common:actions.save")}</Text>
        </Button>
      </View>
    </View>
  );
}

function MoveMembershipForm({
  selectedManagerId,
  memberships,
  agentById,
  busy,
  onManagerChange,
  onCancel,
  onSave,
}: {
  selectedManagerId: string | null;
  memberships: ProjectHierarchyMembership[];
  agentById: Map<string, Agent>;
  busy: boolean;
  onManagerChange: (managerId: string) => void;
  onCancel: () => void;
  onSave: () => void;
}) {
  const { t } = useT("projects");
  return (
    <View className="mt-4 border-y border-border px-4 py-4 gap-3">
      <Text className="text-base font-medium">{t("hierarchy.choose_manager")}</Text>
      {memberships.map((membership) => (
        <Pressable
          key={membership.id}
          onPress={() => onManagerChange(membership.id)}
          className="py-2"
        >
          <Text className="text-sm text-foreground">{membership.title}</Text>
          <Text className="text-xs text-muted-foreground">
            {agentById.get(membership.agent_id)?.name ?? t("hierarchy.agent_unavailable")}
          </Text>
          {selectedManagerId === membership.id ? (
            <Text className="text-xs text-brand">{t("hierarchy.selected")}</Text>
          ) : null}
        </Pressable>
      ))}
      <View className="flex-row gap-2 justify-end">
        <Button variant="outline" onPress={onCancel} disabled={busy}>
          <Text>{t("common:actions.cancel")}</Text>
        </Button>
        <Button onPress={onSave} disabled={busy || !selectedManagerId}>
          <Text>{t("common:actions.save")}</Text>
        </Button>
      </View>
    </View>
  );
}
