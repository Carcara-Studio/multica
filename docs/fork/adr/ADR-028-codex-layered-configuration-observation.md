# ADR-028 — Codex layered configuration observation

- Status: Accepted
- Date: 2026-10-20
- Decision owners: Repository agent under standing implementation authority
- Related issues: #37, #63
- Extends: ADR-023, ADR-026
- Multica upstream baseline: `47120a4131564175da044d63d897ed9b5d76979a`

## Decision

Add a private, opt-in observer beside the existing Codex executor. Each instance
belongs to one actual `codexClient` and one fixed absolute directory. It requests
only `config/read` with `includeLayers: true` and explicit `cwd`. No existing
execution path calls it. It neither starts a process nor reads an account,
filesystem configuration, repository, CLI executable or inference result itself.
The provider may read its own configuration sources when handling this RPC.
Never call it while server owner/state locks are held; eventual server sources
must consume independently refreshed bounded snapshots as required by ADR-026.

Require configuration and origins objects plus a layers array with versioned
source metadata/configuration objects. Retain unknown response fields for
conservative comparison, but retain only a private SHA-256 comparison digest
between observations. The digest never leaves the observer. Accepted changes
issue a random opaque UUID rather than exposing a configuration/credential hash.
Configured model/provider selections remain optional bounded strings; missing or
null values remain unknown. Instances do not share epochs across clients or cwds.
After any read/validation/process/deadline failure, discard the current snapshot;
a later successful read receives a fresh epoch even if values were restored.

Parsing rejects duplicate keys, invalid UTF-8, trailing JSON, excessive nesting,
node counts and responses over 256 KiB. Preserve exact JSON numbers rather than
rounding through float64. Object key ordering is irrelevant; array order and
unknown changes conservatively invalidate. No raw response, source paths,
provider error or comparison digest is emitted in errors, JSON or diagnostics.
The observed result is a private type, not an ExecutionIdentity or evidence seal.

## Why this cannot approve an adapter

The official Codex `config/read` implementation loads cwd-scoped layers and
applies exact managed requirements. It does not run the complete profile/thread/
turn selection path. Its `config.model` is a configured selection, not proof of
the effective model selected by `thread/start` or `thread/resume`. It also does
not establish which credential source/token a request would use. Keychain,
environment, linked auth files, provider-specific settings and refresh behavior
still require independent credential observation. The account description is
not a substitute. This observer produces **no credential epoch**.

The two-second context bounds response waiting in the existing RPC implementation;
its stdin Write is inherited and can still block. Parsing limits apply after
transport receipt; the upstream stream scanner keeps its existing limit. An
approved execution adapter additionally needs bounded cancellable transport and
process lifecycle ownership. Snapshot success is point-in-time, not a lease;
changes after return still need an execution-boundary recheck.

No public method/endpoint, capability claim, production adapter approval, default
executor change, delegated daemon authorization, SQL/schema, task routing or
probe scheduling is introduced. Future integration must combine this observation
with credential and effective thread-model evidence, target/server binding,
reviewed task/probe guarantees and the durable challenge transaction. Automatic
fallback and primary probes remain disabled.

## Evidence and verification

Protocol and implementation inspected at official OpenAI Codex commit
`464add49587a7cb6032d5ab04faa8a1585947853`:

- https://github.com/openai/codex/blob/464add49587a7cb6032d5ab04faa8a1585947853/codex-rs/app-server-protocol/src/protocol/v2/config.rs
- https://github.com/openai/codex/blob/464add49587a7cb6032d5ab04faa8a1585947853/codex-rs/app-server/src/config_manager_service.rs

Tests reuse the real Multica request/response dispatcher with fake stdin and
protocol responses, without executing installed agents or accessing credentials.
Cover method/cwd isolation, stable/change/restoration epochs, unknown fields and
large exact numbers, malformed schemas/JSON, size/depth/count limits, private
serialization/diagnostics, RPC failure, cancellation, process death after a
buffered response and pending-request cleanup. Existing agent/race and backend
CI remain required; fake protocol tests do not certify live Codex compatibility
or real credentials/inference.
