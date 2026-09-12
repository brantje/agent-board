# Domain model

Agent Board is built around durable product/domain objects. UI framework details do not define or own these objects.

## Core objects

### Project

Top-level work, repository and policy boundary.

Owns or scopes:

- Board/Issues
- repository source configuration
  - `local` -> server-accessible repository path/default branch
  - `git` -> clone URL and optional source ref
- workflow/delivery policy
- Project-scoped Agents/configuration where supported
- Runner eligibility policy
- later Source Connection/provider-action binding without replacing the source model

### Issue

The durable unit of work.

An Issue owns one durable Workspace identity and one deterministic Git branch:

```text
agent-board/<issue-key>
```

It may have multiple execution attempts (Runs), all continuing that branch unless the lifecycle explicitly fails closed.

Core Issue fields include title, description, durable Board status, priority and optional assigned Agent.

Each Issue has a public key `<project.issue_prefix>-<number>` allocated atomically per Project. The prefix is configured when the Project is created, is globally unique and immutable. The key is the public Issue identifier in URLs and APIs; the internal persistence identity remains a UUID.

Priority uses the numeric vocabulary `0..4`: `0` is the default/base priority and higher integers represent higher relative priority. Priority is metadata, not an execution command; it does not independently change status, bypass workflow policy or create/cancel Runs.

Board workflow:

```text
BACKLOG -> TODO -> IN_PROGRESS -> BLOCKED -> REVIEW -> DONE
```

`BLOCKED` is a durable Issue state and its normal Board column projection. Run states remain separate.

### Issue Relationship

A durable Project-scoped directed relationship from one source Issue to one target Issue.

Canonical types:

```text
blocks
  source blocks target

depends_on
  source depends on target

related_to
  source records target as related

duplicates
  source declares itself a duplicate of target
```

Both Issues must belong to the same Project. Self-relations and duplicate source/target/type rows are invalid.

The stored relationship is always source -> target. Agent Board does not synthesize a second inverse row. In particular, a stored `blocks` relation is not also persisted as a `depends_on` relation, and `related_to` remains a directed durable record even when its human interpretation is symmetric.

Relationships are server-authoritative workflow inputs. Creating or deleting a relationship does not by itself mutate Board status or Run state; any blocker/dependency policy effect is evaluated by trusted server-side workflow logic rather than by the browser.

### Agent

Durable worker identity/configuration, not a process or container.

An Agent selects Engine and Model Profile directly and may define operational policy such as concurrency. Runner placement is scheduler-owned at Run time.

### Provider

Configured model/inference connection and credential boundary. Shared (instance-wide) or Project-owned; shared Providers are visible and read-only inside a Project.

### Model Profile

Reusable model selection and inference settings associated with a Provider. May define scheduler capacity.

### Runner

Deployment-global execution capacity. External hosts and the server-managed internal Runner authenticate outbound over protocol v2. Live connectivity is ephemeral; persisted last-seen/capabilities never make a Runner scheduler-eligible.

For remote Git Projects, a Runner also owns execution-side bare repository caches and per-Run worktrees. Those are execution materialization/recovery state, not new product/domain code-state objects.

### Run

One durable execution attempt for an Issue by an Agent.

A Run records status, attempt identity, scheduler ownership, immutable execution provenance, execution evidence and relationships to Workspace and selected Runner.

Later attempts continue the Issue's durable Git branch. A blocking Question may keep the exact same live Runner/Execution Session/Engine session and checkout rather than creating a new attempt.

### Workspace

Durable Issue repository identity and Git continuity record.

Each Issue has one Workspace identity. It records the deterministic working branch, exact base revision and current accepted/published Issue revision required to prove continuity.

For a local Project, the Workspace also points at the durable server-owned Issue checkout. For a remote Git Project, the durable code state is the published remote `agent-board/<issue-key>` branch plus the Workspace's recorded revision; per-Run Runner worktrees are temporary execution/recovery materialization.

Workspace lifetime is independent from Runner connection/process lifetime.

### Project Workspace

For local Projects only, the backend-owned checkout representing the current accepted target-branch state. It is the local integration target used by Review approval and is not an agent execution environment.

Remote Issue branch publication does not create an equivalent remote target-integration object and does not mean the remote target branch was merged.

### Execution Session

A durable process-execution identity bound directly and immutably to one selected Runner.

One Execution Session owns one process tree. A Runner may own many sessions over time and may execute multiple sessions concurrently up to advertised capacity. Session ownership survives control-plane transport loss and is reconciled before replacement work can start.

### Question

Structured request for human input. A blocking Question may place the Run in `WAITING_FOR_INPUT` and the Issue in `BLOCKED`.

`WAITING_FOR_INPUT` is not a Git finalization boundary. The same live execution checkout may remain dirty while waiting.

### Decision

Durable attributable human/product outcome.

### Event

Append-only structured execution/audit history. Persist before live publication.

### Artifact

First-class durable Run output stored outside oversized Event payloads. Artifact/evidence storage is not an alternate representation of reviewed source code.

### Review

Human/default delivery-gate decision over an exact Run attempt and exact Git identity.

A Review pins:

```text
base_revision
review_revision
```

The reviewed code is `tree(review_revision)` and the Review diff is reproducible as `git diff base_revision..review_revision`. Tests, commands, messages, Artifacts, usage and provenance are supporting evidence, not a second candidate/source snapshot.

Request Changes advances the same Issue branch; prior Reviews remain immutable because their pinned SHAs do not change.

Later Project delivery policy may allow explicit autonomous PR/MR delivery without making auto-merge/deploy implicit.

## Canonical configuration hierarchy

```text
Provider -> Model Profile
Engine + Model Profile -> Agent
```

There is no Executor Profile or Runner Profile layer. The scheduler selects an eligible Runner.

## Identity and lifetime invariants

```text
Issue != Run
Agent != process
Run != Runner
Run != Execution Session
Runner != Execution Session
Workspace != Runner worktree
Review != filesystem snapshot
```

- Issue survives all attempts.
- Workspace survives Runner sessions and is reused per Issue.
- Git commits/branches are the durable code state.
- One Execution Session has one immutable Runner binding and owns one process tree.
- Historical Run truth comes from immutable provenance plus pinned Git identities rather than current mutable configuration.

## Scheduler invariants

- PostgreSQL owns durable scheduling/claim state.
- Agent concurrency and Model Profile capacity are independent admission constraints.
- Runner eligibility and advertised capacity are execution admission constraints.
- capacity-only waits remain `QUEUED`; they do not make an Issue `BLOCKED`.
- continuation work required after Question/Review decisions is durably recorded before success returns.
- Runner selection does not alter the Issue branch identity.

## Repository invariants

- every Issue branch is deterministically `agent-board/<issue-key>`.
- local and remote Runner hand-back use the same branch-ownership, ancestry, conflict and commit-leftovers finalization rules.
- an Engine-created commit is preserved; uncommitted non-ignored leftovers are committed at a true hand-back boundary.
- local Runner synchronization transfers branch history rather than preserving arbitrary Base/Index/Worktree filesystem state.
- remote Runner caches fetch source refs into `refs/remotes/origin/*` and keep Agent Board Issue branches in `refs/heads/agent-board/*`.
- remote Issue publication is a normal non-force push.
- an unexpected remote Issue-branch advance/rewrite is rejected rather than silently adopted.
- a retained Runner worktree/session remains recovery authority until the server durably records the returned/published revision and acknowledges it.
- different repository caches may mutate concurrently; same-repository cache mutation is serialized.
- one Issue branch must not reset/rebase/mutate another Issue branch.
- Review identity is SHA-based; there is no duplicate Review filesystem delivery snapshot.

Local repository paths are constrained to deployment-authorized roots. Bootstrap failure never silently falls back to an unrelated empty repository.

## Security invariants

- Project IDs are not authorization; ownership/scope is verified.
- Agent-executed code is untrusted.
- Agent Board never provides Docker daemon credentials/socket access through the Runner protocol.
- credentials/secrets are resolved in trusted code and injected ephemerally.
- secret plaintext is excluded from Events, raw logs, Artifacts, provenance and public API responses.
- caller-controlled headers cannot grant trusted actor identity.
- remote Git uses the Runner host's configured Git authentication; #72 does not introduce provider credential forwarding.

## Collaboration extensions

Planning, Automations, Agent-created Issues, delegation, Squads and worker topology reuse the same Issue/Run/scheduler/Workspace/Git-branch model rather than creating parallel execution systems.

Delegation is a subtask within the current Issue; Agent-created follow-up work creates a real new Issue. Squads layer reusable leader/member configuration on delegation.

Users/groups/roles/permissions are later product administration work and are not yet fully specified. Plugin expansion comes later still.

Earlier #15 candidate snapshot/staging assumptions are superseded wherever they conflict with these Git-native domain invariants.
