# Codex recovery probe: request isolation feasibility

R4 / #37 / #131, ADR-038. This is request-capture evidence, not an approved
production recovery adapter or permission to enable fallback.

## Verified boundary

On Windows with the existing standalone Codex CLI 0.156.1, the opt-in
`TestCodexProbeIsolationCapture` sends one request per case to a localhost fake
Responses provider. Five cases deliberately return HTTP400 `MOCK_CAPTURE_ONLY`.
One returns synthetic SSE success, verifying native CLI JSON `turn.started`,
`turn.completed` and the final `OK` message in addition to exit status. No actual
model/account inference or recovery is claimed.

The test uses an empty synthetic home, a synthetic model catalog, minimal base
instructions, a fresh ephemeral session, disabled project documents and an
environment allowlist without inherited credentials/proxies/provider overrides.
No shared auth file is copied. The endpoint records no headers. The model name
is a fixture, not an eligible production model or evidence of account access.

Six cases use the same native executable:

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

The executable version is checked before the cases run. A different version
fails this pinned contract until reviewed; the opt-in gate precedes executable
lookup. The test never calls an actual model endpoint. It verifies request shape,
not an OS-level audit of startup side effects or absence of all managed hooks.
The process working directory and configuration cwd are both the synthetic
workspace. JSON stdout is parsed separately from stderr; native status/answer
events must accompany synthetic success. No model tool calls are emitted by the
fake provider, including the negative controls.

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
