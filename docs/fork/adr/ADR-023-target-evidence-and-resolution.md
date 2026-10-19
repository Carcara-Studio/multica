# ADR-023 — Target-scoped execution evidence and immutable route resolution

- Status: Accepted
- Date: 2026-10-19
- Decision owners: Repository agent under standing implementation authority
- Related issues: #37, #53
- Extends: ADR-007, ADR-010, ADR-013, ADR-018, ADR-022
- Refines: ADR-007 session eligibility for opt-in work

## Decision

Introduce an internal, pure execution-evidence verifier and enqueue/queued-target
resolver. Neither stores runtime observations nor launches work. No production
writer, claim handler, worker, endpoint or daemon advertises/calls these helpers
in this increment. Normal Agents without optional policy/config retain primary
behavior; no lookup/backfill or execution gate is added to their production path.

Saved fallback configuration, a model catalog, heartbeat, successful CLI exit or
client capability header cannot constitute execution readiness. An observation
must match a server-issued challenge bound to Agent/workspace/owner, route,
primary and fallback configuration generations, selected runtime/profile, model,
thinking and tier. Both routes include fallback generation, as ADR-018 requires.

Current authenticated runtime connection (runtime/workspace/owner/session/provider)
and target-specific credential/configuration epochs must independently match the
challenge and observation. Two targets on one runtime can have different account
epochs; runtime ID or process-start generation alone is insufficient. Epochs are
opaque identifiers, never credential hashes. An approved adapter must track the
actual effective credentials/config/default model, including changes while its
daemon stays running. No account inspection is implemented or implied here.
Pinned model identity uses verified catalog qualification without rewriting the
saved model selection; a default model still needs an observed effective model.

Evidence expires at an exclusive server-clock deadline, within two minutes of
issuance. A verified value has private fields and rechecks binding, current
connection, epochs, purpose and freshness when consumed. It cannot transfer to a
new connection, other target, edited generation or a different credential/config
identity. The future authenticated transport must persist/consume its challenge
ledger and revalidate current identity under its owner/claim protocol; this pure
verifier is not that ledger, an execution lease, or replay prevention by itself.

## Adapter approval and probe separation

Capabilities are the intersection of reported and server-approved adapter
guarantees. Production `NewEvidenceVerifier()` approves **no adapters**. Its policy
cannot be populated by transport callers; positive tests use an unexported
fixture policy. Approving a real adapter requires implementing/reviewing its
credential/config epoch tracking and corresponding execution guarantees.

Task capability requires exact target options/credentials/model, faithful shared
context and tool/MCP behavior, and target session isolation. Probe capability is
separate: primary target only, minimal inference, no tools, MCP, hooks, repository,
user context or session. Every guarantee must be both reported and approved.
Task capability is never sufficient for a probe. Probe capability is never an
inference outcome: this verifier cannot mark primary healthy or settle a probe.
The Agent-shaped task resolver is not a probe payload builder.

## Target and route resolution

Enqueue selects primary only in primary-active, otherwise fallback in
fallback-active/primary-probing. Missing/inconsistent policy, suspension, archive,
unknown state, incompatible/unknown target metadata, absent target overrides,
missing evidence or stale evidence block the affected opt-in route. There is no
"try primary instead" branch when fallback validation fails.

Fallback overlays only target execution fields on a copy of the existing Agent:
runtime/mode, model/thinking/tier, environment, arguments, runtime config and MCP.
Empty fallback values do not inherit primary secrets/options. Agent identity,
instructions, permissions, disabled skills and app allowlist stay shared. Copies
avoid mutating source rows or leaking mutable buffers between resolved payloads.

The resolved enqueue result produces the existing route-insert parameters; it
neither writes Task rows nor grants quota activation. The future caller must
insert Task and required route proof atomically in its existing owner transaction,
rolling back the Task if CAS/proof persistence fails. All eight writer/retry paths
remain integration work; no ordinary failed Task is replayed by this resolver.

Queued resolution uses the persisted route/runtime and original generations.
Availability version can advance while the original enqueue version remains in
proof: primary recovery does not retarget queued fallback, and quota activation
does not retarget queued primary. Edits/removal/suspension invalidate opt-in work
before launch. The caller still enforces workspace/task/runtime authorization,
claim/reclaim/lease ownership and all existing lifecycle gates. Unmarked legacy
work stays primary, preserves the upstream runtime mismatch rejection and never
gains opt-in quota authority or fallback secret access. Proof is never removed to
turn opted-in work into legacy work.

## Session safety refinement

Configured routes request a fresh provider session until a target/generation-
scoped session ledger exists. Runtime equality alone cannot distinguish primary
from fallback on the same runtime, or an old policy's session from its replacement.
This is limited to opt-in resolution: ordinary upstream session behavior is
unchanged. Shared-context capability must prove faithful server task context
preparation with a fresh session; fresh does not authorize dropping required
Issue/chat/Project context or tools. No adapter is approved for this today.

## Verification and open boundaries

Offline tests cover server approval, every required guarantee, wrong challenge,
identity/generation/connection/epoch/purpose, inclusive expiry and invalid clocks;
unknown/default policy never grants readiness. Resolver regressions cover normal
Agents/builders, secret isolation/shared identity, source immutability, both queue
availability races, same-runtime isolation, removed/edited/archived/suspended
policy, missing marker/runtime rejection, missing evidence and pinned-model
qualification. These tests never discover CLIs, accounts or perform inference.

No migrations, public contracts or provider executor changes. Remaining gates:
real approved adapters, account/default-model epoch observation, authenticated
challenge/identity ledger and transport, complete tool/MCP/context/session
semantics, all enqueue/retry/claim integration, quota activation hook and real
minimal-inference execution/settlement before exposing configuration/activation.
