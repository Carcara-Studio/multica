# Windows validation and scoped MXC adoption

Governing scope: R5 / [#114](https://github.com/Carcara-Studio/multica/issues/114).
Validated fork main: `4e39c7dcf3659843e38af7a2599d5f4ef2107734`.
No runtime code, schema, scheduling or fallback behavior changes in this record.

## Results

Real Windows/account runs were authorized by the maintainer. One development
daemon was active, using the fork CLI/API; the installed global Multica was not
replaced. The development desktop had opened successfully earlier and was then
closed for memory pressure. Later execution was driven through the native CLI.

| Case | Evidence | Result and limit |
| --- | --- | --- |
| OpenCode completion/diagnostics and directory exclusion | PRs #119, #121, #123; post-merge CI `40544331309` | Delivered regression fixes; fake tests/CI alone are not real-agent proof. |
| Single Codex/MXC task | DEV-12; run `01a17e93-fadf-7e90-a702-615a893198c1` | One completed run; exact shell-written/read marker; expected actor; delivered `in_review`. |
| Mixed hierarchy with MXC | DEV-13 parent; DEV-14 OpenCode and DEV-15 Codex | Exactly two children and four completed runs (manager initial, two children, manager continuation); no errors. |
| Native continuation and workflow | Child agent timelines and manager run `01a17e9a-a540-76e9-bba2-2756eb79a185` | Both children delivered `in_review`. Operator verified artifacts/authors and accepted each as `done` using user CLI `--no-start`. Manager resumed without manual rerun, verified markers and delivered `in_review`. Acceptance was an operator action. |
| MXC filesystem boundary | Native standalone CLI probe | Workspace write succeeded; sibling sentinel write was denied and original content remained intact. Narrow functional check, not a security audit. |
| Legacy setup avoidance | Fresh DEV-12/DEV-15 homes | No legacy setup marker or `.sandbox-secrets` directory; no setup window reported. Desktop was not visually monitored, so absence of report alone is not proof. |
| Usage completeness | Mixed four runs | Four terminal runs measured, zero missing; input 167026, output 2673, cache read 1218560. No causal savings or zero-cost claim. |

The parent initially stopped while children waited for the shared local-directory
lock. Children then ran serially; native continuation followed acceptance.
Exactly-once child creation and expected authorship were checked. This clean
case supersedes the earlier script that incorrectly asked agents to set Done;
it does not rewrite the older incident as a successful test.

Sanitized local evidence is retained in the maintainer's personal workspace:
`mxc-multica-result.json`, `mxc-mixed-setup.json`,
`mxc-mixed-pre-acceptance.json`, `mxc-mixed-result.json`. Issue #114 records the
results without credentials or private transcripts. Local evidence is not
required for repository builds.

## Why the running legacy service did not prevent prompts

Multica's task homes isolate configuration/session exposure. A fresh home lacks
Codex's native setup state. The installed standalone CLI was `0.156.1`; the
packaged sandbox service shipped with CLI `0.164.0-alpha.11.1`.

Inspected service source requires a client with matching Windows package
identity. A native `GetPackageFamilyName` probe on a suspended child of the exact
standalone executable returned `APPMODEL_ERROR_NO_PACKAGE` (15700); the child was
terminated without resuming. The service disconnects rejected callers before a
structured response. The standalone client maps EOF/broken pipe to Unavailable
and falls back to interactive elevated provisioning. The environment's package
family hint selects the pipe but does not grant package identity.

A credential-free, minimal-config native reproduction completed after a window
was authorized. This supports package-admission rejection for this launch
context; historical pipe traffic was not captured. Protocol version and the
inspected protocol/helper-materialization sources matched, so the version
numbers alone do not establish protocol incompatibility. Current post-setup
ACL checks cannot reconstruct historical ACL repair needs.

Source references: OpenAI Codex commits
`b412ff32c417f855c2b2d1581b77058eed87c84b` (standalone source) and
`6905ecede7816e91373e9eabda3f6b0368fc747b` (service source), specifically
`windows-sandbox-service/src/package_identity.rs`, `ipc.rs` and
`windows-sandbox-rs/src/provisioning_client.rs` under `codex-rs/`.

## Adoption boundary and rollback

Only the synthetic **R5 Codex preflight** agent currently has these custom args:

```json
["-c", "windows.sandbox=\"mxc\""]
```

The previous custom args were empty; rollback restores `[]`. Preserve any
existing custom args when applying this to another agent. Do not modify normal
agents or global Codex settings merely by copying this record. Explicit MXC
selection is intentional: unsupported capability/policy must fail visibly,
rather than silently prompting for legacy setup. Managed policy still applies.

The native probe passed both with packaged CLI 0.164 and the existing standalone
0.156.1; no update was needed in this environment. Official documentation
specifies its compatibility probe from 0.162 onward, so the older-version result
is an observation for this installation, not a general support promise.
Multica currently classifies `mxc` as undecidable but preserves workspace-write
and the explicit override; no runtime patch was needed for the scoped smoke.

Do not copy setup markers, `.sandbox-secrets`, or whole homes; disable UAC or
sandbox; or spoof package identity to avoid provisioning. This mitigation does
not repair the legacy standalone/service admission contract.

## Final application handoff and scope of acceptance

After maintainer login, the development desktop on renderer port 5292 was
verified against local API 18198. The user initially landed in the older
Multica Dev workspace; switching through the UI to Dev exposed the recent cases.
DEV-13 showed both child issues (2/2), the initial waiting reply, the final
MXC_MIXED_OK reply, In Review status and a completed run triggered by wakeup.
The R5 Codex preflight profile showed Online/Idle, successful DEV-12/DEV-15 work,
and Custom Args/command preview `codex app-server -c windows.sandbox="mxc"`.
No settings were edited or tasks dispatched during this visual/read-only check.

The desktop rebuilt its bundled CLI at `dea9d117f`, a documentation-only
successor to the tested runtime commit. Its managed daemon was healthy at that
commit with zero active tasks. The manual daemon was stopped before launching
the desktop; the global installed application was left untouched.

This completes the bounded R5 validation and application handoff for this local
Dev workspace and accepted upstream UI baseline. The sole Codex agent there
already has the verified setting. This does not establish machine-wide rollout,
readiness of every historical workspace, another PC, or R4 activation. The
executions occurred through the CLI before the final desktop review; do not
claim that new inference was launched from the final desktop session.

Assess additional behavior only if the intended work needs it. Official MXC
constraints include host-loopback access, managed-network local-binding
requirements, and stopping remaining children when a foreground command exits.
These are adoption limits, not reasons to create an open-ended test program.
No detached development server or managed proxy workflow is certified here.

The integrated upstream UI baseline `c9b6de856` remains accepted. Upstream HEAD
`d7d1831fcf39b874911aae1e48b2fbf804bd068e` was rechecked for this documentation
review; this change touches fork-only documentation and imports no upstream
code. Later full synchronization remains ongoing maintenance, not a moving
completion gate. R4 stays disabled until its independent credential/evidence
activation gates pass; R5 smoke cannot activate it.

Official compatibility and behavior reference:
[Windows sandbox](https://learn.chatgpt.com/docs/windows/windows-sandbox).
