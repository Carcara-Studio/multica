# Codex recovery probe: request isolation feasibility

R4 / #37 / #131, ADR-038. This is request-capture evidence, not an approved
production recovery adapter or permission to enable fallback.

## Verified boundary

On Windows with the existing standalone Codex CLI 0.156.1, the opt-in
`TestCodexProbeIsolationCapture` sends one request to a localhost fake Responses
provider. The fake provider deliberately returns HTTP400 `MOCK_CAPTURE_ONLY`;
neither a successful native inference nor recovery is claimed.

The test uses an empty synthetic home, a synthetic model catalog, minimal base
instructions, a fresh ephemeral session, disabled project documents and an
environment allowlist without inherited credentials/proxies/provider overrides.
No shared auth file is copied. The endpoint records no headers. The model name
is a fixture, not an eligible production model or evidence of account access.

Two cases use the same native executable:

- Without the final goal/skill controls, the request exposes native goal tools
  and automatic skill instructions despite the other tools being disabled.
- With those controls, the captured request has zero tool definitions and
  exactly one user input, `Reply OK.`, with the same minimal base instructions.
  A project instruction canary is absent in both cases.

The executable version is checked before the cases run. A different version
fails this pinned contract until reviewed; the opt-in gate precedes executable
lookup. The test never calls an actual model endpoint. It verifies request shape,
not an OS-level audit of startup side effects or absence of all managed hooks.

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
as isolation. A production adapter still needs a reviewed way to select the real
explicit primary model/options while preserving the native login, reject unknown
or enforced-incompatible configuration, and prove no tools/MCP/hooks/repository/
user context/session reach the actual probe. A model catalog override affects
startup and needs version/configuration fencing.

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

The internal extension ToolPolicy is not itself a public thread/turn parameter.
The capture tests the available startup configuration instead of asserting that
an internal policy can be supplied by the Multica daemon.
