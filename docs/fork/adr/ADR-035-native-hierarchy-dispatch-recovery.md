# ADR-035 — Atomic native hierarchy task registration and recovery

- Status: Accepted
- Date: 2026-10-21
- Scope: DELIVERY_CHECKLIST R3; #102
- Governing decisions: ADR-004/005/012/014/015/031/033/034
- Narrows: ADR-031/033's deferred post-commit dispatch boundary

## Evidence and decision

Issue creation currently commits the hierarchy receipt/Issue before native
EnqueueTaskForIssue. Creation retry intentionally never enqueues again, so a
crash can strand committed work. Queue absence cannot distinguish an undelivered
operation from deleted/pruned execution history.

The native channel Issue path already has an atomic sole-initial-assigned-task
primitive: createDeferredChannelIssueTaskWithQueries uses transaction-bound
native planning/attribution and inserts an inert task. Its historical internal
channel_issue_media_pending marker reserves the native unique pending slot and
preserves native same-thread comment merges before claim, including re-attribution and overlay
replacement. There are no frontend/daemon media behaviors tied to that marker;
its consumers are native queue/coalescing/promotion SQL. Removing it would lose
the native reservation and introduce competing initial runs. Reuse that exact
storage contract for hierarchy initial tasks, without pretending to have channel
attachments or invoking any channel router. No parallel queue, executor or
recovery sweeper is added; the historical marker is an implementation detail.

For a new delegation or manager escalation, insert its native deferred task and
record initial_task_id in the existing request receipt in the Issue transaction.
Any admission, planning, insertion, receipt update or commit failure rolls back
all rows. Readiness refusal cannot silently commit an Issue without a task.
Root human intervention keeps its existing atomic Issue/inbox and no Agent task.
The source work and all existing execution remain unchanged.

Use a PostgreSQL-clock one-minute fire_at fallback. After commit, publish the
ordinary Issue event, best-effort hydrate the optional native MCP overlay under
its untouched-plan guard, and promote through the native idempotent method.
Both hydration and early promotion require no dispatch/start history, so a
late post-commit continuation cannot alter an already executed task even if its
status is later deferred. Native retries create successor task IDs.
Promotion emits task:queued before notifying the daemon. No network call or
external notification runs inside the transaction. A lost response, cancellation,
crash before overlay/promotion or missed notification leaves the same durable
task discoverable by native deferred scheduling/singular or batch polling.
Existing runtime freshness, capacity, claim/reclaim and execution fences remain
authoritative. This guarantees recoverable registration, not exactly-once model
execution or exactly-once event publication.

Creation replay returns the original current Issue and recorded initial task ID
without enqueue, promotion, hydration or other new side effects. Failed, running,
waiting, cancelled, completed or deleted tasks are never cloned/replayed by this
mechanism. Native independently governed retry semantics are not changed.

## Concurrent native promotion fence

Both native due-promotion queries select deferred candidates in a subquery but
previously updated the target by ID alone. A promoter blocked behind a row lock
could use an old candidate snapshot after a claimant/canceller won that row.
Add an outer `status = 'deferred'` predicate to singular and batch promotion so
PostgreSQL rechecks the current target after the wait. A deterministic locked-row
interleaving must prove that dispatched/cancelled rows are not put back in the
queue. This is a necessary R3 execution fence, not a new executor or scope item.

## Historical operations and downgrade

Add nullable initial_task_id to each FK-free hierarchy receipt table (900028).
Do not backfill it by guessing which native task executed an old operation.
NULL on an old Agent-target receipt is unverified history, never proof of safe
replay. Missing/pruned task rows are equally ambiguous. Existing native Issue/run
inspection and escalation diagnostics identify work requiring review; a person
may deliberately use ordinary controls after reviewing artifacts. Automatic
reconstruction from absence is rejected under ADR-012. No new recovery action
impersonates a running Agent. This is a safety outcome for historical ambiguity,
not an additional delivery phase. Root human receipts legitimately have NULL.

Rolling older servers can still write the historical contract; their NULL rows
remain unverified. Full atomic registration applies once the request reaches an
updated server. Downgrade removes only the diagnostic pointers; native Issue,
task and work history survive, and old creation replay still does not enqueue.

## Validation and compatibility

Prove whole-transaction rollback; commit-before-hot-path crash followed by fresh
native service promotion/claim of exactly the recorded task; concurrent creator,
replay, promoter and claim races; source/originator lineage; native pre-claim
comment coalescing/overlay fencing; no partial/terminal/deleted-task replay;
historical NULL refusal to reconstruct; root no Agent task; up/down/up migration
and existing native regressions under PostgreSQL 17/race. No installed CLI or
provider execution is required. Existing media task behavior stays unchanged.

Integrated upstream 2b6456913; reviewed c9b6de856. Qoder adds guarded native task
start/completion transitions, not a different Issue enqueue path; the fork keeps
those transitions outside this change. Isolated upstream integration and real
provider checks remain R5. Revalidate the upstream head and affected hooks before
merge; do not claim an unperformed synchronization or real-account smoke.
