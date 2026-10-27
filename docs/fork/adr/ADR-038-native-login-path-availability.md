# ADR-038 — Native-login execution-path availability

- Status: Accepted
- Date: 2026-10-27
- Decision owner: Human maintainer (explicit chat approval of the limited R4 contract)
- Scope: R4 / #37
- Partially supersedes: ADR-023/025/026 credential-observation requirements for this opt-in mode only
- Preserves: ADR-007/010/011/012/013/018 target, probe, concurrency, no-replay and compatibility rules

## Decision and accepted limitation

For the limited native-login mode, availability describes the configured execution
path of one Agent, not a particular account/token's billing or credential health.
The CLI retains login/refresh ownership. The maintainer explicitly accepted that
an externally changed login need not immediately invalidate availability: fallback
can remain active until a later successful primary probe. Thirty minutes is the
probe cadence, not a recovery deadline when offline or inconclusive.

Do not synthesize credential epochs, populate the existing strict adapter map with
metadata-only reports, or present this mode as strict effective-account evidence.
Existing strict evidence helpers remain closed; the path mode needs distinct,
reviewed authority and end-to-end integration before activation. It cannot grant
runtime/workspace/owner permission by declaring a path unavailable.

A path binds Agent/workspace/owner, permitted runtime/profile, current primary and
fallback configuration generations, explicit supported model/options, and native
run/turn identity. Known config/authorization changes invalidate stale reports;
unknown external login changes have the accepted limitation above. No availability
is shared across Agents on the assumption they use the same account. Default or
unknown model/provider selection is ineligible unless an approved adapter can
observe the effective selection without guessing.

## Quota and execution safety

Only an unambiguous native terminal usage-exhaustion signal bound to the actual
run/turn can activate future-work routing. Assistant/tool text, raw diagnostic
strings, exit status, HTTP status alone, temporary rate limits, authentication,
network and capacity failures are not that signal. Preserve typed provenance
separately from display errors. A native quota observation alone is neither a
routing capability nor proof that no side effects occurred.

The quota-failed run remains terminal. Do not replay it, create a substitute run,
clone the Agent or migrate a running task. Later user-requested continuation must
acknowledge possible partial work. Existing queued work keeps its persisted route;
only new enqueue selects the new route. Owner/config/state/claim fences and
transactional task+route persistence remain mandatory at every writer/retry/claim.
Shared Agent instructions, permissions, durable context and required tools stay
intact; unsupported compatibility blocks activation rather than silently dropping
capabilities. Provider sessions do not transfer between targets.

## Recovery

Keep the requested 30-minute minimal real primary inference, separate from normal
tasks/history, with no tools/MCP/hooks/repository/user context or resumed session.
Adapters lacking a verifiable isolated probe remain ineligible for automatic
recovery. Prompt instructions such as 'do not use tools' do not isolate a probe.
Only native inference success restores availability; heartbeat/catalog/exit0 do
not. Keep durable tokenized leases, duplicate/late result rejection and crash
settlement. A probe describes the then-current logged-in path, not the original
account. Never acquire/store/renew CLI tokens as a shortcut.

## Delivery gate

First preserve a bound, typed native Codex usage-limit observation in the adapter
result, without changing retries, failure text, dispatch or transport authority.
Pin the source shape and test misleading text, retry notifications, wrong/missing
IDs, late/conflicting events, successful/aborted outcomes and result propagation.
OpenCode diagnostics alone remain unapproved; its native signal semantics need
separate review. Subsequent slices must integrate trusted report transport,
configuration/route resolution, every writer/claim/retry, quota settlement and
isolated primary probes before enabling this mode. No partial slice closes R4.
No new auth service, generic executor or account-health subsystem is introduced.

Protocol source inspected: OpenAI Codex commit
`b412ff32c417f855c2b2d1581b77058eed87c84b`,
`codex-rs/app-server-protocol/src/protocol/v2/{shared,notification,thread_data}.rs`.
`CodexErrorInfo::UsageLimitExceeded` is distinct from `RateLimitExceeded`;
`TurnError.codex_error_info` and native thread/turn IDs provide the typed source.
