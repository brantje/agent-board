# Execution provenance, logs, Artifacts and Review evidence

Trustworthy execution evidence is part of the v0.1 critical path. A Run remains independently understandable after configuration changes, reconnects and later attempts.

## Immutable Run provenance

Before/at execution ownership, persist a safe immutable snapshot of what the Run actually used.

Include where applicable:

- Project / Issue / Run / Agent
- Executor Profile
- Engine identity/version/public settings
- Model Profile
- Provider identity/type/base endpoint metadata
- selected model + generation settings
- Runtime ID/name/kind/image/effective policy
- Runtime tooling/capability metadata where relevant
- Workspace identity
- repository/base/working branch
- Source Connection identity without credentials
- relevant workflow/config revision metadata

Runtime is captured directly because Executor Profile references Runtime directly.

Never persist secret plaintext. Editing current Agent/Executor/Model/Provider/Runtime configuration does not alter historical Run provenance.

## Raw output

Large stdout/stderr/protocol output belongs in durable bounded/chunked storage rather than oversized Event JSON.

Raw output records enough metadata for ordering/correlation, including stream/channel and timestamps where applicable. Redaction happens before persistence.

## Artifacts

Artifact is first-class durable Run output.

Metadata includes at least:

- ID
- Project/Issue/Run
- name
- type/media type
- size
- digest/checksum where useful
- opaque storage reference
- created timestamp
- safe metadata

Artifacts are listable/readable/downloadable through Project-scoped APIs.

## Event relationship

Events remain the durable activity timeline. Events may reference raw-output ranges and Artifact IDs, but Event payloads are not the blob store or Artifact database.

## Run inspection

Run detail is human-readable first and raw JSON second.

Show:

- summary/status/timing/attempt
- immutable provenance
- queue/wait/failure reason
- timeline/messages
- commands/tool calls and exit state
- file/change evidence
- tests/checks
- selected Runtime and Runtime Instance lifecycle
- raw logs where useful
- Artifacts
- blocking/Review guidance

Unknown Events remain visible through a safe diagnostic fallback.

## Complete candidate evidence

Review and Run inspection share one canonical evidence/read-model path.

The candidate includes all relevant Workspace changes:

- unstaged tracked modifications
- staged/index changes
- new/untracked candidate files
- deleted files
- renamed files

Ordinary unstaged-only `git diff` is not a complete candidate representation.

Ignored/runtime-private files are not automatically deliverable candidate content.

## Tests/checks

Represent:

- command/suite
- passed / failed / not run
- useful counts/details
- relevant failure output via raw-output references

Missing test evidence is never presented as success.

## Review history

Each Review targets an exact attempt. Later attempts may reuse and modify the same Issue Workspace, while prior Review evidence remains historically inspectable.

Request changes links the next attempt without overwriting prior evidence.

## Security and isolation

- Project isolation applies to provenance/logs/Artifacts/evidence.
- secret values never appear in Events, raw output, Artifacts, provenance or public API responses.
- filenames/download metadata are sanitized appropriately.
- large reads/uploads are bounded/streamed.

## Retention

Blob/log/Artifact retention is explicit and independent from the durable Issue Workspace. Cleanup does not delete Workspace state or leave historical metadata falsely claiming content remains available.
