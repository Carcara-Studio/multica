# ADR-013 — Cross-provider compatibility policy

- Status: Proposed
- Date: 2026-10-14
- Decision owners: Human maintainer
- Related issues: #7
- Supersedes: None
- Superseded by: None

## Context

A useful fallback may use a different runtime/provider/model from the primary. Treating "model" as the whole execution contract would be unsafe.

Current Multica Agent execution includes more than model identity:

```text
runtime_id
runtime_config
custom_env
custom_args
mcp_config
model
thinking_level
service_tier
```

Claim-time execution may also expose task-scoped capabilities such as skills, Remote MCP, connected apps and plugin-hook tools. Individual runtimes/providers differ in what they support and how they interpret model, reasoning, service-tier, MCP and session configuration.

A fallback that can answer plain text but cannot safely satisfy the Agent's required tool/runtime contract must not be treated as an equivalent automatic target.

## Decision Drivers

- Same-provider fallback should be easy when structurally compatible.
- Cross-provider fallback is a supported architectural goal, not an assumption that every pair is interchangeable.
- Automatic fallback must fail closed on known required capability mismatches.
- Credentials and provider-specific configuration must remain within existing Multica security/authorization patterns.
- Do not silently remove tools/MCP/capabilities to make a fallback run.
- Cross-provider session IDs are not portable.
- Compatibility validation should happen as early as possible and be rechecked before execution because runtime/configuration can change.

## Considered Alternatives

### Alternative A — Require primary and fallback to use the same provider/runtime type

**Pros**

- Simplifies compatibility and session behavior.

**Cons**

- Defeats an important resilience goal: a quota-exhausted provider may need a different provider/account/runtime.
- Artificially limits valid configurations.

**Decision:** rejected as a universal restriction.

### Alternative B — Allow any runtime/model combination and let execution fail naturally

**Pros**

- Minimal validation code.

**Cons**

- Can silently lose tools or provider features.
- Failure may occur after partial work begins.
- Poor operator experience and safety.

**Decision:** rejected.

### Alternative C — Silently degrade unsupported capabilities on fallback

Examples: drop MCP, ignore thinking level, remove structured-output/tool functionality.

**Pros**

- More runs may start.

**Cons**

- Changes Agent behavior without explicit consent.
- Can produce incorrect or incomplete work.
- Makes primary and fallback semantically incomparable.

**Decision:** rejected.

### Alternative D — Allow same- or cross-provider targets only when a deterministic compatibility contract passes

**Pros**

- Supports real provider diversity.
- Fails before partial work on known incompatibility.
- Makes unsupported behavior explicit.
- Can expand incrementally as adapters expose better capability metadata.

**Cons**

- Requires a compatibility layer/matrix where upstream metadata is incomplete.
- Some theoretically workable combinations will initially be rejected because capability proof is insufficient.

**Decision:** accepted design, pending ADR approval.

## Decision

Automatic fallback may use the same or a different runtime/provider, but the fallback target must pass compatibility validation before it is considered usable.

### Compatibility categories

#### 1. Runtime identity and workspace/access

Validate that fallback runtime:

- exists;
- belongs to the Agent's Workspace;
- is an allowed runtime for that Agent/user under current Multica ownership/visibility rules;
- is not structurally deleted/unbound;
- has a provider/runtime family the daemon can execute.

A temporarily offline runtime is an availability condition, not a reason to silently route back to primary. Ordinary runtime-offline behavior applies after the fallback route is chosen.

#### 2. Model availability

If the fallback specifies a model:

- validate it against runtime/provider model capabilities when reliable catalog data is available;
- otherwise use the same validation level Multica currently uses for that runtime, plus any adapter-specific compatibility contract.

An explicitly invalid/unavailable model makes automatic fallback unusable.

An empty model continues to mean the runtime's default only when that is valid under current upstream semantics.

#### 3. Reasoning/service-tier compatibility

`thinking_level` and `service_tier` are target-specific capabilities.

A fallback may:

- configure supported values appropriate to its runtime; or
- leave them at runtime defaults.

Do not copy unsupported primary values blindly into another provider.

The fallback config is its own execution target, so compatible fallback values may legitimately differ from primary.

#### 4. Provider/runtime configuration and credentials

Fallback may need provider-specific runtime configuration or credentials different from primary.

Any storage/update path for secrets or environment overrides must reuse existing Multica authorization, redaction and audit rules where applicable. This ADR does not authorize a new plain-text secret store.

Runtime-local OAuth/session credentials already owned by the runtime should remain runtime-local rather than copied into failover state.

#### 5. Tools, MCP and task capabilities

Before automatic execution, the effective fallback path must be capable of honoring the Agent/task functionality that is required for that run.

Relevant capability families can include, depending on current upstream execution:

- local/remote MCP transport support;
- Agent skills/tools;
- plugin-hook tool exposure;
- connected-app/tool invocation;
- structured tool calling/output behavior;
- attachments/multimodal inputs;
- provider/runtime-specific execution environment requirements.

When a required capability is known to be unsupported, automatic fallback fails closed before launching the Agent.

When capability support cannot be established reliably, v1 should prefer "not automatically compatible" over silently stripping functionality.

This does not require every optional UI feature to be identical. The compatibility contract concerns functionality required by the configured Agent/task execution.

#### 6. Context window/input constraints

A fallback with a smaller context/input limit may be unusable for a particular task even if the target is otherwise valid.

Where Multica/runtime exposes deterministic context constraints before launch, validate them. Do not automatically truncate semantically important user/Issue context solely to make fallback fit unless upstream already defines an accepted compaction mechanism for that execution path.

#### 7. Session portability

Provider session IDs are runtime/provider-native and are not translated across targets.

Existing Multica runtime-equality session checks remain in place. A fallback on another runtime therefore starts without the primary runtime's provider session.

Durable Multica state, not provider-session serialization, is the cross-provider continuity boundary.

### Validation timing

Use layered validation:

#### Configuration time

Reject clearly invalid fallback configurations where current metadata makes that deterministic, for example:

- foreign/missing runtime;
- invalid known model;
- unsupported known target option;
- impossible access relationship.

#### Failover activation / route time

Recheck current structural validity because runtime/configuration may have changed since configuration.

If fallback has become structurally incompatible, do not claim normal work can route successfully; surface intervention rather than silently returning to primary because primary is known quota-exhausted.

#### Claim/pre-launch time

Revalidate runtime identity and execution-critical capabilities using current information. This is the final safety gate before partial Agent work begins.

### Compatibility result

The internal compatibility result should distinguish at least conceptually:

```text
COMPATIBLE
INCOMPATIBLE(reason)
UNKNOWN_REQUIRED_CAPABILITY(reason)
```

Both incompatible and unknown-required-capability states fail closed for automatic fallback.

Exact type/error names are implementation details.

### Fallback failure does not alter topology

If a valid fallback later fails due to:

- its own quota;
- network;
- capacity;
- auth;
- runtime outage;
- another execution failure;

ordinary task semantics apply. The system does not dynamically select a third target or ping-pong to primary in v1.

Primary recovery remains controlled only by ADR-010 probes.

## Consequences

### Positive

- Supports real cross-provider resilience.
- Prevents silent feature degradation.
- Keeps credentials/configuration in existing trust boundaries.
- Cross-provider continuity does not depend on proprietary session portability.
- Capability support can expand incrementally without weakening safety.

### Negative / Trade-offs

- Compatibility validation is more complex than checking model name.
- Some target combinations will be conservatively rejected.
- Runtime capability metadata may need additive improvements for precise validation.
- Different providers may require adapter-specific probe and validation logic.

## Data Impact

Fallback configuration stores only target-specific configuration required to reproduce the selected execution target.

Do not duplicate Agent identity/instructions/hierarchy data.

If a compatibility cache is later introduced, it must be treated as derived/expiring data and revalidated before execution; it is not the authority for runtime existence/access.

No additional session-translation data store is introduced.

## Backend / Runtime Impact

Introduce a compatibility validator shared by:

- fallback configuration validation;
- failover activation/readiness;
- fallback claim pre-launch checks;
- possibly UI diagnostic endpoints.

Prefer adapter/runtime capability declarations already present in Multica. Add fork-specific compatibility knowledge only where upstream provides no reusable contract.

Provider probe implementations from ADR-010 must use the same primary runtime/provider identity but do not need the full user-task capability set because probes intentionally disable tools/MCP.

## API / CLI / UI Impact

Fallback configuration UI should diagnose why a target cannot be used automatically.

Examples:

```text
fallback runtime belongs to another workspace
model unavailable on fallback runtime
reasoning setting unsupported
required MCP transport unsupported
credentials/config missing
runtime capability unknown for required feature
```

Do not silently save a fallback as "ready" when deterministic validation says it is incompatible.

Status should distinguish configured-but-invalid from currently active and healthy fallback.

## Failure and Edge Cases

- Same provider, different runtime/account: valid if access/config/capabilities pass.
- Different provider/runtime: valid when compatibility contract passes.
- Same runtime, different model: valid if model/options are supported.
- Fallback lacks a required MCP transport: fail before Agent launch.
- Fallback model removed after configuration: route/claim validation fails closed.
- Fallback runtime temporarily offline: fallback route remains selected; ordinary runtime availability semantics apply rather than switching to quota-exhausted primary.
- Fallback quota exhausted: task fails; remain fallback-active and continue primary probes.
- Required capability metadata unknown: automatic fallback unavailable until support can be established.
- Cross-runtime/provider task: no primary provider session reuse.

## Testing Requirements

### Configuration

- same-workspace runtime accepted;
- cross-workspace runtime rejected;
- known invalid model rejected;
- supported/unsupported thinking and service-tier values handled correctly;
- secret-bearing updates follow existing authorization/redaction behavior.

### Capability matrix

- representative same-provider fallback;
- representative cross-provider fallback;
- required MCP/tool capability mismatch fails pre-launch;
- unknown required capability fails closed;
- no silent removal of configured tools/capabilities.

### Runtime races

- runtime/model removed after configuration is detected at route/claim;
- offline fallback does not ping-pong primary;
- fallback quota does not switch primary;
- cross-runtime fallback has no primary provider session ID.

## Upstream Compatibility

This decision treats current runtime/provider adapters and capability metadata as the first source of truth and adds only missing validation needed for automatic failover safety.

It avoids forcing a global normalized capability schema onto every upstream runtime in v1. Provider-specific logic should be isolated behind a small compatibility interface so future upstream capability work can replace it.

## Open Questions

- Exact v1 compatibility matrix depends on the runtime/provider adapters present when implementation begins and must be re-inspected then.
- Which task capabilities are statically knowable before claim may evolve upstream; unknown required capabilities remain fail-closed until evidence improves.
- Exact fallback environment/credential storage must reuse the safest current Multica mechanism rather than being pre-decided in this ADR.
