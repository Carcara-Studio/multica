# ADR-029 — Codex thread selection acknowledgement

- Status: Accepted
- Date: 2026-10-20
- Decision owners: Repository agent under standing implementation authority
- Related issues: #37, #65
- Extends: ADR-023, ADR-026, ADR-028
- Multica upstream baseline: `47120a4131564175da044d63d897ed9b5d76979a`

## Decision

Observe the model/provider acknowledged by actual Codex thread/start or
thread/resume responses, rather than infer them from requested options, saved
thread history or config/read. A private opt-in observer binds one initialized
client and fixed absolute cwd. No production constructor installs an observer.
The ordinary executor sends the same requests and returns the same results,
errors and resume-to-start fallback behavior, even when observation is rejected.

Each observed response must belong to the client's real pending setup request.
Require root model/provider and cwd, thread ID, fixed request cwd and an exact
resumed thread match. Explicit requested model/provider must match; unresolved
aliases conservatively reject observation, without changing execution. Ignore
nested historical model/provider fields. Parse with ADR-028's existing bounded,
duplicate-key-rejecting, exact-number parser; retain no raw thread context.
Successful acknowledgements receive random opaque UUIDs, not credential hashes.
All observation fields, formatting, JSON and structured diagnostics are private.

Record from the ordered response dispatcher before waking the request goroutine.
A delayed request goroutine must never republish an acknowledgement invalidated
by later protocol traffic. Snapshots remain unavailable until their request has
finished successfully and its context/client remain valid. Reject overlapping
RPCs; they cannot certify a single setup boundary. An atomic count covers all
RPC lifetimes, including clients without an observer, so attachment cannot hide
a request whose reply was removed from the pending map but not yet returned. Late/unknown replies cannot
create observations. Process death, cancellation, malformed frames, later
requests/writes or inbound traffic after recording invalidate the receipt.
Inbound notifications preceding a setup response do not establish authority;
only the subsequent matched response may acknowledge its selection.

One read-only exception permits config/read with includeLayers and the same
explicit cwd when no other RPC is active. A successful scoped read preserves the
thread acknowledgement, so ADR-028 observations can be refreshed independently.
RPC or configuration-validation failure invalidates both observations. Reading
configuration does not itself establish thread selection or credential identity.

Default clients keep a nil atomic observer pointer. The small dispatcher/request
hooks then perform no observation parsing, UUID generation, extra RPC, account
access or inference. No new transport or executor framework is introduced.

## Limits and credential findings

A setup acknowledgement is not proof of which model served a later inference,
a current-provider-state lease, credential identity, tool/session isolation or
health. turn/start, server requests and subsequent notifications can invalidate
it. A response may be unusable immediately because newer traffic arrived; this
is intentionally conservative. Do not turn snapshot UUIDs into evidence claims
without independent target/server scope and execution-boundary revalidation.

The official account/read response exposes API-key versus ChatGPT account
metadata, including email/plan, but no opaque generation for effective credential
selection. The new observer does not call that API or inspect credential files,
keychains or environment secrets. No credential epoch is produced. Effective
credential observation remains unresolved; account metadata must not be promoted
into credential authority. The inherited blocking stdin Write/post-receipt parser
limit from ADR-028 also remains an execution-adapter gate.

No production adapter is approved, no EvidenceReport/ExecutionIdentity is created,
and no public optional-policy route, automatic fallback or primary probe is
activated. SQL, existing API schemas and task/lease authority are unchanged.

## Source evidence and verification

Official Codex source inspected at commit
`464add49587a7cb6032d5ab04faa8a1585947853`:

- https://github.com/openai/codex/blob/464add49587a7cb6032d5ab04faa8a1585947853/codex-rs/app-server-protocol/src/protocol/v2/thread.rs
- https://github.com/openai/codex/blob/464add49587a7cb6032d5ab04faa8a1585947853/codex-rs/app-server-protocol/schema/json/v2/GetAccountResponse.json

Tests exercise the existing request/response dispatcher and start/resume wrapper
with fake stdin/responses only. Cover root-versus-history selection, client/cwd/
resume scope, explicit mismatch, malformed/large responses, copied-value redaction,
ordered notifications before/after acknowledgement, cancellation/death, concurrent
RPCs, late replies, unchanged legacy minimal responses, resume fallback and
composition with layered configuration. No installed CLI, real account or
provider inference is accessed. Exact-head agent/race, PostgreSQL backend and
Windows compatibility CI are required; fake responses do not certify live Codex.
