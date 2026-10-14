# Multica Fork — Project Blueprint

> **Status:** Foundation / Architecture documentation  
> **Fork:** `Carcara-Studio/multica`  
> **Upstream:** `multica-ai/multica`  
> **Reference project:** `paperclipai/paperclip`  
> **Investigated upstream baseline:** `52b26900c51f759b87707b4ea35fa9f141b83b52`  
> **Snapshot date:** 2026-10-14  
> **Current phase:** Foundation → Regression baseline → Incremental implementation

---

## 1. Mission

Build and maintain an upstream-compatible Multica fork that adds two major capabilities:

1. project-scoped multi-level Agent hierarchy;
2. primary execution target plus automatic fallback/recovery.

The fork must remain recognizably Multica and continuously absorb upstream changes.

The primary engineering question for every structural decision is:

> How will this change affect our ability to incorporate future Multica upstream updates?

When implementation convenience conflicts with long-term upstream maintainability, prefer maintainability unless it makes the requirement functionally impossible.

---

## 2. Core Engineering Principles

Prefer:

1. additive changes;
2. isolated modules;
3. backward-compatible migrations;
4. optional configuration;
5. preservation of existing contracts;
6. default behavior equivalent to upstream;
7. few modified upstream hotspots;
8. small cohesive commits;
9. regression coverage;
10. reuse of upstream mechanisms before inventing fork-specific ones.

Avoid:

- rewriting central subsystems;
- duplicating existing Multica logic;
- broad cross-cutting refactors;
- unnecessary contract changes;
- modifying upstream files when a fork-specific extension is sufficient;
- relying on unstable implementation details without tests.

Before structural work:

```text
inspect upstream
    ↓
confirm current behavior
    ↓
identify real files/contracts
    ↓
separate fact from inference
    ↓
compare alternatives
    ↓
ADR
    ↓
Issue
    ↓
implementation
```

---

## 3. Product Objective A — Project-scoped Multi-level Hierarchy

The fork must support deep Agent reporting relationships within a Project.

Example:

```text
Project Root
├── CTO
│   ├── Backend Lead
│   │   └── Backend Engineer
│   └── Frontend Lead
└── Product Lead
    └── Designer
```

The hierarchy is **per Project**, never global on the Agent.

The same Agent may:

- participate in multiple Projects;
- hold different titles in each Project;
- report to different managers in each Project.

Required invariants:

- one active root per Project hierarchy;
- one direct manager per active non-root membership;
- arbitrary hierarchy depth;
- no self-reference;
- no direct cycle;
- no transitive cycle;
- no cross-Project manager relation;
- delegation down the chain;
- escalation up the chain;
- explicit behavior for manager/root removal and Agent archival;
- safe concurrent mutation;
- protection against delegation/re-trigger loops.

---

## 4. Confirmed Upstream Architecture Relevant to Hierarchy

The following facts were confirmed against the investigated upstream baseline.

### 4.1 Projects

Relevant upstream areas include:

```text
server/migrations/034_projects.up.sql
server/internal/handler/project.go
packages/core/types/project.ts
```

Confirmed behavior:

- Project is a first-class entity;
- Project is workspace-scoped;
- Issue has nullable `project_id`;
- `project_id` uses `ON DELETE SET NULL` on Issue;
- Project lead supports both `member` and `agent`.

Consequence:

> Existing Project lead semantics are not equivalent to the proposed operational Agent hierarchy root.

The hierarchy must not silently redefine the current Project lead contract.

### 4.2 Squads

Relevant upstream areas include:

```text
server/migrations/084_squad.up.sql
server/internal/handler/squad.go
server/internal/handler/squad_briefing.go
server/internal/service/builtin_skills/multica-platform/references/squads.md
server/migrations/127_task_squad_id.up.sql
```

Confirmed behavior:

- Squad is workspace-scoped;
- Squad has a required Agent leader;
- Squad has members;
- `squad_member.role` is roster/context information, not operational hierarchy;
- Squad assignment routes to `leader_id`;
- Squad mention routes to `leader_id`;
- Squad autopilot resolves to the leader;
- Squad members are not automatically fanned out;
- the leader receives a Squad operating briefing.

Decision direction:

> Squad and Project Hierarchy are separate domains.

A Squad may be reused across Projects while the same Agents hold different Project reporting relationships.

### 4.3 Issues and child work

Relevant upstream areas include:

```text
server/internal/handler/issue.go
server/internal/service/issue_wakeup_system.go
server/internal/handler/issue_system_wakeup.go
packages/core/types/issue-wakeup.ts
docs/engineering/issue-wakeups.md
```

Confirmed behavior:

- Issue may belong to a Project;
- Issue assignment is independent from Project membership;
- child/sub-issue completion can wake a parent assignee;
- `child_done`/stage wakeups already exist;
- parent completion remains Agent-managed rather than being automatically forced by the server.

This existing machinery should be reused for hierarchical delegation/completion reporting where appropriate.

---

## 5. Recommended Hierarchy Data Model

### Accepted architectural direction

Introduce a Project-scoped Agent membership entity rather than adding `manager_id` to the global Agent.

Conceptual table:

```text
project_agent_membership
------------------------
id
project_id
agent_id
manager_membership_id nullable
title
operational_role
sort_order nullable
archived_at nullable
created_at
updated_at
```

The manager reference targets another **membership**, not the global Agent.

This expresses:

> this Agent, in this Project participation, reports to this other Project participation.

### Required database invariants

Conceptually:

```text
UNIQUE(project_id, agent_id)
```

Manager must belong to the same Project, preferably enforced with a composite relationship such as:

```text
(project_id, manager_membership_id)
→ (project_id, id)
```

Self-reference must be rejected.

A partial unique index should enforce one active root per Project.

The structural decisions above are accepted by ADR-001 and ADR-003. Exact table, column, constraint and index names remain implementation proposals and must be revalidated against the current upstream migration conventions before code is written.

---

## 6. Hierarchy Mutation and Concurrency

Hierarchy correctness is more important than maximizing write concurrency because structural changes are infrequent.

Recommended mutation flow:

```text
BEGIN
  ↓
lock Project row
  ↓
load target membership + proposed manager
  ↓
recursive descendant/cycle validation
  ↓
apply mutation
  ↓
validate root invariant
  ↓
COMMIT
```

Project-level serialization prevents races such as:

```text
Tx A: A reports to B
Tx B: B reports to A
```

where both transactions might independently pass a non-serialized cycle check.

Removal policy direction:

- leaf removal may archive/remove the membership;
- manager with active descendants must not be silently removed;
- root removal requires explicit replacement/reorganization;
- Agent archival must fail closed when it would leave active descendants under an unusable manager/root;
- no silent reparenting.

These invariants are implementation authority under accepted ADR-003 and ADR-006.

---

## 7. Hierarchy Runtime Semantics

Hierarchy augments existing assignment; it does not become a new `assignee_type`.

Existing assignment remains valid:

```text
Issue → Agent
Issue → Squad
Issue → Member
```

Hierarchy context applies when an Issue has a Project and the executing Agent has a membership in that Project.

### Squad-assigned Project Issue

Keep the current entry point:

```text
Issue
  ↓
Squad
  ↓
squad.leader_id
```

If the Issue has `project_id`, resolve the executing leader's Project membership and provide hierarchy context.

Do not automatically replace the Squad leader with the Project hierarchy root.

This separation is governed by accepted ADR-002.

### Hierarchical delegation

Accepted direction under ADR-004:

```text
manager receives work
      ↓
resolve Project membership
      ↓
validate direct report
      ↓
create/assign child Issue
      ↓
worker executes
      ↓
existing child completion wakeup
      ↓
manager reviews result
```

Ordinary `@mention` remains a collaboration mechanism; it is not organizational delegation.

### Escalation

Accepted ADR-005 requires escalation to move one edge at a time:

```text
Worker → Manager → Manager's Manager → Root → Human boundary
```

Do not silently skip an unavailable manager.

---

## 8. Product Objective B — Primary Execution Target + Fallback

An Agent may have:

- its existing primary execution configuration;
- an optional fallback execution configuration.

Failover occurs only when canonical Multica failure classification provides reliable evidence of primary quota/balance exhaustion.

After activation:

- future normal executions use fallback;
- primary is considered temporarily unavailable;
- primary is probed every 30 minutes;
- probe is control-plane work, not user work;
- successful recovery returns future executions to primary automatically.

No ambiguous partial execution is blindly replayed on fallback.

These semantics are implementation authority under accepted ADR-007 through ADR-013.

---

## 9. Confirmed Upstream Architecture Relevant to Failover

### 9.1 Agent/runtime configuration

Relevant upstream areas include:

```text
server/internal/handler/agent.go
server/pkg/db/queries/agent.sql
packages/core/types/agent.ts
server/internal/daemon/types.go
server/internal/service/task.go
```

Confirmed Agent execution concepts include:

```text
runtime_id
runtime_config
custom_env
custom_args
model
thinking_level
service_tier
```

Therefore fallback must not be represented as only `fallback_model`.

A fallback may require a different runtime/provider, model, reasoning setting, service tier, credentials/configuration, context window, or capability set.

### 9.2 Canonical failure classification

Relevant upstream package:

```text
server/pkg/taskfailure/failure.go
server/pkg/taskfailure/classify.go
```

Important canonical reasons include:

```text
agent_error.provider_auth_or_access
agent_error.provider_quota_limit
agent_error.provider_capacity_or_rate_limit
agent_error.provider_server_error
agent_error.provider_network
agent_error.context_overflow
agent_error.missing_config
agent_error.model_not_found_or_unavailable
agent_error.process_failure
agent_error.agent_timeout
agent_error.unknown
```

Critical fact:

> Multica already distinguishes quota/balance exhaustion from transient capacity/rate-limit failures.

Accepted v1 failover trigger under ADR-008:

```text
route == PRIMARY
AND
canonical failure_reason == agent_error.provider_quota_limit
```

Do not build a parallel quota classifier unless upstream evidence later proves the canonical taxonomy insufficient and a later ADR authorizes a policy change.

### 9.3 Daemon control plane

Relevant upstream areas include:

```text
server/internal/handler/runtime_models.go
server/internal/handler/daemon.go
server/internal/daemon/client.go
server/pkg/protocol/messages.go
```

Current runtime model discovery demonstrates an existing control-plane pattern:

```text
server stores pending request
        ↓
daemon heartbeat receives request
        ↓
daemon executes local/provider operation
        ↓
daemon reports result to server
```

This is the accepted conceptual precedent for primary recovery probes under ADR-010.

Multica also already uses durable PostgreSQL worker patterns including `FOR UPDATE SKIP LOCKED` in distributed work.

---

## 10. Recommended Failover Data Model

### Accepted architectural direction

Keep existing Agent execution fields as the primary configuration for backward compatibility.

### Proposed fallback configuration shape

```text
agent_fallback_config
---------------------
agent_id PK
runtime_id
model
thinking_level
service_tier
runtime_config optional
custom_args optional
created_at
updated_at
```

Secret-bearing configuration should reuse existing Multica credential/environment mechanisms rather than creating a parallel secret system.

### Proposed operational state shape

```text
agent_failover_state
--------------------
agent_id PK
status
failure_reason
primary_failed_at
next_probe_at
last_probe_at
recovered_at
probe_claim_token
probe_lease_until
version
last_primary_failure_task_id
updated_at
```

Conceptual states:

```text
PRIMARY_ACTIVE
FALLBACK_ACTIVE
PRIMARY_PROBING
```

Configuration and operational state remain separate under ADR-009. Exact table/column names remain implementation proposals pending current migration and runtime revalidation.

---

## 11. Failover State Machine

Accepted under ADR-008 and ADR-009:

```text
PRIMARY_ACTIVE
   |
   | canonical primary provider_quota_limit
   v
FALLBACK_ACTIVE
   |
   | next_probe_at reached
   v
PRIMARY_PROBING
   |
   +-- primary probe success ----------> PRIMARY_ACTIVE
   |
   +-- quota still exhausted ----------> FALLBACK_ACTIVE + 30m
   |
   +-- transient/inconclusive failure -> FALLBACK_ACTIVE + 30m
```

V1 trigger policy:

| Failure | Activate fallback? |
|---|---|
| quota / balance exhaustion | yes |
| rate limit | no |
| provider capacity | no |
| provider 5xx | no |
| network | no |
| auth/access | no |
| timeout | no |
| context overflow | no |
| invalid model/config | no |
| internal Agent/process failure | no |

Existing upstream retry behavior remains independent from the new failover policy unless a later accepted ADR explicitly changes it.

---

## 12. Execution Continuity and Side-effect Safety

ADR-012 establishes that a quota failure does **not** automatically mean the current task is safe to replay.

Potential side effects include:

- GitHub writes;
- database writes;
- filesystem changes;
- external APIs;
- MCP/tool calls;
- comments/messages;
- provider-specific tool execution.

Accepted safety rule:

```text
primary execution fails with quota
        ↓
activate fallback for future executions
        ↓
do not blindly replay the failed execution
```

A future optimization may allow immediate replay only if a runtime/provider adapter can prove that the request was rejected before generation/tool/side-effect execution, and that change requires a separate accepted ADR.

---

## 13. Durable Per-task Routing

Recovery may race with queued work.

Example:

```text
Task A is routed while Agent is FALLBACK_ACTIVE
Primary recovers before Task A is claimed
```

Task A must still use the target chosen when it was routed.

ADR-007 accepts an additive per-task route identity while reusing the existing persisted task `runtime_id`.

Preferred conceptual shape:

```text
agent_task_execution_route
--------------------------
task_id PK
route
```

Absence means the normal primary/upstream route. A fallback route is persisted transactionally with task creation/routing.

Exact table naming and integration points must be re-investigated before implementation.

---

## 14. 30-minute Primary Recovery Probe

ADR-010 and ADR-011 establish that probe scheduling belongs to the server/control plane while probe execution belongs to the daemon/runtime.

Accepted flow:

```text
FALLBACK_ACTIVE and next_probe_at <= now
        ↓
server claims due row with durable locking
        ↓
PRIMARY_PROBING + claim token + lease
        ↓
daemon receives capability-gated probe request
        ↓
minimal primary provider/model request
        ↓
result returned with claim token
        ↓
compare-and-set state transition
```

Probe requirements:

- use primary runtime/provider/model/credentials;
- no Issue context;
- no normal task history;
- no session resume;
- tools disabled;
- MCP disabled;
- minimal provider-compatible input/output;
- lowest practical cost;
- no external side effects;
- excluded from normal user execution/activity metrics.

Proposed protocol names such as `pending_primary_probe` or capability `primary-probe-v1` are **not upstream facts** and remain implementation details.

Late results from expired claims must not restore primary.

Old daemons without probe capability remain compatible; fallback stays active and recovery status must explain that automatic probing is unavailable.

---

## 15. Cross-provider Fallback

ADR-013 accepts cross-provider fallback when a deterministic compatibility contract passes.

Validation must cover more than model identity:

- runtime existence and Workspace ownership;
- credentials/access;
- model availability;
- thinking/service-tier compatibility;
- context window;
- tool support;
- structured output;
- multimodal capability;
- runtime-specific configuration.

If the fallback cannot satisfy capabilities required by the execution, or a required capability cannot be established reliably, fail closed before partial work rather than silently degrading behavior.

Fallback quota exhaustion does not cause uncontrolled ping-pong to primary. Primary recovery continues through the probe mechanism.

---

## 16. Concurrency Requirements

### Hierarchy

- concurrent reparent operations cannot create a cycle;
- root uniqueness remains valid under races;
- structural writes are serialized sufficiently to make recursive validation trustworthy.

### Failover

- multiple simultaneous quota failures produce one logical transition;
- multiple server replicas do not create probe storms;
- probe claims use durable locking/leases;
- duplicate or stale probe results are idempotent no-ops;
- state survives server/daemon restart;
- a task already routed to fallback remains fallback after recovery;
- already-running fallback work is not interrupted by primary recovery.

These failover concurrency invariants are implementation authority under ADR-011.

---

## 17. Proposed Isolated Modules

The following paths are **proposals**, not claims that they exist upstream.

### Hierarchy

```text
server/internal/projecthierarchy/service.go
server/internal/projecthierarchy/validation.go
server/internal/projecthierarchy/briefing.go
server/internal/handler/project_hierarchy.go
server/pkg/db/queries/project_hierarchy.sql
```

### Failover

```text
server/internal/failover/service.go
server/internal/failover/state_machine.go
server/internal/failover/target_resolver.go
server/internal/failover/probe_worker.go
server/internal/handler/agent_failover.go
server/pkg/db/queries/agent_failover.sql
server/internal/daemon/primary_probe.go
```

Do not choose migration sequence numbers until implementation starts and the current upstream migration head is revalidated.

---

## 18. Existing Upstream Hotspots to Minimize

Likely high-conflict areas include:

```text
server/internal/service/task.go
server/internal/handler/agent.go
server/internal/handler/daemon.go
server/pkg/protocol/messages.go
server/internal/daemon/
server/internal/handler/squad_briefing.go
```

Fork logic should live in isolated modules with narrow hooks into these files.

`server/internal/service/task.go` deserves special caution because it is central runtime orchestration and likely to evolve upstream.

---

## 19. Backward Compatibility

Required default behavior:

- Project without hierarchy → upstream behavior;
- existing Squad → upstream behavior;
- Agent without fallback config → upstream behavior;
- Issue without Project → no hierarchy behavior;
- existing API fields remain optional/unchanged where possible;
- old daemon remains valid for normal execution;
- probe functionality is capability-gated;
- migrations are additive and require no legacy backfill unless later evidence proves otherwise.

Prefer new subresources/endpoints over making existing request fields mandatory.

---

## 20. Testing Strategy

### Hierarchy

Required coverage includes:

- self-reference;
- direct/transitive cycles;
- cross-Project manager relation;
- root uniqueness;
- explicit reparent/remove semantics;
- Agent archival consistency;
- concurrent A→B/B→A mutation;
- Squad + Project hierarchy interaction;
- direct Agent assignment + hierarchy;
- delegation idempotency;
- child completion wakeup to manager;
- escalation one edge at a time.

### Failover

Required coverage includes:

- canonical quota-only activation;
- non-quota failure matrix;
- multiple simultaneous quota failures;
- fallback routing persistence;
- same-provider and cross-provider targets;
- invalid/incompatible fallback;
- due probe claiming;
- lease expiry/crash recovery;
- duplicate/stale probe results;
- recovery race with queued/running fallback tasks;
- ambiguous partial execution is not blindly replayed.

### Regression

Pin unchanged upstream behavior for:

- direct Agent assignment;
- Squad assignment;
- Squad mention/autopilot;
- Squad briefing;
- child completion wakeups;
- existing retry behavior;
- Agent archive behavior;
- normal runtime/model selection.

---

## 21. ADR Status

### Accepted Project Hierarchy ADRs — 2026-10-14

```text
ADR-001 — Project-scoped Agent Hierarchy                         ACCEPTED
ADR-002 — Squad remains workspace-scoped                        ACCEPTED
ADR-003 — Hierarchy integrity and cycle prevention              ACCEPTED
ADR-004 — Hierarchical delegation semantics                     ACCEPTED
ADR-005 — Escalation semantics                                  ACCEPTED
ADR-006 — Agent archive and hierarchy consistency               ACCEPTED
```

These six ADRs are implementation authority for Project Hierarchy work unless superseded by a later accepted ADR.

### Accepted Failover ADRs — 2026-10-14

```text
ADR-007 — Execution Target abstraction                          ACCEPTED
ADR-008 — Canonical quota-triggered failover                    ACCEPTED
ADR-009 — Persisted failover state machine                      ACCEPTED
ADR-010 — 30-minute primary recovery probe                      ACCEPTED
ADR-011 — Failover concurrency and probe leases                 ACCEPTED
ADR-012 — No blind retry after ambiguous partial execution      ACCEPTED
ADR-013 — Cross-provider compatibility policy                   ACCEPTED
```

These seven ADRs are implementation authority for primary/fallback failover work unless superseded by a later accepted ADR.

### Governance

```text
ADR-014 — Upstream compatibility strategy
```

---

## 22. Technical Backlog

### Foundation

```text
MFK-001 — Create and configure upstream-compatible fork                DONE
MFK-002 — Establish repository governance and sync policy              DONE
MFK-003 — Add fork architecture and decision documentation             DONE
MFK-004 — Record hierarchy ADRs                                        DONE
MFK-005 — Record failover ADRs                                         DONE
MFK-006 — Establish upstream regression baseline                       NEXT
MFK-007 — Activate Project Hierarchy implementation
```

### Hierarchy

```text
HIER-001 — Introduce Project hierarchy schema
HIER-002 — Implement ProjectHierarchyService
HIER-003 — Enforce cycle-safe concurrent mutations
HIER-004 — Implement hierarchy API
HIER-005 — Implement hierarchy CLI
HIER-006 — Add Project hierarchy briefing
HIER-007 — Add hierarchy-aware delegation
HIER-008 — Integrate completion wakeups
HIER-009 — Add escalation semantics
HIER-010 — Protect Agent archive from hierarchy corruption
HIER-011 — Add Project hierarchy Web UI
HIER-012 — Add Mobile hierarchy support
```

### Failover

```text
FAIL-001 — Add fallback execution configuration
FAIL-002 — Implement ExecutionTargetResolver
FAIL-003 — Add persisted failover state
FAIL-004 — Integrate canonical quota signal
FAIL-005 — Add durable per-task fallback routing
FAIL-006 — Route future tasks to fallback
FAIL-007 — Add primary probe scheduler
FAIL-008 — Add daemon probe capability
FAIL-009 — Implement minimal provider-aware probes
FAIL-010 — Implement automatic recovery
FAIL-011 — Add probe crash recovery
FAIL-012 — Add cross-provider validation
FAIL-013 — Add observability and UI status
FAIL-014 — Add failover concurrency test suite
FAIL-015 — Add upstream regression suite
```

Implementation ordering remains hierarchy first, failover second, unless new evidence changes the dependency picture.

---

## 23. Known / Inferred / Unknown

### Confirmed from investigated upstream

- Projects are first-class and workspace-scoped;
- Issue has optional `project_id`;
- Squad is workspace-scoped;
- Squad has leader and members;
- Squad member role is context, not operational hierarchy;
- Squad work routes through leader;
- child/sub-issue wakeups exist;
- Agents have runtime/model configuration;
- canonical provider failure classification exists;
- quota and capacity/rate-limit are distinct failure reasons;
- daemon heartbeat already carries control-plane requests;
- upstream uses PostgreSQL durable worker locking patterns.

### Accepted Project Hierarchy decisions

- hierarchy is a separate Project-scoped membership domain;
- global Agent manager is incorrect for the requirement;
- Project lead is not automatically hierarchy root;
- Squad remains workspace-scoped and separate from Project Hierarchy;
- hierarchy is not a new assignee type;
- adjacency-list membership is the v1 structural model;
- structural hierarchy mutations serialize on the Project row;
- delegation moves one direct-report edge at a time and reuses child Issues;
- escalation moves one direct-manager edge at a time;
- unavailable managers are not silently skipped;
- root escalation terminates at a human/project intervention boundary;
- manager/root removal never silently reparents descendants;
- manager/root Agent archival fails closed until explicit reorganization;
- leaf hierarchy memberships may deactivate atomically with Agent archive;
- restoring an Agent does not reactivate old memberships automatically.

### Accepted Primary/Fallback failover decisions

- existing Agent execution fields remain the canonical primary target;
- fallback is an optional additive execution target, not a second Agent;
- fallback route choice is durable per task while reusing existing persisted `task.runtime_id`;
- automatic failover activates only for primary canonical `agent_error.provider_quota_limit` with a valid fallback;
- ordinary upstream retry policy remains separate and unchanged;
- failover configuration and operational state are persisted separately;
- normal user work remains on fallback while primary is being probed;
- primary recovery is proven only by a successful minimal primary inference probe;
- probes run every 30 minutes, are control-plane work, and use durable token/lease fencing;
- stale/duplicate probe results are no-ops;
- the quota-triggering task is not blindly replayed onto fallback;
- same- and cross-provider fallback require deterministic compatibility validation;
- known incompatibility or unknown required capability fails closed;
- cross-runtime/provider fallback never reuses a primary provider session.

### Must be revalidated before implementation

- exact common enqueue hook for target resolution;
- exact final failure-settlement hook for quota transition;
- Agent archive transaction boundaries;
- fallback credential representation using existing mechanisms;
- runtime/provider capability matrix;
- provider-specific minimal probe semantics;
- current migration sequence;
- current audit/metrics extension points;
- any upstream Project/Squad/runtime changes after the snapshot.

---

## 24. Development Workflow

Initial implementation model:

```text
Human owner
    ↓
Architecture / ADR review
    ↓
GitHub Issue
    ↓
Local implementation Agent
    ↓
feature branch
    ↓
implementation + tests
    ↓
Pull Request
    ↓
review
    ↓
human-approved merge
```

The implementation Agent may inspect/edit/run tests/open PRs, but it must not silently redefine accepted architecture or perform broad unrelated refactors.

After the process stabilizes, development may move to a controlled Multica Squad and eventually dogfood the Project Hierarchy feature itself.

---

## 25. Current Foundation Status

Completed in repository history or by this MFK-005 change set:

```text
MFK-001 — fork baseline and origin/upstream setup
MFK-002 — main protection, governance, upstream sync policy
MFK-003 — repository-resident architecture and ADR conventions
MFK-004 — Project Hierarchy ADR-001 through ADR-006 accepted
MFK-005 — Primary/Fallback ADR-007 through ADR-013 accepted
```

Fork base for MFK-005:

```text
feadd478fb90abddd0aea64e66ae6616add7ac27
```

Current investigated upstream `main`:

```text
52b26900c51f759b87707b4ea35fa9f141b83b52
```

No fork-specific product behavior has been implemented yet.

The immediate next foundation work after MFK-005 is MFK-006: establish the upstream regression baseline before any hierarchy implementation begins.

---

## 26. Project Rule

> No structural implementation begins from an informal idea alone.

Every structural change must be traceable:

```text
evidence → architecture → ADR → Issue → branch → tests → PR → review → merge
```

This rule exists to protect correctness and the ability to continuously absorb Multica upstream changes.
