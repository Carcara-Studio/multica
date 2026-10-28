# Native quota callback transport

R4 / #37 / #133 / ADR-038. This increment transports a diagnostic; it does not
approve an execution target, change availability or enable recovery probes.

## Daemon and durable outbox

The native Codex adapter binds usageLimitExceeded to its current failed thread/
turn. The daemon copies that observation before a missing rollout can withhold
the resume pointer. The source event is separate from the resumable session:
transporting it must not restore a deliberately withheld pointer.

Only failed/blocked task results with a supported native shape and matching
session (or explicitly withheld pointer) forward the optional `native_quota`.
Success, cancellation, foreign sessions and unknown shapes do not. The existing
terminal outbox stores the observation by value so restart/replay and conflict
checks preserve its source without pointer-identity comparisons. It stores no
auth token. Version1 records without the field remain readable; older binaries
may ignore/drop the new diagnostic on downgrade and cannot gain authority from it.

## Server boundary

The existing failure endpoint accepts optional raw JSON so malformed/unknown
diagnostic shapes cannot reject an otherwise valid terminal failure. Parsing is
bounded and supported identifiers are restricted. Runtime, task and workspace
facts come from server-loaded rows, not fields supplied beside the observation.

For this HTTP increment, correlation requires an existing authenticated daemon
token context whose daemon/workspace match the running task's actual Codex runtime.
PAT/JWT/cloud user credentials alone do not identify the executing machine;
their optional observation is inconclusive while ordinary failure handling stays
compatible. No broker credential is repurposed or stored, and no WS/auth protocol
is added. Any future WS lane must use the real RuntimeConnectionScope.

A preflight only carries optional source facts into the terminal transaction.
The existing failure UPDATE rechecks running status, the actual selected runtime,
Codex provider, daemon and Agent/runtime workspace in its statement snapshot
before narrowing the stored reason to quota. A stale source retains the ordinary
reason, but a supported executor-correlated preflight still conservatively vetoes
replay: losing current-path proof cannot authorize repeating possible partial
work. Uncorrelated or malformed metadata retains ordinary retry handling.
Subsequent session and chat decisions use the stored reason.
The retry overlay is still prepared before the transaction, without network I/O
under terminal locks. This adds no new lock order or availability transition.

A bounded source diagnostic is logged only after the first successful
terminal transition. Duplicate terminal callbacks cannot publish a new source
event. The original error text is preserved.

The SQL snapshot does not lock runtime/owner/configuration for future execution.
This transport snapshot is not current owner/configuration/model authorization,
provider-side cryptographic proof or durable accepted routing evidence. Source
lookup failure discards the optional diagnostic rather than blocking ordinary
settlement. Logs and syntax checks must never become automatic-routing authority.

## Activation still blocked

Future availability settlement must validate current opt-in route proof, Agent/
owner/runtime/profile, model/options and generations under the correct owner/
configuration/state lock order, composing terminal failure and accepted native
evidence atomically. A task failure reason alone is insufficient. Strict evidence
helpers remain closed; no credential epochs are fabricated here.

ADR-018 excludes session IDs from task route rows. This increment adds no DB
storage or migration; do not put the source session into that row as a shortcut.
All writers/claims/retries, compatible fallback selection and isolated real
primary probes remain required before automatic mode can activate.
