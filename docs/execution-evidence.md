# Execution provenance, logs, Artifacts and Review evidence

Trustworthy execution evidence is part of the v0.1 critical path. A Run remains independently understandable after configuration changes, Runner reconnects and later attempts.

## Immutable Run provenance

Before/at execution ownership, persist a safe immutable snapshot of what the Run actually used.

Include where applicable:

- Project / Issue / Run / Agent
- Engine identity/version/public settings
- Model Profile
- Provider identity/type/base endpoint metadata
- selected model + generation settings
- selected Runner identity/name and advertised version/capability metadata relevant to the attempt
- Workspace identity
- repository/base/working branch
- Source Connection identity without credentials
- relevant workflow/config revision metadata
- Runtime ID/name/kind/image/effective policy and Runtime Instance identity only when the legacy internal managed-compute path was actually used

Runner identity is execution provenance because the scheduler selected that Runner for the attempt. It is not Agent configuration. Runtime/Runtime Instance evidence is conditional legacy provenance, not a required canonical Runner execution field.

Never persist secret plaintext in public execution evidence. Editing current Agent/Model/Provider/Runner configuration does not alter historical Run provenance. Editing legacy Runtime configuration likewise does not rewrite provenance for attempts that used it.

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

Runner transport/Workspace transfer progress Events are normalized, redacted execution evidence. Live connection state itself remains ephemeral; persisted Events/provenance describe what happened without making `last_seen` equivalent to scheduler eligibility.

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
- selected Runner and Execution Session diagnostics/provenance
- legacy Runtime/Runtime Instance lifecycle only for attempts that actually used internal managed compute
- raw logs where useful
- Artifacts
- blocking/Review guidance

Unknown Events remain visible through a safe diagnostic fallback.

## Complete candidate evidence

Review and Run inspection share one canonical public evidence/read-model path.

The candidate includes all relevant authoritative Issue Workspace changes:

- unstaged tracked modifications
- staged/index changes
- new/untracked candidate files
- deleted files
- renamed files

Ordinary unstaged-only `git diff` is not a complete candidate representation.

Ignored/runtime-private/transport-private files are not automatically deliverable candidate content. Runner transfer refs/commits and Runner-local checkout internals must not appear as product-visible candidate history.

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

The private delivery snapshot is trusted backend Workspace state, not an Artifact. It is stored under the durable Workspace root with restricted permissions, is never transferred into an active Runner session, and is not exposed through Artifact, raw-output, Run-evidence or Review APIs. Because it must preserve exact source bytes, it may contain byte sequences that are also registered for redaction; those bytes remain confined to this backend-only approval boundary.

Approval consumes this pinned private snapshot, never the current mutable Issue Workspace and never redacted public Artifacts.

## Runner Workspace and recovery evidence

The backend-owned Issue Workspace remains authoritative. Runner-local Workspace state is temporary execution materialization.

Workspace transfer evidence should make meaningful progress/failure/recovery visible without exposing transport-only Git refs, bundle internals or secrets. Sync-back after non-zero exit/cancellation is attempted where technically possible. A Run cannot be represented as successful if returned Runner work could not be verified/applied to the authoritative Workspace.

The Runner retains its session Workspace until the server has successfully applied the returned state and sent the explicit apply acknowledgement. That retained Workspace is recovery material, not authoritative product history.

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
- Runner identity/capability evidence is safe diagnostic metadata, not a credential or authorization substitute.

## Retention

Blob/log/Artifact retention is explicit and independent from the durable Issue Workspace. Cleanup does not delete Workspace state or leave historical metadata falsely claiming content remains available.

The private Review delivery snapshot must remain available for as long as its Review can still be approved or approval recovery can still require the exact candidate. In v0.1 it lives with durable backend Workspace state rather than public evidence retention. A later cleanup policy must not remove it while a pending/recoverable Review still depends on it.
