# Project continuity checkpoint

Pilot #159 / ADR-040. Publish a short account of decisions, pending work and the
next action. The next task in that project automatically receives it through the
shared claim project context. This is manual publication plus automatic delivery,
not automatic extraction or evidence of agent use/token savings.

## API and CLI

Human member management: GET/PUT/DELETE `/api/projects/{id}/continuity`.
Owner/admin/member write access follows current workspace membership. Daemon/task
actors are rejected. All reads/writes filter by project and workspace; no shared
workspace fallback or nearest-project lookup exists.

GET returns project_id, revision, content, reviewed_by and reviewed_at. An untouched
project has empty fields except project_id. PUT requires content and expected_revision;
DELETE requires expected_revision and returns the cleared checkpoint with its new
revision. Stale writes return409. Non-members/foreign projects retain not-found
behavior. Member identity identifies the publishing account, not source truth.

```text
multica project continuity get <project-id>
multica project continuity set <project-id> --file checkpoint.json
multica project continuity clear <project-id> --file clear.json
```

First-publication checkpoint.json:

```json
{"expected_revision":"","content":"Decision: preserve project isolation. Pending: validate a fresh task. Next: verify current source before changing behavior."}
```

For updates/clear, copy the current revision from GET. clear.json contains only
expected_revision. Clear retains a revision, so obtain it before publishing again.
On409, read/review the competing content before explicitly retrying. GET output
has metadata and is not itself a valid write request.

PowerShell stdin avoids quoting JSON at the command line:

```powershell
Get-Content -Raw checkpoint.json | multica project continuity set <project-id> --file -
```

Input is bounded to16KiB; content limits are4000 UTF-8 /6000 JSON-encoded bytes.
The whole optional memory section stays below8192 bytes. CLI outputs typed JSON;
malformed receipts fail and raw HTTP error bodies are omitted. No automatic retry.
There is no UI in this increment.

## Delivery, limits and first pilot

The claim resolver appends JSON-quoted content with project/revision/publisher/time
to the existing description transport. Existing project descriptions/resources
remain intact. All four task source types use that resolver; no-project or foreign
project tasks receive no checkpoint. Project-wide publication intentionally makes
the checkpoint available to permitted tasks in that project, including its squads.
No squad-private data is collected or automatically published. Cross-project
lessons require a future explicit sharing contract; personal Codex memory stays out.

The brief says this is historical context, not instructions/permissions. Mutable
claims require source verification. There is no code freshness checker, mandatory
memory readiness claim, search model, Jev call or summary of all logs. Corrupt
optional checkpoints are omitted with a warning that contains no content.

Clearing affects subsequent claims, not already delivered briefs/sessions/runtime
files. Avoid credentials and unnecessary personal data. Project deletion removes
its checkpoint with the row; downgrade drops these columns, so export/backup first.

For the first real project, publish one relevant checkpoint, then start a genuinely
needed task in a fresh session. Record the supplied revision separately from the
agent's demonstrated use of it. Check a decision retained, mutable facts rechecked
and a pending action continued. Use the existing issue usage telemetry as secondary
measurement; delivery alone does not prove better continuity or lower token cost.
