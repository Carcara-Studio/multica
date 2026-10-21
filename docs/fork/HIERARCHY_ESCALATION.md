# Project hierarchy escalation

## Implemented boundary (#94)

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
Current reviewed upstream: `e0e9efc5f1d6ff2a88ad2d7844d4d76f2519985d`;
its UI-only advance changes no server files.

## Native routing surfaces reviewed

| Existing primitive | Useful behavior | Boundary before reuse |
| --- | --- | --- |
| `server/internal/handler/comment.go`: comment trigger resolution/enqueue | Native invocation/runtime authorization, queued/coalesced/deferred/blocked outcomes and explicit mention context | A generic mention is not hierarchy authority; saving a comment and triggering execution are separate phases. Do not claim atomic escalation delivery or replay safety from comment creation |
| `server/internal/service/issue_wakeup_system.go`: native `child_done` | Durable condition receipts, ordinary run fences/coalescing and member inbox delivery | Its meaning is completed child work and its target is the current assignee. Upward escalation must not impersonate child completion or reassign the original Issue merely to reuse this rule |
| `server/internal/inbox/create.go`: `CreateIfEligible` | Membership-scoped insertion; ineligible recipients produce no insertion | Does not choose a human recipient or define a Project intervention lifecycle. Root boundary must remain visible when Project lead is an Agent or no lead exists |
| `server/internal/service/issue_hierarchy_delegation.go` | Durable explicit request identity, intent collision and deleted-child handling | Its receipt is specifically a native child-creation contract. Do not overload it for a decision/intervention request |

## Remaining delivery work

Before adding routing, settle a focused durable escalation operation under
ADR-005: request identity and reason/context, frozen source/target lineage,
collision/replay/re-authorization rules, tombstones and commit/delivery recovery.
Select ordinary Issue/comment/inbox/wakeup facilities where they satisfy those
contracts. The root needs a durable Project human-intervention surface and an
explicit recipient/visibility policy that works without an Agent lead loop;
a boolean relation result alone does not meet that requirement.

Then compose the current task credential and native invocation/runtime gates,
manager execution fencing/coalescing and root intervention persistence in one
bounded operation. Define authorization separately from topology and preserve
work artifacts and partial execution; no automatic reassignment, replay or
recursive fan-out. Only after that operation has integration coverage should
API, CLI, Agent briefing and UI advertise escalation.

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
