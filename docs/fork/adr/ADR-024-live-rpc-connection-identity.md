# ADR-024 — Server-owned live RPC connection identity

- Status: Accepted
- Date: 2026-10-19
- Decision owners: Repository agent under standing implementation authority
- Related issues: #37, #55
- Extends: ADR-023

## Decision

Give each authenticated daemon WebSocket a server-generated random UUID before
upgrade. Keep it private on the hub client, outside `ClientIdentity`, headers,
RPC envelopes and daemon reports. This identifies a transport connection, not a
provider conversation/session, account, daemon process or runtime generation.

Expose `daemonws.RuntimeConnectionID(ctx, runtimeID)` only through the private
context value installed by the hub's actual RPC dispatch. A caller supplying a
copied `ClientIdentity`, a body/header UUID, an HTTP request or a background
context cannot mint this identity. Concurrent sockets and reconnections get
different UUIDs even when their daemon/user/runtime metadata is identical.

On every lookup, require a live RPC/connection context, a registered client and
the runtime still present in both the hub index and that client's mutable scope.
Use the existing hub-to-runtime lock order shared by registration, invalidation
and disconnect; never call handlers or access the database while holding it.
Runtime removal revokes only that runtime's connection scope. Disconnect revokes
the whole socket even if a sibling socket still watches the same runtime. An
expired/canceled RPC cannot obtain an identity from a still-live connection.

This is a point-in-time local transport observation. The UUID alone is not an
execution lease or authorization to claim/run work; disconnect/removal can occur
immediately after any snapshot. Future evidence handlers must revalidate the
actual current connection at consumption and retain existing DB workspace,
runtime ownership, target epoch and claim/transaction fences. The hub does not
select an arbitrary "current socket" by runtime ID or persist liveness across
server nodes/restarts.

## Compatibility and remaining boundaries

Generic RPC callback signatures, envelopes, HTTP fallback and ordinary task
execution remain unchanged. No new public API/wire fields, database schema,
daemon capability advertisements or provider CLI/account discovery. No fallback
adapter is approved and no automatic route/probe is enabled by this change.

This supplies the connection-identity prerequisite of ADR-023. It does not
implement credential/default-model/configuration epoch observation, durable
challenge issuance/consumption, evidence message transport, adapter guarantees,
writer/claim integration or real minimal inference. Those remain explicit gates.

## Verification

Offline real-WebSocket tests verify identity stability within a connection,
distinct concurrent/reconnected identities, cross-runtime and non-RPC rejection,
disconnect despite a live sibling, runtime-specific invalidation and expired
request rejection while preserving the socket. Concurrent readers compete with
runtime invalidation under the race detector. Existing daemonws/handler/server
tests retain the generic RPC and lifecycle contracts. No real agent is executed.
