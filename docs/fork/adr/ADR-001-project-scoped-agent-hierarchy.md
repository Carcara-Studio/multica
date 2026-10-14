# ADR-001 — Project-scoped Agent Hierarchy

- Status: Accepted
- Date: 2026-10-14
- Decision owners: Human maintainer
- Related issues: #5
- Supersedes: None
- Superseded by: None

## Context

### Confirmed upstream behavior

Current Multica `main` at the investigated baseline `52b26900c51f759b87707b4ea35fa9f141b83b52` has first-class Projects and workspace-scoped Agents.

`server/migrations/034_projects.up.sql` creates `project` with `workspace_id`, optional `lead_type` (`member` or `agent`) and `lead_id`. The same migration adds nullable `issue.project_id` with `ON DELETE SET NULL`.

`server/pkg/db/queries/agent.sql` consistently resolves user Agents within a Workspace and distinguishes normal user Agents from hidden system execution carriers.

Current Multica does not provide a Project-scoped reporting tree for Agents.

Paperclip is a useful conceptual reference but uses a different scope: its current `packages/db/src/schema/agents.ts` stores `reportsTo` directly on a company-scoped Agent. Its agent configuration path validates manager existence and cycles. That model demonstrates useful tree semantics, but a global Agent manager field cannot satisfy this fork's requirement.

### Fork requirement

The same Agent must be able to participate in multiple Projects and hold different organizational positions in each Project, including reporting to different managers.

Example:

```text
Project A
Agent X -> reports to Agent Y

Project B
Agent X -> reports to Agent Z
```

The hierarchy therefore belongs to the Project participation, not to the global Agent record.

## Decision Drivers

- Hierarchy is explicitly Project-scoped.
- The same Agent may belong to multiple Projects with different managers/titles.
- One root is required for each configured Project hierarchy.
- Every non-root node has one direct manager.
- Arbitrary depth must be supported.
- Existing Project, Agent, Squad and Issue contracts should remain backward compatible.
- Projects without hierarchy configuration must behave exactly as upstream.
- The design must minimize modifications to central upstream tables and services.

## Considered Alternatives

### Alternative A — Add `manager_id` / `reports_to` to `agent`

**Pros**

- Simple adjacency-list model.
- Similar to Paperclip's company-scoped hierarchy.
- Easy manager lookup.

**Cons**

- Incorrect scope for the requirement.
- One Agent could not report to different managers in different Projects.
- Couples Project organization to the global Agent lifecycle.
- Would create a persistent fork change in a central upstream table.

**Decision:** rejected.

### Alternative B — Reuse `squad` / `squad_member`

**Pros**

- Existing leader/member concepts.
- Existing routing and briefing behavior.

**Cons**

- Squads are workspace-scoped, not Project-scoped.
- `squad_member.role` is roster context, not hierarchy or routing authority.
- Squad work routes to `leader_id`; members are not an operational reporting tree.
- A Squad may reasonably participate in multiple Projects with different Project organization.
- Reusing Squad would conflate coordination/routing with Project organization.

**Decision:** rejected. See ADR-002.

### Alternative C — New Project-scoped membership adjacency list

**Pros**

- Correctly scopes manager relationships to Project participation.
- Supports arbitrary depth and one direct manager.
- Additive schema; no global Agent contract change.
- Natural recursive traversal for manager/direct-report chains.
- Compatible with existing Issue and Squad assignment semantics.

**Cons**

- Requires new integrity logic for roots, cycles and concurrent reparenting.
- Adds another domain object and API surface.

**Decision:** accepted.

### Alternative D — Closure table as the primary hierarchy model

**Pros**

- Fast ancestor/descendant queries.
- Cycle checks can be efficient.

**Cons**

- More write complexity and derived rows.
- Reparenting becomes substantially more complex.
- Larger migration and maintenance footprint for an initial feature.
- More fork-specific data logic to reconcile with upstream.

**Decision:** rejected for v1. It may be reconsidered only if measured hierarchy-query performance justifies it.

## Decision

Introduce a new, optional Project-scoped Agent membership domain represented initially by an adjacency list.

The conceptual table is **proposed**, not an existing upstream table:

```text
project_agent_membership
------------------------
id UUID PK
project_id UUID NOT NULL
agent_id UUID NOT NULL
manager_membership_id UUID NULL

title TEXT
operational_role TEXT
sort_order ... NULL
archived_at TIMESTAMPTZ NULL

created_at TIMESTAMPTZ NOT NULL
updated_at TIMESTAMPTZ NOT NULL
```

Core semantics:

1. A membership represents an Agent's organizational participation in one Project.
2. An Agent may have at most one active membership in a given Project.
3. `manager_membership_id` references another membership in the same Project.
4. A configured active hierarchy has exactly one active root (`manager_membership_id IS NULL`).
5. Every other active membership has exactly one active direct manager.
6. Hierarchy depth is derived from the adjacency list and is not stored as authoritative state.
7. Only normal user Agents are eligible hierarchy members; hidden/system execution carriers are not organizational nodes.
8. The existing Project `lead_type` / `lead_id` relationship remains independent. A Project lead is not automatically the hierarchy root.
9. Hierarchy is not a new `issue.assignee_type`.
10. An Issue without `project_id` has no hierarchy semantics.
11. A Project without hierarchy memberships retains upstream behavior.

The operational root is an Agent membership. Human Project leadership remains a separate existing Project concept and becomes the boundary for root-level escalation rather than an implicit hierarchy node.

## Consequences

### Positive

- Correct Project scope.
- Same Agent can have different managers/titles across Projects.
- Existing Agent table does not gain fork-specific organization fields.
- Existing Project lead semantics remain intact.
- Existing assignment/routing contracts remain available.
- Schema can be introduced additively without backfilling existing installations.

### Negative / Trade-offs

- Project hierarchy becomes a new domain that must be kept consistent with Agent lifecycle.
- Recursive queries are required for ancestor/descendant traversal.
- Structural mutations need explicit concurrency control.
- UI/CLI/API need new Project-hierarchy surfaces.

## Data Impact

Expected additive data change:

- new `project_agent_membership` table;
- FK to `project`;
- FK to `agent`;
- same-Project self-reference for manager membership;
- uniqueness and root indexes described in ADR-003.

No existing Project, Agent, Squad or Issue row requires backfill.

Rollback of the initial schema is possible by removing the new hierarchy tables only when no fork feature still depends on them.

Exact migration number and final column names must be selected from the current upstream migration sequence at implementation time.

## Backend / Runtime Impact

Hierarchy behavior should be isolated behind a Project-hierarchy service/module. Existing central handlers/services should receive only small integration hooks where required.

Runtime hierarchy context is resolved from:

```text
Issue.project_id
+ executing Agent
+ active Project membership
```

The hierarchy does not replace the current task assignee or runtime target.

## API / CLI / UI Impact

Additive Project-hierarchy subresources/commands are preferred over required changes to existing Project/Agent payloads.

Conceptual API shapes such as `/projects/:id/hierarchy` are proposals only and must follow the router conventions observed at implementation time.

Existing clients that know nothing about hierarchy must continue to function.

## Failure and Edge Cases

- Agent belongs to several Projects: resolve membership only through the current `project_id`.
- Issue has no Project: hierarchy is not used.
- Project has no configured hierarchy: upstream behavior.
- Project lead is a human Member: this does not create a hierarchy node.
- Project lead is an Agent: this still does not automatically make that Agent the root.
- Agent is absent from the Project hierarchy: normal upstream execution may continue, but hierarchy-only actions are unavailable.
- Hidden/system Agent: cannot be inserted as an organizational membership.
- Cross-Project manager reference: invalid; ADR-003 defines enforcement.
- Archive/removal behavior: ADR-006.

## Testing Requirements

- same Agent may hold memberships in multiple Projects;
- different managers for the same Agent across Projects;
- Project without hierarchy preserves upstream behavior;
- Issue without Project preserves upstream behavior;
- Project lead remains independent from hierarchy root;
- system Agents cannot become hierarchy members;
- direct Agent and Squad-assigned Issue regressions remain unchanged.

## Upstream Compatibility

This decision intentionally avoids adding `manager_id` to the central `agent` table and avoids repurposing Squad.

Primary strategy:

- additive table;
- isolated service/module;
- optional behavior;
- no existing payload field becomes required;
- no existing assignee type changes.

If upstream later introduces a native Project membership/hierarchy model, the fork should migrate toward that model and retire redundant fork structures where the upstream invariants satisfy these requirements.

## Open Questions

- Final table/module/API names should be chosen only after re-inspecting current upstream conventions at implementation time.
- Whether `title` and `operational_role` should both exist in v1 or one should be deferred until a concrete UI/runtime use is proven.
