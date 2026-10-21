"use client";

import { useState } from "react";
import { useInfiniteQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { hierarchyEscalationsOptions } from "@multica/core/issues";
import { useWorkspacePaths } from "@multica/core/paths";
import { Button } from "@multica/ui/components/ui/button";
import { AppLink } from "../../navigation";
import { useT } from "../../i18n";

export function HierarchyEscalations({ issueId }: { issueId: string }) {
  const [open, setOpen] = useState(false);
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const { t } = useT("issues");
  const query = useInfiniteQuery({ ...hierarchyEscalationsOptions(wsId, issueId), enabled: open && !!wsId && !!issueId });
  const repairs: Record<string, string> = {
    decision_deleted: t(($) => $.escalations.decision_deleted),
    decision_reassigned: t(($) => $.escalations.decision_reassigned),
    dispatch_not_recorded: t(($) => $.escalations.dispatch_not_recorded),
    manager_unavailable: t(($) => $.escalations.manager_unavailable),
  };
  return (
    <details className="mt-6 rounded-md border p-3" open={open} onToggle={(event) => setOpen(event.currentTarget.open)}>
      <summary className="cursor-pointer text-body font-medium">{t(($) => $.escalations.title)}</summary>
      {open && <div className="mt-3 space-y-3">
        <Button size="sm" variant="outline" disabled={query.isFetching} onClick={() => void query.refetch()}>{t(($) => $.escalations.refresh)}</Button>
        {query.isPending && <p role="status">{t(($) => $.escalations.loading)}</p>}
        {query.isError && <p role="alert">{t(($) => $.escalations.error)}</p>}
        {query.isSuccess && !query.data.pages.some((page) => page.items.length) && <p className="text-body text-muted-foreground">{t(($) => $.escalations.empty)}</p>}
        {query.data?.pages.flatMap((page) => page.items).map((item) => <article key={item.decision_issue_id} className="space-y-2 border-t pt-3 text-body">
          <div className="flex flex-wrap gap-3">
            <AppLink href={paths.issueDetail(item.source_issue_id)}>{t(($) => $.escalations.source)}</AppLink>
            {item.decision_status !== null && <AppLink href={paths.issueDetail(item.decision_issue_id)}>{t(($) => $.escalations.decision)}</AppLink>}
            <span>{item.human_intervention ? t(($) => $.escalations.human) : t(($) => $.escalations.manager)}{item.manager_agent_id ? `: ${item.manager_agent_name || item.manager_agent_id}` : ""}</span>
            <span>{item.decision_status ?? t(($) => $.escalations.decision_deleted)}</span>
          </div>
          <p className="break-words text-muted-foreground">{item.source_agent_name || item.source_agent_id}</p>
          <p className="whitespace-pre-wrap break-words">{item.reason}</p>
          {item.context && <p className="whitespace-pre-wrap break-words text-muted-foreground">{item.context}</p>}
          {item.repair && <p role="status" className="text-muted-foreground">{repairs[item.repair] ?? item.repair}</p>}
        </article>)}
        {query.hasNextPage && <Button variant="outline" size="sm" disabled={query.isFetching} onClick={() => void query.fetchNextPage()}>{t(($) => $.escalations.more)}</Button>}
      </div>}
    </details>
  );
}
