# Remaining delivery checklist

This is the single current execution-scope checklist for the five remaining
items agreed with the maintainer on 2026-10-21. Consolidated by #96 after #95;
repository baseline `fe00de5a5cf64f6c3952fbe4f7e4b1889ddef69b`.
Code/tests and accepted ADRs retain architectural authority under GOVERNANCE.md.
Older blueprint candidate lists describe history/design options; they do not
independently authorize additional delivery work.

## Fixed scope and completion criteria

R1 is delivered by #98 / PR #99, R2 by #100 / PR #101 and R3 by #102 / PR #103; R4-R5 remain open. A helper, audit, mock test
or foundation PR alone is not completion of an item. Maintain evidence here as
work lands; do not replace these identifiers with new phases.

| ID | Agreed delivery | Current status | Completion criteria |
| --- | --- | --- | --- |
| R1 | Complete escalation operation | Complete: #98 / PR #99, ADR-033; native PG17 manager/root, replay/collision and current-credential authorization coverage. Native dispatch crash recovery remains R3 | Persist reason/context and source/target lineage with stable request identity; retries cannot duplicate the request or change its intent silently. Route one direct-manager edge using native permission/readiness and execution fences. Unavailable manager is not skipped. Root reaches a durable, visible human-intervention condition without an Agent/Project-lead loop. Preserve original work/partial execution; no automatic reassignment or replay. Integration tests prove delivery, replay/collision and root behavior. |
| R2 | Expose escalation | Complete: #100 / PR #101, ADR-034; MAT-only API/CLI, bounded briefing and shared web/desktop decision management. Native PG17 and changed client regression gates required before merge | Provide the API, CLI, bounded Agent instructions and management UI for the R1 operation. Validate the current task credential, actor/Issue binding and permissions; reject unauthorized/unavailable targets. Show source, destination/root boundary, reason/state and repair needs. Preserve successful existing contracts and test the changed client surfaces. The relation helper alone must not be advertised as executable escalation. |
| R3 | Recover committed operations whose execution was not dispatched | Complete: #102 / PR #103, ADR-035; atomic native task/receipt registration, native restart/concurrency/rollback and migration coverage passed PostgreSQL 17/race CI 39658187665 | A server failure between durable registration and native enqueue can be detected and recovered without duplicate work or blind replay of ambiguous partial execution. Define and prove the recovery/reconciliation rule against native execution fences, including restart/concurrency tests. Cover the delivered delegation boundary and applicable escalation delivery; do not create a second general executor. |
| R4 | Complete automatic model fallback and recovery | Blocked at activation: producer credential/evidence boundary remains unresolved; control-plane foundations exist | Obtain trustworthy request-bound evidence for the effective execution credential/target, integrate all required dispatch/claim/retry writers, switch only on accepted quota evidence, route subsequent work to fallback, and test the primary every 30 minutes with isolated inference. Recover safely when the primary is available. Prove concurrency/crash/partial-execution behavior and the accepted provider compatibility policy. Automatic fallback/probes stay disabled until the evidence and execution gates actually pass; control-plane tests cannot close this item. |
| R5 | Real-environment validation and upstream compatibility | Partial: upstream c9b6de856 integrated via #104 / PR #105; real Windows/account smoke still pending | Run the bounded installed CLI/daemon/application smoke with the maintainer's real Windows/account environment when authorized, record exact evidence and limitations, and resolve regressions in R1-R4. Incorporate the reviewed upstream UI advance through the normal isolated synchronization workflow (prior integrated baseline 2b645691387e1535cc63d6ebf2ba4e996387c8b2; sync target c9b6de8567d7bb57bf9a2a3220207cf2b3e5a038 includes the reviewed UI advance e0e9efc5f1d6ff2a88ad2d7844d4d76f2519985d). Revalidate upstream for affected changes and verify the final integrated behavior. No real-account smoke is claimed from fake HTTP/agent or server tests. |

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


## R1 delivery evidence (#98 / PR #99)

The native decision/intervention operation is specified in ADR-033 and
HIERARCHY_ESCALATION.md. PostgreSQL 17 race CI exercises service delivery,
concurrency/replay/collision, current MAT/originator/readiness admission,
root recipients, atomic rollback and workspace cleanup. The final PR head must
pass the full backend, migration, SQLC, agent, Windows and vulnerability gates
before integration. Initial CI 39641632646 passed handler (100.720s), service
(21.623s) and hierarchy (4.322s); its static migration guard caught an omitted
concurrent-index retry registration. The PR adds that native registration and
an isolated up/down/up migration test; the final full CI is the merge gate.
No real installed CLI, daemon or provider inference was used. Public operation
exposure remains R2, and native post-commit enqueue reconciliation remains R3.


## R2 delivery evidence (#100 / PR #101)

ADR-034 and HIERARCHY_ESCALATION.md describe the bounded task-token creation
contract, workspace-authorized paged history and native decision management.
Local Go CLI tests/vet, 52 affected core tests, 101 history/Issue-detail view
tests, core/views typechecks and affected ESLint passed. Native PG17/race CI
39650872334 exercised the new HTTP operation, authorization, replay, manager/
root delivery, diagnostics and pagination. Its only backend failure was a test
expecting 403 where the native middleware correctly conceals an outsider's
workspace as 404; the assertion is corrected without changing admission policy.
The final PR head must pass full native/backend, client, build, Windows and
mobile compatibility gates before integration. Final CI results remain attached
to PR #101. The response parser additionally rejects zero identities and
inconsistent manager/root routes; valid JSON-escaped maximum text is covered.

No real installed CLI/account/provider inference was performed. Local browser
screenshot installation failed because its downloaded archive was truncated;
component semantics/navigation and production client builds are the available
UI evidence. R3 still owns safe dispatch reconciliation; R4's activation blocker
and R5 real-environment/upstream synchronization remain open. No new top-level
work item is introduced.


## R3 delivery evidence (#102 / PR #103)

ADR-035 closes the new-operation enqueue gap with the native task and receipt
pointer in the Issue transaction, best-effort post-commit overlay/promotion and
native DB-clock deadline/polling recovery. It preserves native thread-scoped comment coalescing,
current admission and claim fences, and the root human inbox path. Native due
promoters recheck deferred status after row-lock waits; early promotion and
overlay refuse dispatch/start history. Replay never restarts terminal, partial,
deleted or ambiguous legacy history. No legacy backfill or second executor.

Local compilation, targeted tests and vet passed with DB-backed tests skipped
because local PostgreSQL is unavailable. Native PostgreSQL 17/race CI
39658187665 passed migrator (39.359s), handler (97.760s), hierarchy (3.789s),
service (20.632s), WeCom (25.495s), agents, Windows, SQLC and vulnerability gates.
It proves restart, singular/batch concurrent claims, thread-scoped comment merge,
rollback, late continuations and locked-row promotion races. First CI
39657248120 exposed a new fixture that ignored native comment-thread identity;
the fixture was corrected without changing production thread semantics. Its
unrelated WeCom quoted-image failure did not recur in the full passing run.
Final follow-up changes record documentation/evidence only; source and tests
remain identical to the validated head 35f39a5f3. Upstream c9b6de856 remained
current on final compatibility review; it is reviewed, not integrated here.
R4 activation and R5 real-environment/upstream synchronization remain unchanged.


## R5 upstream synchronization (#104)

The isolated synchronization to upstream c9b6de856 includes tooltips/shortcut
hints and dark surfaces, linked-PR conflict wakeups and managed Qoder runtime
support. The fork base is 33eef1224; upstream ancestry is preserved by merge,
not squash. [UPSTREAM_SYNC_20261021.md](./UPSTREAM_SYNC_20261021.md) records the
conflict resolution, compatibility boundaries and verification. This advances
R5's upstream criterion only; no real Windows/account/provider smoke is claimed.
R4 remains blocked by CODEX_CREDENTIAL_BOUNDARY.md, not solved by adding Qoder.


### R5 Windows database port collision (#106)

The maintainer's Windows setup has a native PostgreSQL process listening on
5432. Docker could not publish the managed development database there. The
local scripts already support POSTGRES_PORT and DATABASE_URL; the development
Compose mapping now honors POSTGRES_PORT (default 5432) on loopback while the
container stays on 5432. Choose an unused host port such as 15432 and set both
POSTGRES_PORT and the local DATABASE_URL port in the selected .env (or
.env.worktree). Keep the managed database name, credentials and volume. Existing
Windows PostgreSQL remains running; no database reset or production config
change is required. R5 remains open pending successful app/daemon/provider
smoke; this setup repair is not that evidence.
