# Domain model

Agent Board is built around durable product/domain objects. UI framework details do not define or own these objects.

## Core objects

### Project

Top-level work, repository and policy boundary.

Owns or scopes:

- Board/Issues
- repository source configuration: local repository path/default branch or Git clone URL/optional ref
- later authenticated Source Connection binding
- workflow/delivery policy
- Project-scoped Agents/configuration where supported

A Project repository source is one of:

```text
local
  -> repository path
  -> default branch

git
  -> clone URL
  -> optional ref
```

An omitted Git ref means the remote default branch is resolved at execution time. Basic Git source configuration is provider-neutral and does not imply a GitHub/GitLab/Forgejo connection or stored credentials.

### Issue

The durable unit of work.

An Issue owns one authoritative Workspace in v0.1 and may have multiple execution attempts (Runs).

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

### Runtime

Reusable configured execution environment and complete execution policy for **legacy internal managed compute only**. External runner hosts do not require a Runtime or Runtime Instance.

Contains implementation kind, image, resources, timeout, network policy, Workspace policy, allowed secret references, tooling/capabilities, enabled state and health/preflight information.

### Runner

Deployment-global execution capacity. External hosts and the server-managed internal runner authenticate outbound over protocol v2. Live connectivity is ephemeral; persisted last-seen/capabilities never make a Runner scheduler-eligible.

### Runtime Instance

Disposable compute/session materialized from a Runtime for execution.

Runtime Instance identity is not Agent or Run identity. Destroying an instance never destroys the Issue Workspace.

### Run

One durable execution attempt for an Issue by an Agent.

A Run records status, attempt identity, scheduler ownership, immutable execution provenance, execution evidence and relationships to Workspace/selected Runner (and Runtime Instances only on the legacy managed-compute path).

Later attempts reuse the Issue Workspace.

### Workspace

Durable repository state owned by an Issue.

v0.1 uses exactly one authoritative Workspace per Issue. It survives Runtime Instance destruction and is reused by later attempts/Review changes.

### Question

Structured request for human input. A blocking Question may place the Run in `WAITING_FOR_INPUT` and the Issue in `BLOCKED`.

### Decision

Durable attributable human/product outcome.

### Event

Append-only structured execution/audit history. Persist before live publication.

### Artifact

First-class durable Run output stored outside oversized Event payloads.

### Review

Human/default delivery-gate decision over an exact candidate/attempt. Review evidence includes the complete candidate, tests and relevant execution evidence.

Later Project delivery policy may allow explicit autonomous PR/MR delivery without making auto-merge/deploy implicit.

## Canonical configuration hierarchy

```text
Provider -> Model Profile
Engine + Model Profile -> Agent
```

There is no Runtime Profile, Executor Profile or Runner Profile layer. The scheduler selects an eligible Runner.

## Identity and lifetime invariants

```text
Issue != Run
Agent != process
Run != Runtime Instance
Run != Runner
Runtime != Runtime Instance
Workspace != Runtime Instance
```

- Issue survives all attempts.
- Workspace survives Runtime Instances and is reused per Issue.
- Runtime is reusable configuration; Runtime Instance is disposable compute.
- A Run may use replacement Runtime Instances during recovery/resume.
- Historical Run truth comes from immutable provenance rather than current mutable configuration.

## Scheduler invariants

- PostgreSQL owns durable scheduling/claim state.
- Agent concurrency and Model Profile capacity are independent admission constraints.
- capacity-only waits remain `QUEUED`; they do not make an Issue `BLOCKED`.
- continuation work required after Question/Review decisions is durably recorded before success returns.

## Repository invariants

Project source configuration is explicit and mutually exclusive:

- `local` uses a trusted-backend-visible Git repository path constrained to deployment-authorized roots plus a default branch.
- `git` stores a provider-neutral clone URL plus an optional branch/tag/commit ref. Saving this configuration does not clone, fetch or validate the remote and does not require credentials.

Git-source configuration is durable input for Runner execution. Runner-managed clone/fetch/cache/worktree behavior is a separate execution concern and is not part of Project configuration.

Project repository configuration materializes the durable Issue Workspace through the source-specific execution path. Bootstrap failure never silently falls back to an unrelated empty repository.

Authenticated remote Source Connections may later supply provider-specific identity and credentials without replacing the Project source model or Workspace identity/lifecycle.

## Security invariants

- Project IDs are not authorization; ownership/scope is verified.
- Agent Runtime code is untrusted.
- Agent Runtime Instances never receive Docker daemon credentials/socket access.
- credentials/secrets are resolved in trusted code and injected ephemerally.
- secret plaintext is excluded from Events, raw logs, Artifacts, provenance and public API responses.
- caller-controlled headers cannot grant trusted actor identity.

## Collaboration extensions

Planning, Automations, Agent-created Issues, delegation, Squads and worker topology reuse the same Issue/Run/scheduler/Workspace model rather than creating parallel execution systems.

Delegation is a subtask within the current Issue; Agent-created follow-up work creates a real new Issue. Squads layer reusable leader/member configuration on delegation.

Users/groups/roles/permissions are later product administration work and are not yet fully specified. Plugin expansion comes later still.
