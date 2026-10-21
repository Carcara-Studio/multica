# ADR-034 — Escalation transport and management surfaces

- Status: Accepted
- Date: 2026-10-21
- Scope: DELIVERY_CHECKLIST R2; #100
- Governing decisions: ADR-005/012/032/033

POST /api/issues/{id}/hierarchy-escalations accepts only request_id, reason and
optional context. Only the authenticated running task MAT may create or replay;
workspace, Project, source Agent/task and direct-manager/root route are server
resolved. Compose ADR-033's native admission without alternative create/assign
or mention fallback. A strict bounded DTO rejects unknown fields. New requests
return 201; authorized replay returns 200 with the original Issue identity.
Conflicts/tombstones are 409; admission denial is generic 403. Infrastructure
failures remain errors. No automatic HTTP/CLI retry is added.

The CLI requires a persisted explicit UUID and task credential. Only the bounded
direct-Issue Project briefing advertises it. The root creates human intervention;
unavailable managers are never bypassed. Existing Agent Identity remains binding.

GET on the same route uses ordinary workspace/Issue read authorization, accepts
an optional decision-Issue UUID cursor, and returns at most 50 receipts involving
this source or decision Issue. Records expose frozen source/route, reason/context,
native current decision status, deleted decisions and repair diagnostics. Native
queue presence is diagnostic, never proof of exactly-once delivery. Read errors
must not be converted to an empty history. No migration or secondary state
machine is needed. This is a live read snapshot, not execution admission.

Shared web/desktop Issue management shows this history and links both Issues.
Humans manage the native decision Issue through existing controls; there is no
button impersonating a running Agent. Explicit refresh and bounded pagination
avoid silent stale history. Unknown decision statuses/repair codes stay visible.
R3 owns dispatch reconciliation; this UI must never blindly replay work. R5 owns
installed CLI/account smoke and isolated upstream synchronization.
