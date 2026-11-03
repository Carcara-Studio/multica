"use client";

import { useId, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ApiError, validContinuityContent, type ProjectContinuity } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { projectContinuityOptions, useProjectContinuityDraft, usePublishProjectContinuity } from "@multica/core/projects/continuity";
import { Button } from "@multica/ui/components/ui/button";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from "@multica/ui/components/ui/dialog";
import { AppLink } from "../../navigation";
import { useT } from "../../i18n";

export function ProjectContinuitySection({ projectId }: { projectId: string }) {
  const wsId = useWorkspaceId();
  return <ContinuitySection key={`${wsId}:${projectId}`} wsId={wsId} projectId={projectId} />;
}

function ContinuitySection({ wsId, projectId }: { wsId: string; projectId: string }) {
  const { t } = useT("projects");
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const query = useQuery(projectContinuityOptions(wsId, projectId));
  const current = query.data;

  async function reload() {
    const result = await query.refetch();
    if (result.error || !result.data) throw new Error("Continuity reload failed");
    return result.data;
  }

  return (
    <section className="space-y-2 px-2" aria-label={t(($) => $.continuity.title)}>
      <div className="flex items-center justify-between gap-2">
        <h3 className="text-caption font-medium">{t(($) => $.continuity.title)}</h3>
        {current && (
          <Dialog open={open} onOpenChange={(next) => { if (!busy) setOpen(next); }}>
            <DialogTrigger render={<Button size="sm" variant="outline" />}>
              {t(($) => $.continuity.review)}
            </DialogTrigger>
            <DialogContent className="flex max-h-[85vh] flex-col sm:max-w-2xl">
              <DialogHeader>
                <DialogTitle>{t(($) => $.continuity.review)}</DialogTitle>
                <DialogDescription>{t(($) => $.continuity.scope)}</DialogDescription>
              </DialogHeader>
              {open && <ContinuityEditor initial={current} wsId={wsId} projectId={projectId} reload={reload} onBusy={setBusy} onPublished={() => setOpen(false)} />}
            </DialogContent>
          </Dialog>
        )}
      </div>
      {query.isLoading && <p role="status" className="text-caption text-muted-foreground">{t(($) => $.continuity.loading)}</p>}
      {query.isError && (
        <div className="space-y-2">
          <p role="alert" className="text-caption text-destructive">{t(($) => $.continuity.load_failed)}</p>
          <Button size="sm" variant="ghost" disabled={query.isFetching} onClick={() => void query.refetch()}>{t(($) => $.continuity.retry)}</Button>
        </div>
      )}
      {current && <p className="line-clamp-4 whitespace-pre-wrap break-words text-caption text-muted-foreground">{current.content || t(($) => $.continuity.empty)}</p>}
    </section>
  );
}

function ContinuityEditor({ initial, wsId, projectId, reload, onBusy, onPublished }: {
  initial: ProjectContinuity; wsId: string; projectId: string;
  reload: () => Promise<ProjectContinuity>; onBusy: (busy: boolean) => void; onPublished: () => void;
}) {
  const { t } = useT("projects");
  const paths = useWorkspacePaths();
  const fieldId = useId();
  // Capture the opening revision. Background refetches must never rebase local edits.
  const [baseline, setBaseline] = useState(initial);
  const [content, setContent] = useState(initial.content);
  const [reloading, setReloading] = useState(false);
  const [reloadFailed, setReloadFailed] = useState(false);
  const publish = usePublishProjectContinuity(wsId, projectId);
  const draft = useProjectContinuityDraft(wsId, projectId);
  const dirty = content !== baseline.content;
  const staleDraft = !!draft.data && draft.data.current.revision !== baseline.revision;
  const conflict = (publish.error instanceof ApiError && publish.error.status === 409) || staleDraft;
  const pending = publish.isPending || reloading;
  const source = draft.data?.source;
  const valid = validContinuityContent(content);

  async function publishContent() {
    onBusy(true);
    try {
      await publish.mutateAsync({ expected_revision: baseline.revision, content });
      onPublished();
    } catch { /* The editor retains its input and renders the mutation error. */ }
    finally { onBusy(false); }
  }

  async function reloadPublished() {
    setReloading(true);
    setReloadFailed(false);
    onBusy(true);
    try {
      const latest = await reload();
      setBaseline(latest);
      setContent(latest.content);
      publish.reset();
      draft.reset();
    } catch { setReloadFailed(true); }
    finally { setReloading(false); onBusy(false); }
  }

  return (
    <>
      <div className="min-h-0 space-y-4 overflow-y-auto">
        <div className="space-y-2">
          <label htmlFor={fieldId} className="text-caption font-medium">{t(($) => $.continuity.content)}</label>
          <Textarea id={fieldId} rows={8} value={content} disabled={pending} onChange={(e) => setContent(e.target.value)} aria-invalid={!valid && content !== ""} aria-describedby={`${fieldId}-limit`} />
          <p id={`${fieldId}-limit`} className="text-caption text-muted-foreground">{t(($) => $.continuity.limit)}</p>
          {!valid && content !== "" && <p role="alert" className="text-caption text-destructive">{t(($) => $.continuity.invalid)}</p>}
        </div>
        <div className="space-y-2 rounded-md border p-3">
          <Button size="sm" variant="outline" disabled={pending || draft.isPending} aria-busy={draft.isPending} onClick={() => draft.mutate()}>{t(($) => $.continuity.fetch_report)}</Button>
          {draft.isPending && <p role="status" className="text-caption">{t(($) => $.continuity.loading)}</p>}
          {draft.isError && <p role="alert" className="text-caption text-destructive">{t(($) => $.continuity.draft_failed)}</p>}
          {draft.data && !draft.data.available && <p role="status" className="text-caption text-muted-foreground">{t(($) => $.continuity.no_source)}</p>}
          {source && (
            <>
              <p className="text-caption font-medium">{t(($) => $.continuity.unreviewed)}</p>
              <AppLink href={paths.issueDetail(source.issue_id)} target="_blank" rel="noopener noreferrer" className="text-caption underline">{t(($) => $.continuity.source)}</AppLink>
              <p className="break-all text-caption text-muted-foreground">{source.completed_at} · {source.task_id}</p>
              <pre className="whitespace-pre-wrap break-words text-caption">{source.report_excerpt}</pre>
              {source.truncated && <p className="text-caption text-muted-foreground">{t(($) => $.continuity.truncated)}</p>}
              {draft.data?.suggested_content === null && <p className="text-caption text-muted-foreground">{t(($) => $.continuity.budget)}</p>}
              {draft.data?.suggested_content != null && draft.data.reason === "review_required" && (
                <Button size="sm" variant="secondary" disabled={dirty || pending || staleDraft || draft.isPending} onClick={() => setContent(draft.data!.suggested_content!)}>{t(($) => $.continuity.use_draft)}</Button>
              )}
              {dirty && <p className="text-caption text-muted-foreground">{t(($) => $.continuity.edits_preserved)}</p>}
            </>
          )}
        </div>
        {conflict && <p role="alert" className="text-caption text-destructive">{t(($) => $.continuity.conflict)}</p>}
        {publish.isError && !conflict && <p role="alert" className="text-caption text-destructive">{t(($) => $.continuity.publish_failed)}</p>}
        {reloadFailed && <p role="alert" className="text-caption text-destructive">{t(($) => $.continuity.load_failed)}</p>}
        {(conflict || publish.isError) && <Button size="sm" variant="outline" disabled={pending} onClick={() => void reloadPublished()}>{t(($) => $.continuity.reload_discard)}</Button>}
      </div>
      <DialogFooter>
        <Button disabled={pending || !valid || !dirty || conflict} aria-busy={publish.isPending} onClick={() => void publishContent()}>{t(($) => $.continuity.publish)}</Button>
      </DialogFooter>
    </>
  );
}
