# Project hierarchy context in direct Issue claims

- Issue: #73
- Authority: ADR-001, ADR-003, ADR-004, ADR-014
- Baseline: fork `54533b02c912b31b40360aad5def01fb213855f9`;
  upstream `47120a4131564175da044d63d897ed9b5d76979a`.

A direct Issue task whose Agent has an active membership in the Issue's Project
receives an informational snapshot in its existing Agent instructions. It includes
only its membership/Agent identifiers and title, its direct manager and its direct
reports. The Project identity is explicit; the same Agent's role in another
Project is not reused. This changes no queue or dispatch target.

The shared claim builder inserts this after existing Issue workspace/Project
checks. Claims without a Project, nonmembers, Squad-associated tasks and
Squad-assigned Issues retain their existing behavior. Chat/autopilot/quick-create
paths are outside this initial slice. No wire fields, capability flags, migrations
or public endpoints are added.

The domain projection reuses active-tree validation. Foreign, duplicate, cyclic,
orphaned and oversized input is omitted as a whole. Input is capped at 1024 rows,
32 direct reports and 256 UTF-8 bytes per included title. Output is capped at
16 KiB after JSON escaping. These are admission/render limits, not a SQL LIMIT:
the existing scoped query remains unchanged. The optional read has a shared
500 ms context budget; query/Agent lookup errors, cancellation or archived/missing
relevant Agents omit the snapshot rather than fail normal task delivery.

Titles are JSON-escaped untrusted data, not a new prompt instruction surface.
The block explicitly grants no invocation or delegation authority. It is a
point-in-time informational snapshot, not a lease; dispatch-time hierarchy
authorization for future delegation must be independently implemented under
ADR-004. No child Issues, completion wakeups, escalation or Agent lifecycle
mutation policies are enabled by this increment. Squad leader flags/protocols,
normal assignment and task runtime ownership remain authoritative.

Domain tests cover one-hop projection, deterministic ordering, Project/workspace
scope, archived/missing memberships and malformed/excessive data. Handler tests
cover bounded escaped rendering and real direct claim routing/instructions,
neutral no-Project/nonmember/archived-Agent cases, cancelled optional reads,
per-Project roles and unchanged Squad protocols. Existing service/concurrency and
Squad suites remain compatibility gates.

The local execution environment was unavailable during implementation. Source
review and exact-head CI provide the compile/test evidence; no local test result
or installed-agent/account/inference execution is claimed. Automatic fallback
and primary recovery probes remain disabled.
