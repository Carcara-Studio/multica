# Codex effective-credential observation boundary

- Investigation: #69; parent #37
- Date: 2026-10-20
- Fork baseline: `c61b17f50efcc14ab3dcd8556f8ca7060090c511`
- Multica upstream: `47120a4131564175da044d63d897ed9b5d76979a`
- Codex source pin: `464add49587a7cb6032d5ab04faa8a1585947853`
- Authority: ADR-023, ADR-026, ADR-028–030; this is implementation evidence,
  not approval of a new adapter or a proposed upstream protocol.

## Finding

The inspected Codex sources maintain internal authentication revisions, but the
inspected account RPCs and notifications do not expose an effective credential
revision bound to the model request. Multica cannot turn an account description,
a cached-auth revision or a startup identifier into `EffectiveCredentialsKnown`.
The current production evidence approval map must remain empty.

This is a producer-observability dependency. Reading more user credential files
or running account-status commands would not close it. No such access occurred.
The conclusion is scoped to the source pin and inspected interfaces; it does not
assert that every future Codex release lacks a suitable capability.

## Three different revisions

[`AuthChangeState`][change-state] separates these internal counters:

| Revision | Meaning | Limitation for Multica |
| --- | --- | --- |
| `generation` | Cached credentials changed | Local to an auth manager; not an exported model-request receipt |
| `owner_generation` | Login/logout or owner/auth-mode boundary changed | A credential refresh can preserve the owner |
| `source_generation` | Underlying actor credential/source changed | Refresh within an identifiable OAuth session can preserve the source |

[`AuthManager`][manager] compares cached auth when updating these counters.
`auth_change_state_for` additionally checks source continuity for an expected
credential. It does not certify arbitrary provider credentials for a caller.
Counters reset with their manager; reconnect/reset cannot inherit earlier
authority. None of these internal values is currently consumed by Multica.

[`same_source`][source] uses token equality or selected OAuth claims to decide
local continuity. Its own boundary is runtime reuse, with server authentication
performed elsewhere. Multica must not reproduce JWT-claim comparisons or token
hashes as a credential epoch, or treat source continuity as credential equality.

## Public account information is insufficient

The inspected [`account` protocol][account] exposes account descriptions,
`requiresOpenaiAuth`, optional workspace routing, and account-update
`authMode`/`planType`. These payloads have no selected-credential revision tied to
a specific model request. Equal account metadata can therefore coexist with
different tokens. Account-session IDs likewise identify sessions, not every
effective credential selection or refresh.

The [`notification sender`][notifications] checks owner generation internally
before enqueueing account notifications. That prevents stale-owner publication;
it does not export a credential generation or make the notification stream an
exhaustive credential-change log. Absence of a notification grants no authority.

`account/read` can request token refresh. Do not invoke login, logout, account
switching or refresh to manufacture observation evidence. An auth-management
request that exports a token would also add secret handling without proving the
credential selected for the eventual model request.

## Selection must be observed at the request boundary

[`Provider auth resolution`][provider-auth] can select provider API-key or explicit
bearer material before the base auth; custom provider auth can use a separate
manager. Scoped auth resolution also has agent-identity paths. Thus a global
account/auth-manager revision cannot alone cover all configured providers.

In [`current_client_setup`][client], Codex resolves provider/auth/routing together
and checks internal revisions while preparing the request. It can rebuild after
a credential refresh or reject an owner change. This is the relevant integration
boundary: the actual resolved auth bundle, not a separate earlier account read.
The function returns internal setup state, not a public Multica receipt.

ADR-028 configuration observation and ADR-029 thread acknowledgements remain
useful independent facts. ADR-030 bounds transport writes. Combining them still
does not reveal the auth bundle selected inside this request setup.

## Contract required before implementation approval

Any candidate adapter must demonstrate the existing ADR-023 guarantees at its
actual execution boundary. The following are review requirements, not wire fields
or capabilities implemented by this investigation:

1. A trusted producer observes the resolved auth for the exact target, provider,
   model and task/probe request, including provider overrides and refresh paths.
   Unknown auth modes or unobserved credential sources reject admission.
2. An opaque, non-secret credential revision changes on effective credential
   replacement, refresh or loss. Producer-instance and connection scope prevent
   counter reset/reconnect and target cross-use from reviving earlier evidence.
   Neither raw secrets nor secret-derived hashes leave the credential owner.
3. The receipt is bound to the server challenge and independently resolved
   target/generations/live connection under ADR-025/026. A daemon-supplied report,
   account ID or advertised capability cannot approve its own authority.
4. Selection and dispatch are coherently fenced: refresh/account/config changes
   between preflight and dispatch reject or restart preflight. A sampled
   before/after account description does not prove that the same credential was
   used for inference. Dispatch-time selection must match the accepted receipt.
5. The reviewed implementation covers cancellation, cleanup, same-owner token
   rotation, provider-specific auth and reconnects. Credential knowledge remains
   separate from task isolation and successful minimal probe inference.

## Candidate integration paths and disposition

| Path | Disposition |
| --- | --- |
| Existing account/config/thread RPCs only | Insufficient; do not approve an adapter |
| User auth-file snapshots, account commands or token hashes | Insufficient; do not add this secret-access path |
| Codex internal counters inferred from notifications | Insufficient; internal state is not exported request-bound evidence |
| Add an upstream-supported producer receipt at resolved-auth dispatch | Candidate for a separate, versioned upstream/runtime integration and review; not implemented here |
| A separately reviewed executor that owns credential selection and dispatch | Candidate only if it preserves target/options/isolation and actual provider semantics; no new executor, API-key requirement or provider change is authorized by this document |

Prefer a native upstream capability when available. Do not silently replace the
installed Codex binary, require paid API credentials, or weaken ADR-023 to work
around this dependency. A Codex fork would be a separate maintenance/product
choice, not an incidental Multica patch.

## Required rejection and acceptance evidence

| Scenario | Required result from a future reviewed adapter |
| --- | --- |
| Same email/plan, new token | Old credential evidence rejected |
| OAuth refresh preserving owner/source | Credential revision changes; old dispatch evidence rejected |
| Different provider override on the same runtime | No reuse of global account evidence |
| Reconnect or auth-manager counter reset | Earlier instance evidence rejected |
| Change while preparing or dispatching a request | No dispatch under stale authority |
| Missing receipt, unsupported source or fabricated report | Admission rejected; default execution retains existing behavior |
| Correct request-bound credential receipt | Still requires all remaining task/probe guarantees; does not itself mark primary healthy |

These are acceptance requirements, not tests claimed to exist. This increment is
documentation only. No installed agent, user auth file, keychain, environment
credential, account API or inference was accessed. Existing production adapters,
automatic routing and primary recovery probes remain disabled. Continue work on
independent hierarchy/API or routing integration gates while this producer
dependency remains unresolved; do not repeatedly replace it with metadata-only
observers.

[change-state]: https://github.com/openai/codex/blob/464add49587a7cb6032d5ab04faa8a1585947853/codex-rs/login/src/auth/change_state.rs
[manager]: https://github.com/openai/codex/blob/464add49587a7cb6032d5ab04faa8a1585947853/codex-rs/login/src/auth/manager.rs
[source]: https://github.com/openai/codex/blob/464add49587a7cb6032d5ab04faa8a1585947853/codex-rs/login/src/auth/source.rs
[account]: https://github.com/openai/codex/blob/464add49587a7cb6032d5ab04faa8a1585947853/codex-rs/app-server-protocol/src/protocol/v2/account.rs
[notifications]: https://github.com/openai/codex/blob/464add49587a7cb6032d5ab04faa8a1585947853/codex-rs/app-server/src/account_notifications.rs
[provider-auth]: https://github.com/openai/codex/blob/464add49587a7cb6032d5ab04faa8a1585947853/codex-rs/model-provider/src/auth.rs
[client]: https://github.com/openai/codex/blob/464add49587a7cb6032d5ab04faa8a1585947853/codex-rs/core/src/client.rs
