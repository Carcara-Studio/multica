# ADR-030 — Cancellable owned Codex stdin writes

- Status: Accepted
- Date: 2026-10-20
- Decision owners: Repository agent under standing implementation authority
- Related issues: #37, #67
- Extends: ADR-028, ADR-029
- Multica upstream baseline: `47120a4131564175da044d63d897ed9b5d76979a`

## Problem and decision

RPC response waiting already respects context/handshake deadlines, but the
preceding stdin Write could block indefinitely when an app-server stopped
reading, especially for a large turn prompt. The optional configuration observer
therefore could not claim a bounded call merely by wrapping its request context.

Wrap the actual os.File pipe returned by the existing exec.Cmd.StdinPipe. Reuse
the existing process cancellation/tree ownership and lifecycle/reaping; do not
spawn a second process or a background writer goroutine. A context-aware gate
serializes complete frames with their deadline callbacks. RPC writes inherit the
request context, including its handshake cause. All writes also have a fixed
30-second ceiling, including notifications and server-request replies without a
caller context. Caller deadlines may shorten that ceiling; it is not a new
public runtime/profile setting.

Probe pipe deadline support before sending bytes. context.AfterFunc expires the
write deadline when cancellation fires. Join any callback already started before
clearing the deadline/releasing the gate, so a late cancellation cannot poison a
subsequent frame. Cancellation while waiting for the gate sends no bytes and does
not abort another caller's frame. Pre-cancelled calls similarly preserve a
healthy transport. A complete write that races cancellation returns the context
error; later RPC lifecycle/cleanup remains authoritative.

An attempted write that fails, short-writes or cannot reset its deadline closes
the owned pipe and cancels its process exactly once. Even a zero-byte I/O failure
can indicate an unusable peer; never append another JSON frame to that stream.
Mark the real client exited to release pending requests and revoke observation
liveness. Never fall back to unbounded I/O if a real pipe rejects deadlines. Keep
errors static and preserve parent cancellation versus handshake-timeout
classification, including the initialize-retry safety rules. Graceful EOF and
existing reap/descendant cleanup remain in the existing executor.

Only production process stdin receives this wrapper. Existing in-memory test
writers retain their old behavior and cannot certify production cancellation.
No new CLI command, public API, database field or independent transport framework
is introduced. The default executor now gains a bounded write phase; normal
wire bytes, options, resume behavior and response parsing remain unchanged.

## Platform evidence and verification

The repository uses Go 1.26.9. Its os.File documentation permits deadlines on
pollable pipes. Its Windows os.Pipe implementation creates nonblocking pipe
handles, and the poll FD closes/evicts pending pipe I/O. The implementation follows
the standard context.AfterFunc connection example's callback-stop/join/reset
pattern. Inspected local runtime sources correspond to these official references:

- https://github.com/golang/go/blob/go1.26.9/src/context/example_test.go
- https://github.com/golang/go/blob/go1.26.9/src/os/file.go
- https://github.com/golang/go/blob/go1.26.9/src/os/file_windows.go
- https://github.com/golang/go/blob/go1.26.9/src/internal/poll/fd_windows.go

Tests use actual OS pipes and fake owner-abort hooks: blocked write cancellation
and deadlines, fixed background-write ceiling, queued cancellation without
cross-aborting, concurrent frame integrity, stopped-callback deadline reset,
unsupported file destinations, pre-cancelled healthy reuse, pending-request
cleanup, observation invalidation and parent-versus-handshake causes. The Windows
launcher/stdin CI step explicitly includes these portable tests. Existing guarded
Codex/agent race and process cleanup suites remain required; no installed agent,
real account or inference is used by the new tests.

## Remaining credential and execution boundary

This closes the inherited unbounded write phase for actual owned stdin, not the
entire adapter review. Response parsing still uses the upstream scanner and
post-receipt bounds; a task/probe needs its own reviewed execution/cleanup and
freshness checks. Production evidence approvals remain empty, automatic fallback
and primary probes stay disabled, and no credential epoch is produced.

Credential-source investigation at official Codex commit
`464add49587a7cb6032d5ab04faa8a1585947853` confirms that AuthManager has an in-memory
auth cache and guarded reload/refresh logic comparing effective auth material.
A filesystem snapshot or account description alone therefore cannot establish
the auth value currently selected inside a running app-server. No user file,
environment credential, keychain or account was inspected in this investigation.
Further work must bind credential authority to actual effective selection rather
than manufacture an epoch from account metadata or a startup UUID.

- https://github.com/openai/codex/blob/464add49587a7cb6032d5ab04faa8a1585947853/codex-rs/login/src/auth/manager.rs
