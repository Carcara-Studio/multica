# Remaining delivery checklist

This is the single current execution-scope checklist for the five remaining
items agreed with the maintainer on 2026-10-21. Consolidated by #96 after #95;
repository baseline `fe00de5a5cf64f6c3952fbe4f7e4b1889ddef69b`.
Code/tests and accepted ADRs retain architectural authority under GOVERNANCE.md.
Older blueprint candidate lists describe history/design options; they do not
independently authorize additional delivery work.

## Fixed scope and completion criteria

On2026-10-29 the maintainer explicitly paused R4 and prioritized persistent memory.
The bounded project continuity pilot (#159, ADR-040) is an approved scope addition;
its current acceptance contract is [PROJECT_CONTINUITY.md](PROJECT_CONTINUITY.md).
It does not reopen R1-R3/R5 or activate R4. The R4 row below describes unfinished
work retained for a later explicit resume; no fallback work is active now.

R1 is delivered by #98 / PR #99, R2 by #100 / PR #101 and R3 by #102 / PR #103; R4 is in implementation and remains disabled; R5 passed its bounded Windows/account and accepted upstream-baseline criteria on 2026-10-27. A helper, audit, mock test
or foundation PR alone is not completion of an item. Maintain evidence here as
work lands; do not replace these identifiers with new phases.

| ID | Agreed delivery | Current status | Completion criteria |
| --- | --- | --- | --- |
| R1 | Complete escalation operation | Complete: #98 / PR #99, ADR-033; native PG17 manager/root, replay/collision and current-credential authorization coverage. Native dispatch crash recovery remains R3 | Persist reason/context and source/target lineage with stable request identity; retries cannot duplicate the request or change its intent silently. Route one direct-manager edge using native permission/readiness and execution fences. Unavailable manager is not skipped. Root reaches a durable, visible human-intervention condition without an Agent/Project-lead loop. Preserve original work/partial execution; no automatic reassignment or replay. Integration tests prove delivery, replay/collision and root behavior. |
| R2 | Expose escalation | Complete: #100 / PR #101, ADR-034; MAT-only API/CLI, bounded briefing and shared web/desktop decision management. Native PG17 and changed client regression gates required before merge | Provide the API, CLI, bounded Agent instructions and management UI for the R1 operation. Validate the current task credential, actor/Issue binding and permissions; reject unauthorized/unavailable targets. Show source, destination/root boundary, reason/state and repair needs. Preserve successful existing contracts and test the changed client surfaces. The relation helper alone must not be advertised as executable escalation. |
| R3 | Recover committed operations whose execution was not dispatched | Complete: #102 / PR #103, ADR-035; atomic native task/receipt registration, native restart/concurrency/rollback and migration coverage passed PostgreSQL 17/race CI 39658187665 | A server failure between durable registration and native enqueue can be detected and recovered without duplicate work or blind replay of ambiguous partial execution. Define and prove the recovery/reconciliation rule against native execution fences, including restart/concurrency tests. Cover the delivered delegation boundary and applicable escalation delivery; do not create a second general executor. |
| R4 | Complete automatic model fallback and recovery | In implementation, disabled: maintainer approved limited native-login path availability (ADR-038); strict credential evidence remains unavailable; route/probe integration unfinished | Implement the approved ADR-038 native-login path mode with trustworthy run/turn-bound native quota evidence and current target/configuration authorization (no exact-account claim); retain strict credential mode disabled. Integrate all required dispatch/claim/retry writers, switch only on accepted quota evidence, route subsequent work to fallback, and test the primary every 30 minutes with isolated inference. Recover safely when the primary is available. Prove concurrency/crash/partial-execution behavior and the accepted provider compatibility policy. Automatic fallback/probes stay disabled until the evidence and execution gates actually pass; control-plane tests cannot close this item. |
| R5 | Real-environment validation and upstream compatibility | Complete for the validated local Windows environment: accepted upstream c9b6de856 integration, real single/mixed account execution, scoped MXC and final desktop verification (#114, 2026-10-27). R4 activation and broader rollout are separate | Run the bounded installed CLI/daemon/application smoke with the maintainer's real Windows/account environment when authorized, record exact evidence and limitations, and resolve regressions in R1-R4. Incorporate the reviewed upstream UI advance through the normal isolated synchronization workflow (prior integrated baseline 2b645691387e1535cc63d6ebf2ba4e996387c8b2; sync target c9b6de8567d7bb57bf9a2a3220207cf2b3e5a038 includes the reviewed UI advance e0e9efc5f1d6ff2a88ad2d7844d4d76f2519985d). Revalidate upstream for affected changes and verify the final integrated behavior. No real-account smoke is claimed from fake HTTP/agent or server tests. |

Recommended order: R1, then R2, then R3. R4's independent necessary work may
proceed when supported by evidence, but its external blocker is not permission
to invent an alternative product. R5 includes continuous compatibility work and
a final real-environment pass; it is not an unlimited feature-development bucket.
Upstream will continue moving after a validated baseline: later compatibility
maintenance is ongoing maintenance, not a new condition that postpones project
completion indefinitely.

## R4 native probe feasibility

[Codex request isolation capture](CODEX_PROBE_ISOLATION.md) verifies a pinned,
opt-in native CLI request to a localhost fake provider, including a negative
control for residual goal tools/skill instructions. It does not prove real
inference, native-login recovery or absence of all startup side effects. R4 stays
disabled pending trusted run/config binding, route integration and real isolated
probe execution/settlement.

## Current Windows validation evidence (2026-10-27)

The older incident notes below describe earlier test states. The later
[Windows validation record](WINDOWS_VALIDATION_2026-10-27.md) supersedes their
pending-smoke statements for the exact cases tested. On main `4e39c7dcf`, real
single-agent and mixed OpenCode/Codex execution, expected authorship, native
manager continuation and the corrected `in_review`/operator-acceptance workflow
passed. Explicit MXC avoided legacy provisioning in fresh Codex task homes on
the tested machine. This is scoped test-agent evidence, not global deployment,
a general fix for legacy elevated provisioning, or R4 activation.

After maintainer login, the updated development desktop was verified on the Dev
workspace: the mixed parent showed accepted children, native wakeup continuation
and In Review delivery; the Codex agent displayed its explicit MXC custom args.
The desktop daemon ran documentation-only successor `dea9d117f` with zero active
tasks. This completes the scoped application handoff; the sole Codex agent in
that workspace already has MXC. It does not roll out settings to other workspaces
or the installed global application. The already integrated upstream UI baseline
is accepted; following every later upstream commit, repeating identical smoke
tests, and testing another device are not new conditions for completion.

## Scope controls

R5 #118 extends ADR-036 query eligibility to the DEV-13 continuation-gap
diagnosis. The native proof predicate is unchanged: the current persisted
assistant must have stopped and the turn must end in successful idle. A pending
continuation, failed turn or unavailable proof remains failed. This does not
establish whether free-model availability caused the maintainer's failure,
does not add inference/replay, and does not close #114 or R5.

R5 follow-up #116 / ADR-036 records the post-#115 identity test: each monitor's
visible marker and status had the correct author, but three task attempts per
monitor failed with an open step at EOF. Native OpenCode 2 completion proof now
consults only a bounded sanitized session export after clean process exit and
requires the current assistant plus native successful idle. Missing proof keeps
the original failure. Fake runtime tests are not a real Windows/account pass;
Codex UAC, the old branched run and full R5 completion remain unresolved.

R5 real Windows evidence from 2026-10-22 is tracked in #114. Basic application,
model-selected execution and single-child delegation/manager resumption passed
with maintainer review. Branched mixed-harness completion did not pass: OpenCode
network/unknown failures, repeated mathematics leaf runs after In Review,
premature tutor review, a changed-intent replay possibility and a Portuguese
comment/status attributed to the mathematics monitor remain under investigation.
Supplied mathematics logs do not establish cross-issue writes. Sixteen Codex
sandbox-setup UAC prompts were reported; no precise run-to-prompt mapping or
sandbox fix is claimed. R4 remains blocked and R5 remains partial.

The #114 runtime fix isolates authenticated built-in OpenCode 2 task launches
using its native `run --standalone`: a resident server retains its startup
environment, so CLI-only task credentials cannot safely be delivered through it.
Conflicting custom server/standalone flags are filtered for those task launches.
OpenCode 1, unknown versions, custom runtime profiles and the existing MCP
refusal retain their contracts. Existing process-tree cancellation owns the
private server. This corrects environment delivery; it does not prove the cause
of the DEV-10 author or suppress native provider-network retries after partial
side effects. Request-hash replay protection and Windows sandbox policy are
unchanged. Official contract references:
https://opencode.ai/v2/docs/cli/commands and
https://opencode.ai/v2/docs/network/ .

An installed-agent-free fixture models resident environment retention and checks
distinct task identities and custom-server rejection. This slice changes one
runtime implementation and its existing test file, with no API/schema/migration,
queue or failover addition; it stays within ADR-014/021 and R5. A new real Windows
run is still needed after delivery; fake fixtures cannot close that criterion.

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

### R5 Windows development process identity (#108)

The Windows setup reached PostgreSQL migrations and API startup on port 18198,
but the managed launcher rejected ownership and stopped the API (exit 143).
Git Bash process IDs and Unix listener tools cannot establish native Windows
process ownership. The isolated Windows helper records native launcher and
listener PIDs together with creation times, validates parent ancestry, and
terminates only verified process identities. Commit/start-time health checks
remain mandatory. Native CI uses Node fixture processes, including an unrelated
listener and a stale receipt; it does not exercise installed agents or accounts.
R5 remains open until the user's app/daemon/provider smoke succeeds; R4 remains
blocked on the existing producer evidence gate.

### R5 Desktop Go build environment (#110)

After #108 / PR #109 the Windows API passes ownership/commit/start-time health
validation and the database is reachable and migrated. Desktop reaches CLI
bundling, where Turbo strict filtering removes native Go cache/profile variables
and Go reports that neither GOCACHE nor LocalAppData is defined. Turbo's explicit
pass-through list preserves native profile, cache and temporary directories
without hashing machine-specific paths or disabling strict filtering. A fixture
uses the pinned Turbo, checks unrelated variables stay filtered, and builds a
tiny native Go binary with default and explicit cache directories (including
paths with spaces) in Windows CI. R5 real app/account/provider validation and
R4's producer evidence gate remain open.

### R5 Windows auxiliary console visibility (#112)

The user reached Environment ready on 493a156bf but observed two persistent
black terminal windows. The Windows launch helper's Node detached flag does not
provide hidden descendant-console semantics. Native launch now gives each
managed tree its own hidden console, with only NUL/log handles inherited and
native creation identity recorded before returning. PID/birth ownership and
verified stop remain unchanged; the installed Multica instance is outside this
managed tree. Windows fixture checks cover hidden root/child consoles, lifetime
after the invoking shell exits, shared stdout/stderr logging and paths with
spaces, alongside existing stale-identity/unrelated-listener protections.
Actual Desktop window visibility and the user's console observation still need
local confirmation; R5 and R4's producer evidence gate remain open.
