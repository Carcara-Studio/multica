# ADR-036 — Native OpenCode 2 completion proof

- Status: Accepted
- Date: 2026-10-22
- Scope: DELIVERY_CHECKLIST R5; #116/#118, follow-up to #114
- Governing decisions: ADR-012/014/021; native task lifecycle remains unchanged

## Evidence

The maintainer's post-#115 Windows identity test produced correctly attributed
comments and In Review changes, followed by three failed attempts per monitor.
Every supplied failure says the final step remained open at EOF. That is a
Multica protocol diagnosis, not direct evidence of a provider network outage.

OpenCode v2.0.28, commit `026b106bc5d41b679ffc413264229c340871e4d3`,
`packages/cli/src/run/noninteractive.ts` awaits the native session wait,
sets `finalizing`, reconciles persisted text/tools and aborts its event consumer.
After finalizing, that consumer discards non-execution events, including
`session.step.ended`; reconciliation does not emit `step_finish`. This can lose
the terminal JSON event despite successful persistence. Source compatibility
does not establish the exact missing-event trace on the maintainer's machine.

Native session export is read-only. Its transfer schema contains session info
and ordered persisted messages. Assistant completion and final `idle` outcome
provide stronger completion evidence than displayed text or Issue status.

The later DEV-13 log reports a closed step requiring continuation at EOF.
That shape previously never consulted this proof. It is eligible for the same
verification, not an exemption from its predicate. The local raw event trace
and native transcript are unavailable; model availability is not established
as the cause.

V2 can reconcile a new assistant's text after the earlier tool step without a
new step_start. Select the latest assistant ID actually observed on text/tool/
step events, rather than retaining the earlier step-start ID. Observing an ID
only selects the record to verify; it supplies no completion evidence itself.

Compatibility review on 2026-10-23 found upstream `76dfd4a58` (#9463), which
accepts clean built-in V2 process exit for both missing-step shapes while
retaining error/exit/void-step checks. That differs from this ADR's exact
persisted-message requirement. #118 only broadens read-only query eligibility;
it preserves that stronger predicate instead of silently replacing the accepted
completion contract. The reviewed upstream HEAD is
`dbdbb299164347586e2650bb67684a490726a798`; it is not an integrated baseline.

## Decision

Only a built-in OpenCode 2 run that exits zero, finishes its prompt write,
has no cancellation, scanner or explicit runtime error, and leaves an open
step or unresolved continuation gap with a known assistant message ID may consult native
`session export --sanitize <session-id>`. Use the same executable, environment,
directory and native private/server selection. Bound it by the original run
context plus ten seconds and eight MiB; use owned process-tree cleanup. Discard
stderr; never persist, log, emit or replay the transcript. Log only static
verification status and session/message IDs.

Never resolve the default managed service for verification: its startup installs
restart continuity that resumes suspended sessions. Only a private server or
an already explicit server connection is eligible. OpenCode's
`packages/server/src/process.ts` installs that hook only when its managed
`lifecycle` argument exists; native private `serve --stdio` supplies no lifecycle.

Accept completion only when the export names the same session and ends with
the exact streamed assistant followed by native `idle: succeeded`. Require
assistant `finish: stop`, completed timestamps in order, no assistant/idle
error, no unfinished tools, and a preceding user prompt in that turn. Reject
stale/mismatched IDs, later prompts, missing/failed/interrupted idle, malformed,
oversized, unavailable or timed-out evidence. Without proof the existing
failure remains. A persisted assistant still requiring tool continuation cannot
pass, even if it has produced text or changed an Issue. Already terminal steps,
explicit errors, cancellation, failed process
exit, old/unknown versions and custom runtime contracts keep existing behavior.

This is completion verification, not inference, task retry, provider failover
or reconstruction of side effects. No model request is made. Existing native
retry policy, hierarchy idempotency and task lifecycle are unchanged. It does
not address Codex UAC setup or prove branched hierarchy completion. Missing
final-step usage is not reconstructed by this completion-only change.

## Compatibility and validation

Keep the change in an isolated runtime module with a small hook in the existing
OpenCode adapter. No API/schema/migration or alternate executor. Fake native
executables cover Windows and Linux completion, failed exit, explicit error,
cancel, bounded export and old-runtime refusal. Pure fixtures cover invalid
persisted evidence. The open-step/continuation guards remain regression gates.
Actual installed-account behavior must be validated separately by the maintainer.

Official source references:

- https://github.com/anomalyco/opencode/blob/026b106bc5d41b679ffc413264229c340871e4d3/packages/cli/src/run/noninteractive.ts
- https://github.com/anomalyco/opencode/blob/026b106bc5d41b679ffc413264229c340871e4d3/packages/cli/src/commands/handlers/session/export.ts
- https://github.com/anomalyco/opencode/blob/026b106bc5d41b679ffc413264229c340871e4d3/packages/schema/src/session-message.ts
- https://github.com/anomalyco/opencode/blob/026b106bc5d41b679ffc413264229c340871e4d3/packages/core/src/session/transfer.ts
