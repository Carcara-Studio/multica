# Project hierarchy escalation

## Relation boundary (#94)

The first ADR-005 slice is an internal, caller-transaction relation gate in
`server/internal/projecthierarchy/escalation.go`. It validates the acting Agent's
own directly assigned Project Issue and derives exactly one persisted
`manager_membership_id` edge. Its input has no requested target, so it cannot
select a peer, skip a manager or jump to the root.

A root yields `HumanInterventionRequired = true` with no manager Agent or
membership. Project `lead_type` / `lead_id` never adds an organizational edge:
an Agent lead, including the root itself, cannot become an implicit escalation
target. This result identifies a boundary; it does not create an intervention
record or notify a person.

The gate validates the active tree, memberships and the source/direct-manager
Agents' workspace, user kind and non-archived state. It retains native lock
ordering: Workspace `FOR NO KEY UPDATE`, Project `FOR UPDATE`, Issue lock,
Agent `FOR SHARE NOWAIT` in UUID order. Contended Agent admission fails closed;
no ancestor is tried as a substitute. Memberships are read again after Agent
locks, including after an archive followed by restore, so stale membership
snapshots cannot authorize the path. ADR-006 archive writers take no Project
lock; this fresh read is required even while this gate owns the Project lock.

Returned facts are valid only in the caller's current transaction. They are not
MAT authentication, task ownership, originator invocation permission, runtime
readiness, an execution lease or a durable replay receipt. The structural gate
can accept Agents without runtimes. A future routing adapter must independently
approve the current execution and reject an unusable direct manager without
silently choosing an ancestor. Missing/archived/system/cross-workspace Agents,
inactive memberships, broken trees, Squad assignment and Issues without the
selected Project are denied. Infrastructure errors remain errors.

No Issue, comment, inbox item, wakeup, task or activity is created. Existing Issue
assignment/status and work remain unchanged. A manager's next upward hop needs
another explicit operation under its own authenticated execution; this gate does
not transfer ownership of the worker's Issue to that manager. There is no public
API/CLI/UI action or new Agent briefing capability in this slice.

Implementation baseline: fork `e1d24a89155515431a1d3dae7b905119eca87cbb`.
Integrated upstream: `2b645691387e1535cc63d6ebf2ba4e996387c8b2`.
Relation-gate review baseline: `e0e9efc5f1d6ff2a88ad2d7844d4d76f2519985d`.
R2 additionally reviewed `c9b6de856` (Qoder cloud runtime, PR-conflict wakeups,
tooltip UI and runtime docs). Router/client additions occupy separate sections;
the Issue detail change does not touch the new panel insertion. Hierarchy/MAT
admission and R1 creation remain unchanged. Qoder-specific start/completion
transitions require native regression when integrated in isolated R5 sync; this
review does not claim that synchronization or real-provider execution.

## Native routing surfaces reviewed

| Existing primitive | Useful behavior | Boundary before reuse |
| --- | --- | --- |
| `server/internal/handler/comment.go`: comment trigger resolution/enqueue | Native invocation/runtime authorization, queued/coalesced/deferred/blocked outcomes and explicit mention context | A generic mention is not hierarchy authority; saving a comment and triggering execution are separate phases. Do not claim atomic escalation delivery or replay safety from comment creation |
| `server/internal/service/issue_wakeup_system.go`: native `child_done` | Durable condition receipts, ordinary run fences/coalescing and member inbox delivery | Its meaning is completed child work and its target is the current assignee. Upward escalation must not impersonate child completion or reassign the original Issue merely to reuse this rule |
| `server/internal/inbox/create.go`: `CreateIfEligible` | Membership-scoped insertion; ineligible recipients produce no insertion | Does not choose a human recipient or define a Project intervention lifecycle. Root boundary must remain visible when Project lead is an Agent or no lead exists |
| `server/internal/service/issue_hierarchy_delegation.go` | Durable explicit request identity, intent collision and deleted-child handling | Its receipt is specifically a native child-creation contract. Do not overload it for a decision/intervention request |

## Durable operation (R1, #98)

[ADR-033](./adr/ADR-033-native-escalation-decision-issue.md) selects a native
child decision Issue. `IssueService.Escalate` persists reason/context, source
membership/task lineage, frozen manager/root route and stable request identity
in the Issue creation transaction. Original work stays unchanged. Manager
assignment uses native enqueue and originator invocation/runtime admission;
no unavailable manager is skipped.

At root, the visibly titled human-intervention Issue is unassigned. Native
action-required inbox items commit atomically for workspace owners/admins plus
a valid member Project lead, deduplicated. Agent leads never route the request.
No eligible human causes rollback. Native Inbox schemas tolerate unknown type
strings and display the title; explicit client actions belong to R2.

Same identity/intent returns the current original decision after fresh MAT/task,
hierarchy and invocation/readiness checks. Changed reason/context or manager
route conflicts. Deleted decisions retain tombstones. A later turn can retry
with fresh credentials while original lineage is retained. Replay creates no
Issue, counter increment, event, task or duplicate inbox. Another upward hop
requires an explicit operation from the manager's own Issue/execution.

R2 owns API/CLI, Agent briefing and management UI exposure. ADR-035 addresses R3 with atomic native initial-task registration for manager
decisions and native deadline/polling recovery: replay never enqueues again. R5 owns real
installed-Agent/account smoke. These are the existing fixed checklist items.

## Validation

Database tests cover direct-manager derivation, a second explicit hop, no
Project/Squad/cross-workspace paths, invalid/inactive/broken topology, unavailable
Agents, root with no/Agent/self Project lead, retained/released locks, Agent
archive contention and reparented later-turn facts. A deterministic transaction
interleaving archives and restores the source between discovery and Agent
admission and requires fresh membership rejection. Commit/rollback checks assert
unchanged Issue content and no new Issue/task/comment/inbox/wakeup/activity.

These are server structural tests, not real installed-Agent/provider smoke and
not proof of delivered escalation notifications. Native delegation, lifecycle,
no-Project/direct-Agent/Squad and runtime regressions remain compatibility gates.

R1 integration tests cover manager delivery/originator, concurrent replay,
later-turn credentials, intent conflict/tombstones, expired/revoked MAT and
stopped task denial, unavailable manager, root human inbox without Agent
execution, rollback and workspace cleanup. Native PostgreSQL CI is the delivery
gate; local database absence is not a passing integration result.

## Public surfaces (R2, #100)

ADR-034 exposes `POST /api/issues/{id}/hierarchy-escalations` to the current
running task's MAT only. Input is `request_id` (persisted nonzero UUID), required
reason (1-8000 UTF-8 bytes), optional context (up to 16000 bytes), with a 160 KiB
encoded JSON envelope limit and no unknown fields. The server resolves all
actor/task/Project/route identities. Returns 201 for registration, 200 for fresh
authorized replay, and `{issue_id,replayed}`. Invalid input is 400, unavailable
or unauthorized admission 403, route/intent conflict or deleted decision 409.
A busy hierarchy is 409; clients preserve identity and inspect the outcome.

`multica issue escalate <id-or-key> --request-id <UUID> --reason <reason>`
accepts `--context`, `--context-file` or `--context-stdin`. It requires a task
credential and never automatically retries, reassigns or falls back to another
operation. Persist the identity before the first call; use unchanged reason and
context on a later authenticated retry. A new UUID deliberately requests new
work. The bounded direct-Issue Project briefing advertises these rules only in
its existing eligible task context; it grants no additional Agent Identity rights.

GET on the same route uses native workspace/Issue read authorization. It returns
`{items,next_cursor}` for either the source or decision Issue, with up to 50 rows
and an optional `before=<decision-Issue-UUID>` cursor. Records expose frozen
source/task/manager or human route, reason/context, current native decision
status, display names and repair diagnostics. Deleted decisions retain the
receipt. Read failures are errors, never an empty successful history.

Shared web/desktop Issue detail has an expandable Escalations panel, source and
decision links, reason/context, native state, manager/human boundary, repair
messages, explicit refresh and older pages. Native Issue events invalidate these
workspace-scoped read snapshots. People handle the linked native decision Issue
with existing controls; creation does not impersonate a running Agent. Queue
presence is not delivery/execution proof; a missing dispatch prompts inspection,
not blind replay. Native mobile still sees the decision Issue and root inbox;
the new dedicated panel is shared web/desktop. The R2 surface itself introduces no migration or executor. ADR-035 / #102 adds
nullable initial_task_id receipt columns for R3; root intervention retains its
atomic human inbox without an Agent task. Historical NULL/deleted-task evidence
requires inspection, never reconstruction. R5 real smoke remains open.
