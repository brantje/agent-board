# Execution provenance, logs, Artifacts and Review evidence

Trustworthy execution evidence is part of the v0.1 critical path. A Run remains independently understandable after configuration changes, Runner reconnects and later attempts.

Evidence describes execution. Git commits and branches are the authoritative code state; evidence does not duplicate that state as a candidate or Review filesystem snapshot.

## Immutable Run provenance

Before/at execution ownership, persist a safe immutable record of what the Run actually used.

Include where applicable:

- Project / Issue / Run / Agent
- Engine identity/version/public settings
- Model Profile
- Provider identity/type/base endpoint metadata
- selected model + generation settings
- selected Runner identity/name and advertised version/capability metadata relevant to the attempt
- Workspace identity
- repository/base/working branch
- exact execution-start revision where applicable
- Source Connection identity without credentials when that feature exists
- relevant workflow/config revision metadata

Runner identity is execution provenance because the scheduler selected that Runner for the attempt. It is not Agent configuration.

Never persist secret plaintext in public execution evidence. Editing current Agent/Model/Provider/Runner configuration does not alter historical Run provenance.

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

Artifacts may contain test reports, generated files or other useful outputs, but they are not the authoritative representation of the Issue's reviewed source tree.

## Event relationship

Events remain the durable activity timeline. Events may reference raw-output ranges and Artifact IDs, but Event payloads are not the blob store or Artifact database.

Runner transport/Git publication progress Events are normalized, redacted execution evidence. Live connection state itself remains ephemeral; persisted Events/provenance describe what happened without making `last_seen` equivalent to scheduler eligibility.

## Run inspection

Run detail is human-readable first and raw JSON second.

Show:

- summary/status/timing/attempt
- immutable provenance
- queue/wait/failure reason
- timeline/messages
- commands/tool calls and exit state
- Git branch/change evidence
- tests/checks
- selected Runner and Execution Session diagnostics/provenance
- raw logs where useful
- Artifacts
- blocking/Review guidance

Unknown Events remain visible through a safe diagnostic fallback.

## Git-native Review identity

Review and Run inspection share one canonical evidence/read-model path, but Review code identity comes from Git.

Every Review pins:

- `base_revision` — the exact Issue base commit
- `review_revision` — the exact durable Issue branch HEAD selected for that Review

The reviewed source diff is reproducible from those immutable identities:

```bash
git diff <base_revision>..<review_revision>
```

The invariant is:

```text
what the reviewer sees
==
tree(review_revision)
==
what local delivery later integrates
```

There is no backend-only Review delivery filesystem snapshot, candidate snapshot, staged/unstaged patch archive or other competing representation of reviewed source code. Public redaction therefore cannot accidentally become delivery input: local approval integrates the pinned Git commit, not bytes reconstructed from public evidence.

### Change evidence

The UI/API may derive safe file/change summaries from the Git diff between the pinned revisions and may show commands, tests, messages and Artifacts from the Run. These are evidence about the reviewed commit range.

Ignored/private/transport-private files are not part of the reviewed Git tree. Runner transfer refs, bare-cache internals and temporary worktree paths must not appear as product-visible source history.

If evidence generation is retried, the Review SHAs stay immutable. A retry may regenerate a safe projection from the same Git identities; it must not silently move to a later Issue branch HEAD.

## Runner Workspace and recovery evidence

For local Projects, the server-owned Issue branch is authoritative and a Runner checkout is a temporary materialization of that branch. For remote Git Projects, the durable remote `agent-board/<issue-key>` branch plus the Workspace's recorded revision is the authoritative unmerged Issue state; the selected Runner's retained worktree is recovery authority only until durable acknowledgement.

Workspace transfer/publication evidence should make meaningful progress, failure and recovery visible without exposing transport-only Git refs, bundle internals or secrets. Finalization after normal completion, non-zero exit or cancellation is attempted where technically safe. A Run cannot be represented as successfully handed back if the finalized branch revision could not be verified and durably returned/published.

The Runner retains its session checkout until the server has durably imported or recorded the finalized revision and sent the explicit apply acknowledgement. If a remote push succeeds but the response, database persistence or acknowledgement is lost, retry uses that same retained checkout/session and republishes the same clean branch HEAD with a normal non-force push. This retained state is recovery material, not a second product code history.

## Questions and live state

A blocking Question is evidence and workflow state, not a Git finalization boundary. `WAITING_FOR_INPUT` keeps the same Runner, Execution Session, native Engine session and checkout. No Review revision is created merely because a Question is open.

After the Decision is persisted, execution continues in the same live state. Finalization occurs only at a real completion/failure/cancellation hand-back boundary.

## Tests/checks

Represent:

- command/suite
- passed / failed / not run
- useful counts/details
- relevant failure output via raw-output references

Missing test evidence is never presented as success.

## Review history

Each Review targets an exact Run attempt and exact pair of Git revisions. Later attempts continue the same durable Issue branch and may advance its HEAD, while prior Review evidence remains historically inspectable because prior SHAs do not change.

Request Changes links the next attempt without overwriting prior evidence.

## Security and isolation

- Project isolation applies to provenance/logs/Artifacts/evidence.
- secret values never appear in Events, raw output, Artifacts, provenance or public API responses.
- Review source identity is Git SHA-based; public evidence is never replayed as source changes.
- filenames/download metadata are sanitized appropriately.
- large public reads/uploads are bounded/streamed.
- Runner identity/capability evidence is safe diagnostic metadata, not a credential or authorization substitute.
- remote Git credentials stay in the Runner host's Git configuration and are not copied into evidence.

## Retention

Blob/log/Artifact retention is explicit and independent from durable Issue Git state. Cleanup of evidence blobs does not delete Issue branches or change the pinned Review SHAs, and historical metadata must not falsely claim an Artifact remains available after its retention period.

Git history required by a pending/recoverable Review must remain available through the authoritative local Issue Workspace or published remote Issue branch. There is no separate Review filesystem archive to retain.

## Superseded evidence assumptions

Earlier #15-era documentation treated staged/unstaged/untracked filesystem capture and a private Review delivery snapshot as authoritative candidate code state. #72 supersedes that model. Those snapshot/staging assumptions are no longer authoritative; Git revisions are the only durable reviewed code identity.
