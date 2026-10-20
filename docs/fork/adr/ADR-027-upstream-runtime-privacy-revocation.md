# ADR-027 — Upstream runtime privacy revocation

- Status: Accepted
- Date: 2026-10-20
- Decision owners: Repository agent under standing implementation authority
- Related issues: #37, #61
- Extends: ADR-021, ADR-025, ADR-026
- Upstream baseline: `47120a4131564175da044d63d897ed9b5d76979a`

## Decision

Preserve upstream's runtime privacy confirmation, cancellation, unbinding and
Autopilot pause semantics. A private runtime belongs to its runtime owner;
workspace administration does not make another Agent owner's execution valid.
The upstream dependency plan continues to describe primary bindings and active
tasks. Optional fallback-only references do not add task cancellation or rewrite
a primary binding, but lose execution authority when access is revoked.

Both direct privacy changes without primary dependents and confirmed revocations
capture the union of user Agents referencing the runtime as primary or fallback.
Visibility changes rotate existing policy generations and clear pending probes
and evidence challenges in the same transaction. Becoming private also suspends
policies whose Agent owner differs from the runtime owner. The runtime owner's
policy retains its existing availability. Becoming public never implicitly
resumes a suspended policy or declares primary health. Ordinary Agents gain no
optional policy rows; immutable task route history remains unchanged.

Runtime access writers use upstream's `FOR NO KEY UPDATE` fence. Binding writers
lock workspace before runtime before Agent and retain the fork's single
transaction through nullable override changes and policy invalidation. Privacy
writers acquire affected Agent rows in UUID order with `NOWAIT`, including owner
and fallback-only references. Contention returns upstream's structured busy
conflict and rolls back; it must not introduce a runtime/Agent or chat/Agent wait
cycle. Optional state mutation remains a leaf phase after every upstream task,
chat, Agent and Autopilot write. Failure in that phase rolls back the entire
privacy operation, including cancellations and unbinding.

## Verification and scope

Regression tests cover direct and confirmed privacy transitions, owner versus
nonowner authority, fallback-only bindings, pending challenge removal, complete
rollback on generation overflow and an independently locked fallback Agent.
Existing upstream privacy, binding and lock-order tests remain required.
PostgreSQL and race verification run in CI; local checks without PostgreSQL do
not count skipped database tests as behavioral validation.

No DDL, provider access, production evidence adapter, public failover API or
automatic fallback/probe routing is enabled. Upstream changes to Codex skill
preparation and DSH discovery are incorporated unchanged, rather than replaced
by fork implementations. Real adapter observations remain the next increment.
