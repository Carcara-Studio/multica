# Fallback configuration management

R4 / #37 / #153. These endpoints store configuration intent; they do not enable
fallback execution or recovery. Native startup admission remains blocked by
[#152](https://github.com/Carcara-Studio/multica/issues/152).

## Access and responses

`/api/agents/{id}/fallback` uses the existing authenticated Agent resource lookup
and private-configuration policy: the human Agent owner or workspace owner/admin
may manage it. Agent actors and daemon-token contexts are rejected. The existing
ConfigManager rechecks current membership/ownership in its transaction. This is
human management, not delegated worker authorization.

- `GET`: 200 with `configured`, `execution_enabled: false`, `revision` and nullable
  `configuration`. No policy is represented by `configured: false`, not by a
  failed request. An existing strict policy is readable with `mode: "strict"`.
- `PUT`: 200 with the saved, redacted receipt. Requires explicit
  `mode: "native_login_path"`, `runtime_id`, nonempty trimmed `model`,
  `expected_revision` and a complete version1 `overrides` document.
- `DELETE`: requires `expected_revision`; returns 204 after removal. Read again
  before recreating policy. Removal preserves availability history and does not
  mark an exhausted primary healthy.

`configuration` exposes Agent/runtime IDs, mode, model/effort/tier, environment
key names, argument count and presence of MCP/runtime config. It never returns
environment values, arguments, raw MCP/runtime configuration or a Reveal API.
No response field grants execution authority. Existing write audits contain key
changes and field names, not values; persistence errors are sanitized.

## Revision and replacement semantics

Copy `revision` from GET into `expected_revision` for PUT or DELETE. For the first
creation with no history it is:

```json
{"generation":"","primary_generation":"","state_version":0}
```

After removal, GET still returns the retained primary generation/state version,
with empty configuration generation. This state-only revision is required to
recreate policy without resetting history. Stale revisions return409. Missing,
malformed, zero UUID generations in a nonempty revision, incompatible runtime
access, invalid mode/model or invalid override documents return400. Access
denials return403; lookup failures retain existing Agent resource behavior.

Example PUT body (use the real IDs and revision returned by your own GET):

```json
{
  "mode":"native_login_path",
  "runtime_id":"11111111-1111-4111-8111-111111111111",
  "model":"explicit-model-id",
  "thinking_level":"",
  "service_tier":"",
  "expected_revision":{"generation":"","primary_generation":"","state_version":0},
  "overrides":{
    "version":1,
    "custom_env":{},
    "custom_args":[],
    "runtime_config":{},
    "mcp_config":null
  }
}
```

Overrides are a complete target-local replacement, not inherited primary
credentials, arguments or MCP options. Existing environment masking rules retain
`****` values only where the existing manager permits them; a changed target
runtime cannot borrow masked credentials from the old runtime. Other override
fields must be supplied explicitly. JSON input is bounded to1MiB and unknown
request fields/trailing JSON are rejected. Body-supplied actors, catalogs or
execution enablement cannot change authorization/readiness.

The API deliberately performs no CLI/account/model lookup. Unknown model metadata
can be stored pending later validation through the existing manager; saving it
does not establish compatibility. Config and state writes, generation rotation,
audits and CAS reuse existing transactions. There is no new schema, auth service,
worker, writer/claim routing or automatic probe.

## Shared client and validation

The shared ApiClient provides getAgentFallback/saveAgentFallback/
deleteAgentFallback. JSON responses use zod and parseWithFallback. Malformed or
inconsistent receipts fail visibly rather than reporting a successful save or an
empty configuration; unknown future mode values remain readable as unknown.
Revision numbers outside JavaScript's safe integer range are rejected. Secrets
in unknown response fields are stripped before reaching consumers.

Canonical HTTP tests cover lifecycle/CAS/recreation, metadata/audit redaction,
owner/admin access, member/agent/daemon denial, bad bodies and private runtime
rejection. Existing manager tests own transaction/concurrency/audit-failure
semantics. Client tests own malformed-response and serialization behavior. No UI
or CLI command is added, and this API does not complete the full R4 delivery.
