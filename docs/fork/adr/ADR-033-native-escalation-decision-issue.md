# ADR-033 — Durable native escalation decision Issues

- Status: Accepted
- Date: 2026-10-21
- Scope: DELIVERY_CHECKLIST.md R1; #98
- Governing decisions: ADR-005/006/012/014/015/031/032

## Decision

An explicit hierarchy escalation creates an ordinary decision Issue in the same
Project, linked as a child of the source work Issue. Original work, assignment,
status and any partial execution are preserved. The decision Issue contains the
reason/context and source reference; its native status is the decision lifecycle.
There is no duplicate escalation queue/state machine or automatic work replay.

The existing one-hop relation gate derives the direct manager. A manager request
assigns the decision Issue to that Agent and uses native invocation/readiness and
Issue/task execution. A manager can escalate again only through another explicit
operation from its own decision Issue/run; no recursive fan-out occurs.

At root, create a visibly titled human-intervention decision Issue with no Agent
assignee. In the same transaction, create native action-required inbox items for
current workspace owners/admins and a valid member Project lead (deduplicated).
An Agent Project lead is never a recipient or substitute organizational edge.
No eligible human notification is a failure, not silent escalation success.
This uses existing Project Issue/inbox surfaces; R2 adds explicit action/status UI.

A FK-free operation receipt stores workspace/source Issue/source Agent/request
UUID, original task and membership lineage, frozen manager/root destination,
reason/context, intent hash and decision Issue ID. The native Issue, receipt and
root inbox items commit atomically. A separate concurrent unique index protects
operation identity. Workspace deletion explicitly removes receipts; decision
Issue deletion retains the receipt as a tombstone, so retry cannot recreate it.

Intent comprises Project, normalized reason/context and manager/root relation.
Source task is lineage, not intent, allowing a later authenticated turn to retry
the same UUID. Replays revalidate current task/MAT, hierarchy and native invocation
permissions/readiness; return the original current decision Issue without creating
another row, counter increment, event, inbox item or task. Changed intent/manager
route conflicts; deliberate follow-up requires another request UUID.

Use a small opt-in hook in IssueService.Create, preserving normal creation and
native attribution/enqueue. The callback is internal, required and never client
supplied: current task credentials and source execution ownership are distinct
from topology. R1 composes it with the existing handler-native authorization
helper and tests; public transport and client wiring are R2. No privilege is
granted by a Project relation or by workspace admin status over a private Agent.

Lock order remains Workspace -> Project -> source Issue -> shared NOWAIT Agents,
then native create/counter writes and root inbox insertion. Native inbox snapshot
eligibility avoids adding member locks or subscriber-order inversions. No extra
Project acquisition from archive; current membership is rechecked after Agent
locks under ADR-006. All authorization is fresh admission, not a post-commit lease.

Native automatic assignment enqueue follows commit. Its crash gap belongs to R3:
creation replay must not blindly enqueue again or assert exactly-once execution.
Durable decision/intervention registration and normal native delivery complete
this R1 slice; crash reconciliation remains an explicitly separate agreed item.

## Rejected alternatives

- Reassigning original work upward or replaying its partial run.
- Skipping unavailable managers or using an Agent Project lead at root.
- Faking child_done to route an upward request to a different assignee.
- Using untracked generic mentions as authority or idempotency receipts.
- Adding a second executor, new dashboard, recursive queue or global Agent state.

## Validation and compatibility

Native PG17 tests must cover manager/root delivery, originator/invocation/runtime
permissions, concurrency/replay/collision, fresh later-turn credentials, deleted
receipts' target tombstones, whole-transaction rollback and workspace cleanup.
Original work stays unchanged; root creates no Agent task. Preserve native
no-Project/direct-Agent/Squad/child_done/lifecycle regression gates. No public
API change in R1; R2 must expose bounded input and the durable receipt semantics.
