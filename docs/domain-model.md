# Domain model

Agent Board is built around durable product/domain objects. UI framework details do not define or own these objects.

## Core objects

### Project

Top-level work, repository and policy boundary.

Owns or scopes:

- Board/Issues
- local v0.1 repository configuration
- later Source Connection binding
- workflow/delivery policy
- Project-scoped Agents/configuration where supported

### Issue

The durable unit of work.

An Issue owns one authoritative Workspace in v0.1 and may have multiple execution attempts (Runs).

Board workflow:

```text
BACKLOG -> TODO -> IN_PROGRESS -> BLOCKED -> REVIEW -> DONE
```

`BLOCKED` is a durable Issue state and its normal Board column projection. Run states remain separate.

### Agent

Durable worker identity/configuration, not a process or container.

An Agent references one Executor Profile and may define operational policy such as concurrency.

### Executor Profile

Reusable execution selection:

```text
Name
Engine
Model Profile
Runtime
```

Executor Profile references Runtime directly.

### Provider

Configured model/inference connection and credential boundary.

### Model Profile

Reusable model selection and inference settings associated with a Provider. May define scheduler capacity.

### Runtime

Reusable configured execution environment and complete execution policy.

Contains implementation kind, image, resources, timeout, network policy, Workspace policy, allowed secret references, tooling/capabilities, enabled state and health/preflight information.

### Runtime Instance

Disposable compute/session materialized from a Runtime for execution.

Runtime Instance identity is not Agent or Run identity. Destroying an instance never destroys the Issue Workspace.

### Run

One durable execution attempt for an Issue by an Agent.

A Run records status, attempt identity, scheduler ownership, immutable execution provenance, execution evidence and relationships to Workspace/Runtime Instances.

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
Engine + Model Profile + Runtime -> Executor Profile
Agent -> Executor Profile
```

There is no Runtime Profile layer.

## Identity and lifetime invariants

```text
Issue != Run
Agent != process
Run != Runtime Instance
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

The first v0.1 source is a local Git repository accessible to the trusted backend and constrained to deployment-authorized roots.

Project repository configuration materializes the durable Issue Workspace automatically. Bootstrap failure never silently falls back to an unrelated empty repository.

Authenticated remote Source Connections are layered on later without replacing Workspace identity/lifecycle.

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
