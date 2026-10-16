import { useMemo } from "react";
import { ActivityIndicator, Pressable, View } from "react-native";
import { useQuery } from "@tanstack/react-query";
import { Text } from "@/components/ui/text";
import { projectHierarchyOptions } from "@/data/queries/project-hierarchy";
import { useWorkspaceStore } from "@/data/workspace-store";
import { projectHierarchyToTree } from "@/lib/project-hierarchy";
import { useT } from "@/lib/i18n";

interface Props {
  projectId: string;
  onManage: () => void;
}

export function ProjectHierarchySection({ projectId, onManage }: Props) {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { t } = useT("projects");
  const hierarchy = useQuery(projectHierarchyOptions(wsId, projectId));
  const projection = useMemo(
    () => (hierarchy.data ? projectHierarchyToTree(hierarchy.data) : null),
    [hierarchy.data],
  );

  return (
    <View>
      <View className="flex-row items-center justify-between px-4 py-2 bg-background">
        <Text className="text-xs uppercase tracking-wider text-muted-foreground font-medium">
          {t("hierarchy.title")}
        </Text>
        <Pressable
          onPress={onManage}
          className="px-2 py-1 active:bg-secondary rounded-xs"
        >
          <Text className="text-xs text-brand">
            {projection?.ok && projection.root
              ? t("hierarchy.manage")
              : t("hierarchy.setup")}
          </Text>
        </Pressable>
      </View>

      {hierarchy.isLoading ? (
        <View className="px-4 py-4 items-center">
          <ActivityIndicator size="small" />
        </View>
      ) : hierarchy.error ? (
        <View className="px-4 py-3 gap-2">
          <Text className="text-sm text-destructive">
            {t("hierarchy.load_failed")}
          </Text>
          <Pressable onPress={() => hierarchy.refetch()}>
            <Text className="text-sm text-brand">{t("hierarchy.retry")}</Text>
          </Pressable>
        </View>
      ) : projection && !projection.ok ? (
        <View className="px-4 py-3 gap-1">
          <Text className="text-sm text-destructive">
            {t("hierarchy.invalid_state")}
          </Text>
          <Text className="text-xs text-muted-foreground">
            {t("hierarchy.invalid_hint")}
          </Text>
        </View>
      ) : projection?.ok && projection.root ? (
        <View className="px-4 py-3 border-t border-border">
          <Text className="text-sm font-medium text-foreground">
            {projection.root.membership.title}
          </Text>
          <Text className="text-xs text-muted-foreground mt-0.5">
            {t("hierarchy.member_count", { count: projection.count })}
          </Text>
        </View>
      ) : (
        <View className="px-4 py-3">
          <Text className="text-sm text-muted-foreground/70">
            {t("hierarchy.not_configured")}
          </Text>
        </View>
      )}
    </View>
  );
}
