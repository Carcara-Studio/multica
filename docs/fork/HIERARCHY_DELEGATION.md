# Transactional direct-report delegation admission

- Issues: #75 and #77; follows #73/#74
- Authority: ADR-001, ADR-003, ADR-004 and ADR-014
- Fork baseline: `1529aec2db660aad6417ac3842dce91e768ec181`
- Upstream baseline: `47120a4131564175da044d63d897ed9b5d76979a`

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

Call before taking Issue/property/Agent/counter locks. Future integration must
review the complete upstream create/teardown/permission lock graph; this order
is not a blanket approval for arbitrary outer transactions or multi-action use.

The returned identifiers are structural facts, not a transferable authorization
receipt. They grant no caller authentication, target invocation permission,
runtime readiness or execution/idempotency capability. Those checks must be
composed independently in the same final create operation; no public caller
can invoke this gate yet. A fresh transaction must recheck the relation.

## Native child creation and durable replay

Issue #77 adds an optional internal `IssueCreateOpts.HierarchyDelegation`
hook in the existing Create transaction. It calls the relation gate before
other create locks, then requires a trusted transaction-bound authorization
callback on both creation and replay. No production caller is registered.

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

Public API/CLI remains unavailable until independently authenticated actor/task
and normal invocation/runtime admission are implemented and reviewed. A test
callback is not production permission. Parent completion, mentions and Squads
keep native semantics.

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

No local execution environment is available. Compile/vet/full PostgreSQL/race
CI is required for the exact merge head. No local test/formatting result or
installed-agent/account/inference use is claimed. Public delegation, automatic
fallback and primary recovery probes remain disabled.
