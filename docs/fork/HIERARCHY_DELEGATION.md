# Transactional direct-report delegation admission

- Issues: #75, #77, #80, #82 and #88; follows #73/#74
- Authority: ADR-001, ADR-003, ADR-004, ADR-014, ADR-031 and ADR-032
- Fork baseline for completion integration coverage: `dbe8cc5eac99f01b7d709a970d8cfa8d80342303`
- Integrated/reviewed upstream: `2b645691387e1535cc63d6ebf2ba4e996387c8b2`

## Implemented boundary

`projecthierarchy.CheckDelegationRelation` is an internal structural gate.
The caller must supply independently resolved identities and a real, existing
`pgx.Tx`, use one Project per action and roll back on any error. The function
neither begins nor commits a transaction and creates no Issue/task.

The first delegation slice is deliberately limited to a parent Issue currently
assigned directly to the delegating Agent. The parent must still be in the
expected workspace/Project. Both Agents must be live user Agents in that
workspace, with active memberships in the same valid Project tree. The target
must report directly to the delegator. Peers, upward/self/deeper-descendant and
cross-Project delegation are rejected. Mention and Squad routing are unchanged.

The caller retains these locks until commit/rollback:

1. workspace `FOR NO KEY UPDATE`, fencing teardown and serializing with the native Issue counter before Project locks;
2. Project structural `FOR UPDATE`, using the existing hierarchy writer protocol;
3. parent Issue `FOR UPDATE`, freezing its Project/assignment;
4. relevant Agent rows `FOR SHARE` in UUID order, freezing archive/owner/config
   changes to those rows.

The workspace lock is intentionally stronger than a lifetime key-share fence:
ordinary Issue creation holds its counter row before INSERT checks Project
foreign keys. Taking Project first and upgrading workspace later would permit an
inverted wait. The gate acquires the counter-compatible lock first; it does not
increment the counter. This serializes admission with other workspace Issue
creates and is a deliberate contention cost.

Call before taking Issue/property/Agent/counter locks. The HTTP adapter follows
this ordering and native snapshot authorization. This is not blanket approval
for arbitrary outer transactions, task-row locks or multi-action use.

The returned identifiers are structural facts, not a transferable authorization
receipt. They grant no caller authentication, target invocation permission,
runtime readiness or execution/idempotency capability. Those checks are composed independently by the task-token HTTP adapter in the
same final Create operation. A fresh transaction must recheck the relation.

## Native child creation and durable replay

Issue #77 adds an optional internal `IssueCreateOpts.HierarchyDelegation`
hook in the existing Create transaction. It calls the relation gate before
other create locks, then requires a trusted transaction-bound authorization
callback on both creation and replay. The task-token-only HTTP adapter described below supplies this callback.

ADR-031 records the inspected absence of a general native create identity and
the selected FK-free operation identity table. Workspace, parent, delegator
and request UUID identify one operation. Project, target, title, description
and priority form the immutable intent hash. Original source task is lineage;
later authorized turns can reuse the same request identity.

Identity and native child commit together. Same intent returns the current
child without counters/events/enqueue; changed intent conflicts. Child
completion/edits do not reopen the identity. A deleted child retains a
tombstone until workspace teardown and returns an unavailable-child error.
A new request UUID represents deliberate follow-up, including the same title.
No separate hierarchy task/executor is introduced.

The initial hook accepts a todo child with native agent_create origin and
rejects attachments/labels/properties/dates/stages/source capture/channel
deferral. Ordinary Create options preserve existing behavior. Its ordinary
post-commit assignment enqueue remains unchanged, including its crash gap:
this guarantees creation replay, not exactly-once durable dispatch.

## Authenticated API

POST /api/issues/{id}/hierarchy-delegations accepts a bearer MAT for the
delegator's currently running, directly assigned parent Issue. Parent supports
UUID or its workspace identifier. Body fields are request_id, target_agent_id,
title, optional description and priority. Actor, task, originator and Project
cannot be supplied in JSON.

The adapter re-reads the token, bindings, expiry, token-user membership and
running source task in native Create's transaction, then uses the normal
originator-based canInvokeAgent and AgentReadiness checks with transaction-bound
queries. Archived/unbound/unusable/owner-incompatible targets fail closed;
normally waitable offline targets retain native queue semantics. There is no
hierarchy or admin bypass for private/member-scoped Agents. Squad tasks are
excluded. Creation and replay use the same admission checks.

Response is {issue_id, replayed}: 201 new, 200 replay, 409 changed intent and
410 deleted child. A receipt does not mean a daemon has started or that a queue
ID can be recovered from a prior attempt. Fetch native Issue/task resources.
The task-scoped CLI exposes this adapter as `multica issue delegate`.

ADR-032 explicitly defines fresh-read authorization, not a task-completion or
revocation lease spanning commit. No task lock is acquired after Issue/Agent
locks, avoiding inversion with native completion. The native post-commit
scheduling gap remains. Parent completion, mentions and Squads are unchanged.

## Task-scoped CLI

```sh
multica issue delegate <parent-id-or-key> \
  --agent <direct-report-agent-uuid> \
  --request-id <persisted-uuid> --title 'Review the implementation'
```

The caller persists the explicit nonzero request UUID before invoking the
command. Reuse that UUID and unchanged intent after an uncertain result or in
a later task turn; create another UUID only for deliberately new work. The
command makes one POST, requires a native task token, resolves the parent using
the existing Issue resolver and requires an explicit Agent UUID. It preserves
typed HTTP errors and validates both receipt fields before printing JSON.
`--description`, `--description-file` and `--description-stdin` reuse native
exclusive text sources and file containment checks. Priority defaults to `none`.

The existing bounded direct-Issue hierarchy briefing advertises the command and
stable-ID rule. It grants no authority, adds no automatic fanout and keeps the
existing no-Project/Squad exclusions. The existing agent shell tooling already
permits `multica` commands; no new executor or credential source is introduced.
If the command or endpoint is unavailable or permission is denied, report the
block rather than bypassing admission through generic creation/assignment or
mentions. Existing clients and servers do not negotiate a fallback.

## Completion and manager resumption

Delegated children retain native Issue/task completion and `child_done` wakeups.
The server integration regression under #88 composes real MAT authentication,
delegation creation/replay, native runtime claims and the task tokens they mint,
the child's authenticated Issue status update, daemon completion/token revocation,
the native completion receipt/timeline and the manager's claimed wakeup run.

The resumed manager uses its newly issued MAT and the original request UUID to
replay the completed delegation. It receives the same child without another
child task or manager wakeup. Re-saving the same completed child is not another
completion fact. The parent stays `in_progress`; the responsible Agent decides
its next action. No hierarchy-specific executor, completion callback or queue
is added.

This is server integration coverage using HTTP handlers and PostgreSQL fixtures,
not a real installed CLI/daemon/provider inference run. CLI transport validation
remains separately covered by the simulated HTTP suite. The existing dispatch
crash gap remains; escalation and Agent lifecycle integration are separate work.

## Validation

Tests cover direct-edge acceptance; peer/upward/self/deeper/cross-Project/
cross-workspace rejection; missing/no-Project/moved/reassigned/Squad parents;
archived relevant Agents/memberships; invalid identity and absent transaction.
Separate transactions attempt conflicting row locks with NOWAIT and must receive
55P03 while the caller retains admission. After caller commit/rollback, those
locks are available; an actual reparent then invalidates the old relation.
A busy native workspace counter blocks admission before Project acquisition.
The gate creates neither child Issues nor queue rows.

Native create tests additionally cover concurrent retry, follow-up, changed
intent, completed/edited/deleted children, fresh authorization on replay,
counter/event suppression, reassigned parent, missing callback, commit rollback
and workspace identity cleanup.

CLI tests use an in-process HTTP server and cover stable intent, creation/replay
receipts, typed failures without retry/fallback, invalid inputs and receipts,
native description sources and canonical parent lookup. Compile/vet/full
PostgreSQL/race CI is required for the exact merge head. No installed-agent,
account or inference use is claimed. Automatic fallback and primary recovery
probes remain disabled.
