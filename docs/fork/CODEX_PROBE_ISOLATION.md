# Codex recovery probe: request isolation feasibility

R4 / #37 / #131, ADR-038. This is request-capture evidence, not an approved
production recovery adapter or permission to enable fallback.

## Verified boundary

On Windows with the existing standalone Codex CLI 0.156.1, the opt-in
`TestCodexProbeIsolationCapture` sends one request per case to a localhost fake
Responses provider. Ten cases deliberately return HTTP400 `MOCK_CAPTURE_ONLY`.
Two return synthetic SSE success, verifying native CLI JSON `turn.started`,
`turn.completed` and the final `OK` message in addition to exit status. No actual
model/account inference or recovery is claimed.

The test uses an empty synthetic home, a synthetic model catalog, minimal base
instructions, a fresh ephemeral session, disabled project documents and an
environment allowlist without inherited credentials/proxies/provider overrides.
No shared auth file is copied. The endpoint records no headers. The model name
is a fixture, not an eligible production model or evidence of account access.

Twelve cases use the same native executable (the four command-hook cases are
Windows-specific):

- Without the final goal/skill controls, the request exposes native goal tools
  and automatic skill instructions despite the other tools being disabled.
- With those controls, the original captured request has zero tool definitions and
  exactly one user input, `Reply OK.`, with the same minimal base instructions.
  A project instruction canary is absent in both cases.
- With shell/search and usage-instruction metadata enabled in the synthetic
  catalog, the same controls still remove tools and extra input.
- Re-enabling `shell_tool` exposes `exec_command` and `write_stdin` despite
  `unified_exec=false`. The pinned source normalizes unified execution on unless
  managed requirements prohibit it; `shell_tool=false` is a separate gate.
- Adding model-declared apply-patch and async/clock tools exposes tools despite
  the feature opt-outs. Model metadata can directly register those tools.
- Removing those explicit model-tool declarations supplies a fixture catalog
  ceiling: zero tools/minimal input, followed by native completion of the local
  synthetic stream. This does not replace a production source of real metadata.
- User and trusted project SessionStart hooks each have a positive control and
  a blocked control. The PowerShell canary writes a marker under the synthetic
  test root and emits a unique context string. Each positive control requires
  the marker before the provider request and the extra hook context in that
  request. The project fixture has minimal local Git metadata and explicitly
  trusts its native Windows path spelling; no real repository is used.
- Both hook controls apply trust bypass only to these known test-created sources
  for that invocation, so missing hook trust cannot explain the blocked outcome.
  The blocked cases pass `-c features.hooks=false` while user/project config
  enables hooks. They require no marker before or after the request, no hook
  context, zero tools and the original single minimal user input. Do not use
  this trust bypass for a real-profile recovery probe.

The executable version is checked before the cases run. A different version
fails this pinned contract until reviewed; the opt-in gate precedes executable
lookup. The test never calls an actual model endpoint. It verifies request shape,
not an OS-level audit of startup side effects or absence of all managed hooks.
The process working directory and configuration cwd are both the synthetic
workspace. JSON stdout is parsed separately from stderr; native status/answer
events must accompany synthetic success. No model tool calls are emitted by the
fake provider, including the negative controls. Commands have a 35-second context
and a two-second pipe WaitDelay;
this bounds inherited-pipe waits, not all detached process lifetimes. Run the
whole opt-in capture with a bounded Go test timeout, for example `-timeout 3m`.

## Reproduce

From `server/`, opt in and explicitly supply the absolute CLI executable path:

```powershell
$env:MULTICA_RUN_REAL_AGENT_SMOKE = '1'
$env:MULTICA_CODEX_PROBE_CLI = 'C:/absolute/path/to/codex.exe'
go test -tags=agentintegration ./pkg/agent -run '^TestCodexProbeIsolationCapture$' -count=1 -v
```

Without the opt-in variable, this test skips before any executable/account lookup.
No global CLI installation or configuration change is needed. The `windows.mxc`
override belongs only to the synthetic configuration; the experiment does not
change a user's sandbox policy.

## Native bundled model metadata

Two additional cases obtain full metadata from the pinned native executable's
`debug models --bundled` command. Its pinned source branch dumps the embedded
catalog without loading config/auth or refreshing a live account catalog. The
test chooses an explicit visible code-mode/apply-patch model from that output,
keeps its slug and wire mode, and preserves raw metadata fields rather than the
Multica picker projection. This is genuine bundled metadata, not proof that an
actual Agent/account currently supports that selection. A fallback catalog must
not become execution authority.

The positive control exposes code-mode exec/wait despite feature opt-outs. The
ceiling case removes only apply_patch_tool_type, experimental_supported_tools
and tool_mode declarations; other metadata stays intact. Both explicitly set
agents.enabled=false, which the pinned resolver gives precedence over a model's
multi_agent_version. Feature flags alone do not provide that override.

For the selected native Responses Lite model, tools are nested in an
additional_tools input item and base instructions are a developer message;
root tools/instructions are absent. The test checks that exact layout before
checking the nested tool array, minimal instruction text and single user input.
An absent root tools field alone must never count as zero tools. The ceiling
requires an empty nested tool array and native completion of a synthetic stream.
No model tool call is emitted and no real inference is claimed.

## Managed startup admission remains closed

Pinned ManagedFeatures::normalize_candidate applies managed feature pins before
validation. Config construction uses that normalization; strict-config is not a
blanket promise that conflicting startup opt-outs are rejected. The separate
explicit-feature conflict validator is used by the inspected config-write path.
Do not confuse that write validation with startup admission or rely on opt-outs
alone when a managed policy may require hooks/plugins.

On Windows the system requirements directory comes from the ProgramData known
folder API, not a process ProgramData environment substitution. No system files,
managed settings, debug-only loader overrides or real profiles were changed to
test this. These are source findings; managed-policy rejection has not been
established by the synthetic native captures. Same-process admission before any
hook/plugin/MCP startup remains a required dependency for a real probe.

## Synthetic startup introspection

`TestCodexProbeStartupIntrospection` uses the same opt-in environment and pinned
native CLI. It starts only stdio app-server with strict config in a fresh
synthetic home/workspace, performs initialize, config/read,
configRequirements/read and paginated experimentalFeature/list, then terminates
the process. It creates no thread or turn and uses no shared authentication.
Run it explicitly with `-run '^TestCodexProbeStartupIntrospection$'`.

The native Windows test observes raw unified_exec=false but effective
unified_exec=true, while shell_tool, hooks and plugins remain false. Thus raw
config/read flags alone are not an effective-feature admission check. Pagination
must terminate without duplicate entries or oversized pages. Unknown config is
rejected under strict-config. Requirements are loaded but neither printed nor
certified: host-managed requirements can still make a real target ineligible.
The fixture provider/MCP HTTP endpoint receives zero requests during this bounded
exercise. This does not monitor all network traffic or subprocesses and is not
proof of universal startup isolation. The test does not exercise project-layer
overrides, startup hooks, marketplace synchronization or actual model inference.

Pinned app-server initializes its message processor before handling these RPCs.
That constructor can schedule plugin startup tasks; core-plugins gates those
tasks on plugins_enabled. The disable-plugin-startup-tasks-for-tests switch is
debug-only, not a release isolation control. No real-profile introspection is
approved by this test. A snapshot from this synthetic process does not authorize
another process, configuration or inference.

## Remaining gates

Do not copy this fixture catalog into production or treat a disabled-tool prompt
as isolation. Feature opt-outs alone are insufficient for arbitrary real catalog
metadata. A production adapter still needs a reviewed way to obtain actual model
metadata, retain the real explicit model/options and apply a checked tool ceiling
without inventing a model or trusting a daemon-supplied approval. It must reject
unknown or enforced-incompatible configuration before a probe can run. Verify
startup hooks/MCP independently; an empty request tool list cannot prove that no
startup process already ran. Current official docs describe merged hooks from
active config layers, including managed sources; flags do not authorize ignoring
those requirements.

The real selected path must preserve the native login and prove that no tools,
MCP, hooks, repository/user context or resumed session reach the probe. A model
catalog override affects startup and needs version/configuration fencing.

Real inference success, trusted result transport, current task/runtime/owner and
configuration binding, durable probe leases and crash/late-result settlement
remain separate requirements. Runtime workspace access alone is insufficient.
The automatic fallback and probe schedulers remain disabled.

## Sources

- [Official configuration reference](https://developers.openai.com/codex/config-reference/)
- Pinned Codex source `b412ff32c417f855c2b2d1581b77058eed87c84b`:
  [configuration schema](https://github.com/openai/codex/blob/b412ff32c417f855c2b2d1581b77058eed87c84b/codex-rs/core/config.schema.json),
  [tool registration](https://github.com/openai/codex/blob/b412ff32c417f855c2b2d1581b77058eed87c84b/codex-rs/core/src/tools/spec_plan.rs),
  [thread initialization](https://github.com/openai/codex/blob/b412ff32c417f855c2b2d1581b77058eed87c84b/codex-rs/core/src/session/session.rs).
- [Official hooks reference](https://learn.chatgpt.com/docs/hooks), consulted for
  current layer discovery and the documented `features.hooks` switch; the native
  executable contract remains pinned, not inferred from current documentation.
- Pinned [managed feature normalization](https://github.com/openai/codex/blob/b412ff32c417f855c2b2d1581b77058eed87c84b/codex-rs/core/src/config/managed_features.rs)
  and `spec_plan.rs` explain the independent shell gate and model-declared tools.
  The synthetic SSE shape follows pinned
  [response fixtures](https://github.com/openai/codex/blob/b412ff32c417f855c2b2d1581b77058eed87c84b/codex-rs/core/tests/common/responses.rs).

The internal extension ToolPolicy is not itself a public thread/turn parameter.
The capture tests the available startup configuration instead of asserting that
an internal policy can be supplied by the Multica daemon.

The bundled export and startup findings follow pinned `cli/src/main.rs`
run_debug_models_command, `core/src/config/managed_features.rs`,
`core/src/config/mod.rs`, `app-server/src/config_manager_service.rs` and
`config/src/loader/mod.rs`. Model-owned tool mode and agent defaults follow
`core/src/tools/mod.rs` and Config::multi_agent_version_override. The Lite input
layout is pinned by `core/tests/suite/responses_lite.rs` and
`protocol/src/models.rs` AdditionalTools. Current documentation was also read at
[app-server](https://learn.chatgpt.com/docs/app-server) and
[configuration](https://learn.chatgpt.com/docs/config-file/config-reference);
the implementation contract remains version-bound.

The Windows hook controls cover ordinary user/project command hooks in the
pinned executable. They do not establish suppression of managed/required hooks,
plugin/MCP handlers, every lifecycle event, all network activity or detached
processes. An incompatible managed policy must remain ineligible. These controls
do not approve a native-login adapter, actual model metadata or real inference.
