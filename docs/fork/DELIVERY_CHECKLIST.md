# Remaining delivery checklist

This is the single current execution-scope checklist for the five remaining
items agreed with the maintainer on 2026-10-21. Consolidated by #96 after #95;
repository baseline `fe00de5a5cf64f6c3952fbe4f7e4b1889ddef69b`.
Code/tests and accepted ADRs retain architectural authority under GOVERNANCE.md.
Older blueprint candidate lists describe history/design options; they do not
independently authorize additional delivery work.

## Fixed scope and completion criteria

All five items below remain open. A helper, audit, mock test or foundation PR
alone is not completion of an item. Maintain status/evidence in this document as
work lands; do not replace these identifiers with new phases.

| ID | Agreed delivery | Current status | Completion criteria |
| --- | --- | --- | --- |
| R1 | Complete escalation operation | In progress #98: native decision/intervention operation implemented; integration CI/review pending | Persist reason/context and source/target lineage with stable request identity; retries cannot duplicate the request or change its intent silently. Route one direct-manager edge using native permission/readiness and execution fences. Unavailable manager is not skipped. Root reaches a durable, visible human-intervention condition without an Agent/Project-lead loop. Preserve original work/partial execution; no automatic reassignment or replay. Integration tests prove delivery, replay/collision and root behavior. |
| R2 | Expose escalation | Pending; follows R1 | Provide the API, CLI, bounded Agent instructions and management UI for the R1 operation. Validate the current task credential, actor/Issue binding and permissions; reject unauthorized/unavailable targets. Show source, destination/root boundary, reason/state and repair needs. Preserve successful existing contracts and test the changed client surfaces. The relation helper alone must not be advertised as executable escalation. |
| R3 | Recover committed operations whose execution was not dispatched | Pending; existing native post-commit enqueue gap is documented | A server failure between durable registration and native enqueue can be detected and recovered without duplicate work or blind replay of ambiguous partial execution. Define and prove the recovery/reconciliation rule against native execution fences, including restart/concurrency tests. Cover the delivered delegation boundary and applicable escalation delivery; do not create a second general executor. |
| R4 | Complete automatic model fallback and recovery | Blocked at activation: producer credential/evidence boundary remains unresolved; control-plane foundations exist | Obtain trustworthy request-bound evidence for the effective execution credential/target, integrate all required dispatch/claim/retry writers, switch only on accepted quota evidence, route subsequent work to fallback, and test the primary every 30 minutes with isolated inference. Recover safely when the primary is available. Prove concurrency/crash/partial-execution behavior and the accepted provider compatibility policy. Automatic fallback/probes stay disabled until the evidence and execution gates actually pass; control-plane tests cannot close this item. |
| R5 | Real-environment validation and upstream compatibility | Pending final smoke; upstream revalidation remains continuous | Run the bounded installed CLI/daemon/application smoke with the maintainer's real Windows/account environment when authorized, record exact evidence and limitations, and resolve regressions in R1-R4. Incorporate the reviewed upstream UI advance through the normal isolated synchronization workflow (integrated baseline 2b645691387e1535cc63d6ebf2ba4e996387c8b2; reviewed advance e0e9efc5f1d6ff2a88ad2d7844d4d76f2519985d). Revalidate upstream for affected changes and verify the final integrated behavior. No real-account smoke is claimed from fake HTTP/agent or server tests. |

Recommended order: R1, then R2, then R3. R4's independent necessary work may
proceed when supported by evidence, but its external blocker is not permission
to invent an alternative product. R5 includes continuous compatibility work and
a final real-environment pass; it is not an unlimited feature-development bucket.
Upstream will continue moving after a validated baseline: later compatibility
maintenance is ongoing maintenance, not a new condition that postpones project
completion indefinitely.

## Scope controls

1. Every new work Issue and PR must name its R1-R5 item and the specific
   completion criterion it advances or defect it resolves. Before coding, check
   the current implementation, accepted ADRs and existing evidence. Reuse the
   delivered behavior instead of creating another implementation or audit.
2. Necessary implementation, regression, migration, compatibility and
   documentation substeps inside those criteria remain authorized through the
   autonomous governed workflow. They are subtasks of an existing item, not new
   top-level steps. No separate approval is required per Issue/PR.
3. Prefer the smallest complete delivery slice that advances a criterion.
   Split only for a concrete architectural/testability reason recorded in the
   Issue. Do not introduce endless preparatory phases, duplicate tests or
   unrelated refactoring to make the roadmap grow.
4. A new top-level item or functional expansion beyond these five requires
   explicit maintainer agreement before implementation. Record the proposed
   change, evidence, user benefit, affected criterion and alternatives in a
   scope-change Issue; update this checklist only after that agreement. Do not
   silently expand scope because an older blueprint listed a possibility.
5. If new code/upstream evidence reveals a necessary defect within an item,
   record the evidence and map its fix to that item's acceptance criteria. If
   the requirement is genuinely outside them, surface it as a scope-change
   proposal rather than treating it as automatically authorized.
6. A blocked item remains visibly blocked with its dependency and next concrete
   unblock condition. Do not hide a blocker by adding more foundations, buying
   a different service, replacing Codex, inspecting secrets or declaring a
   simulated check equivalent to real execution.
7. Mark an item complete only with Issue/PR links and verification evidence for
   every applicable criterion; retain genuine limitations. When all five meet
   their criteria, this agreed scope is complete. Remaining ideas become
   optional backlog proposals, not retroactive completion requirements.

## Explicit exclusions from this remaining scope

- Rebuilding hierarchy persistence/service/API or web/desktop/mobile management,
  direct-Issue delegation, native completion/resumption or archive safeguards
  already delivered (#16/#18/#22/#32/#33/#73/#75/#77/#80/#82/#88/#92).
- A separate hierarchy-management CLI, new chat/autopilot/mention-only hierarchy
  authority, recursive multi-level fan-out, a second orchestration engine, and
  new dashboards/observability products beyond what R1-R5 need for operation.
- Unrelated refactoring, gratuitous new test layers, paid-provider migration,
  producer replacement or unauthorized real-account execution as workarounds.

## Evidence and detailed sources

| Subject | Source |
| --- | --- |
| Escalation relation boundary and native routing candidates | [HIERARCHY_ESCALATION.md](./HIERARCHY_ESCALATION.md), ADR-005, #94 / PR #95 |
| Delegation and the native enqueue gap | [HIERARCHY_DELEGATION.md](./HIERARCHY_DELEGATION.md), ADR-004/031/032, #88 / PR #89 |
| Delivered Agent lifecycle consistency | [HIERARCHY_AGENT_LIFECYCLE.md](./HIERARCHY_AGENT_LIFECYCLE.md), ADR-006, #92 / PR #93 |
| Failover writer/evidence gates | [FAIL-001_INVESTIGATION.md](./FAIL-001_INVESTIGATION.md), ADR-007-030 |
| Documented external credential blocker | [CODEX_CREDENTIAL_BOUNDARY.md](./CODEX_CREDENTIAL_BOUNDARY.md), #70 |
| Upstream and standing regression workflow | [UPSTREAM_SYNC.md](./UPSTREAM_SYNC.md), [REGRESSION_BASELINE.md](./REGRESSION_BASELINE.md), ADR-014/015 |

This document controls remaining scope; it does not weaken security, data
integrity, architecture decisions or required regression gates to force a date.
Necessary fixes must be justified against an agreed criterion, not invented as
new product goals.
