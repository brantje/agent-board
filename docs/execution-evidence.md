# Execution provenance, logs, Artifacts and Review evidence

Trustworthy execution evidence is part of the v0.1 critical path. A Run remains independently understandable after configuration changes, reconnects and later attempts.

## Immutable Run provenance

Before/at execution ownership, persist a safe immutable snapshot of what the Run actually used.

Include where applicable:

- Project / Issue / Run / Agent
- Engine identity/version/public settings
- Model Profile
- Provider identity/type/base endpoint metadata
- selected model + generation settings
- Runtime ID/name/kind/image/effective policy when the legacy managed-compute path was used
- selected Runner identity/name when Runner-based execution was used
- Runtime tooling/capability metadata where relevant
- Workspace identity
- repository/base/working branch
- Source Connection identity without credentials
- relevant workflow/config revision metadata

Runner identity is captured because the scheduler selected that Runner for the attempt. Runtime is captured only when the legacy managed-compute path was used.

Never persist secret plaintext in public execution evidence. Editing current Agent/Model/Provider/Runtime configuration does not alter historical Run provenance.

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

Review and Run inspection share one canonical public evidence/read-model path.

The candidate includes all relevant Workspace changes:

- unstaged tracked modifications
- staged/index changes
- new/untracked candidate files
- deleted files
- renamed files

Ordinary unstaged-only `git diff` is not a complete candidate representation.

Ignored/runtime-private files are not automatically deliverable candidate content.

### Trusted Review delivery snapshot

Public Review evidence and the bytes used for approval have different trust requirements.

Candidate Artifacts exposed through Run/Review inspection are always redacted before persistence. Approval must therefore **never** reconstruct source changes from those public Artifacts: redaction can legitimately replace byte sequences that match registered secret values, and applying those transformed bytes would no longer apply the exact reviewed candidate.

For a Review-ready Run, Agent Board captures one backend-only **Review delivery snapshot** before publishing candidate evidence. The snapshot contains:

- the exact staged binary patch
- the exact unstaged binary patch
- exact new/untracked file bytes
- executable intent for new/untracked files
- the candidate change manifest binding those values to the Run

The delivery snapshot is immutable for that Run. Public candidate Artifacts are generated as a redacted projection of this pinned snapshot. If candidate evidence publication is retried after an interruption, Agent Board reuses the existing private snapshot instead of rereading the mutable Issue Workspace. This prevents public Review evidence and approval delivery from silently describing different candidate states.

The private delivery snapshot is trusted backend Workspace state, not an Artifact. It is stored under the durable Workspace root with restricted permissions, is never mounted into an agent Runtime, and is not exposed through Artifact, raw-output, Run-evidence or Review APIs. Because it must preserve exact source bytes, it may contain byte sequences that are also registered for redaction; those bytes remain confined to this backend-only approval boundary.

Approval consumes this pinned private snapshot, never the current mutable Issue Workspace and never redacted public Artifacts.

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
- the backend-only Review delivery snapshot is not public evidence and is protected like trusted Workspace source state.
- filenames/download metadata are sanitized appropriately.
- large public reads/uploads are bounded/streamed.
- trusted delivery patch/file captures are bounded and validated before approval use.

## Retention

Blob/log/Artifact retention is explicit and independent from the durable Issue Workspace. Cleanup does not delete Workspace state or leave historical metadata falsely claiming content remains available.

The private Review delivery snapshot must remain available for as long as its Review can still be approved or approval recovery can still require the exact candidate. In v0.1 it lives with durable backend Workspace state rather than public evidence retention. A later cleanup policy must not remove it while a pending/recoverable Review still depends on it.
