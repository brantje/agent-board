# Architecture

Agent Board is a Go-backed, server-owned work board for autonomous software agents. This document is authoritative for implementation architecture. `product-v0.1.md` defines user-facing behavior and `roadmap.md` defines ordering.

## Architectural priority

Reach the complete v0.1 coding flow as quickly as possible:

```text
Local Project repository
 -> Issue
 -> Agent
 -> Executor Profile
      -> Engine
      -> Model Profile -> Provider
      -> Runtime
 -> durable scheduler claim
 -> durable Issue Workspace
 -> Runtime Instance
 -> Engine process
 -> durable execution evidence
 -> Question/resume when needed
 -> Review
 -> human delivery gate
```

Do not add parallel schedulers, Run lifecycles, Engine-owned Workspaces, or configuration layers that do not solve a product need.

## Stack

```text
Frontend        Nuxt 4 + Vue 3 + TypeScript + Tailwind CSS + Nuxt UI v4
Backend         Go (`apps/server`)
HTTP            chi
Database        PostgreSQL + pgvector
Live updates    Server-Sent Events
Runtime         Docker first
API contracts   OpenAPI + intentional public DTOs
Blob storage    local filesystem first; S3-compatible later
```

## Core boundaries

### Web

The Nuxt application owns presentation, routing, forms, Board/Issue/Run views, Questions, Review, and live rendering. Nuxt UI is the required generic component foundation.

The web application is a client of the Go control plane. Nitro/server routes, server middleware, process memory and framework caches do not become an authoritative product backend.

Nuxt UI components should be reused for common controls/interactions rather than recreated as custom primitives.

### Go backend

The Go backend owns:

- Project/Issue/Agent configuration and commands
- Provider/Model Profile configuration
- Runtime configuration and health
- Executor Profiles and Engine registry
- durable Run scheduling/claiming/reconciliation
- canonical execution-context resolution
- Workspace/Git orchestration
- Runtime Instance lifecycle and process/session execution
- Engine execution orchestration
- Questions/Decisions/Review commands
- Event persistence and SSE
- provenance, raw output and Artifact metadata
- authentication/authorization and secret resolution

HTTP handlers are adapters around separable application/domain/store/runtime logic.

### PostgreSQL

PostgreSQL is authoritative for durable structured state and scheduler ownership. `packages/database/schema.sql` is the canonical pre-release schema.

### Blob/output storage

Large stdout/stderr, protocol output and Artifacts use durable opaque blob/output references instead of oversized Event rows.

## Configuration hierarchy

```text
Provider -> Model Profile
Engine + Model Profile + Runtime -> Executor Profile
Agent -> Executor Profile
```

Runtime is a reusable configured execution environment and owns the complete execution policy. Executor Profile references Runtime directly.

## Runtime model

Runtime contains at least:

- scope / Project ownership
- name
- implementation kind (`docker` first)
- image
- CPU/memory/PID/timeout policy
- network policy
- Workspace policy
- allowed secret references
- tooling/capability metadata
- enabled state
- health/preflight information

Runtime Instance is disposable compute materialized from the selected Runtime and is a separate identity.

## Project repository and Workspace

The first v0.1 repository source is a local Git repository accessible to the trusted backend. Project owns the validated local source/path plus base/default branch.

v0.1 uses one durable authoritative Workspace per Issue. It is materialized from that repository, has a stable Issue working branch, survives Runtime Instance destruction, and is reused by later attempts.

Local repository paths are validated against deployment-authorized repository roots. Project repository configuration must not become arbitrary filesystem access.

A repository/bootstrap failure is actionable; execution never silently falls back to an unrelated empty repository.

Authenticated remote Source Connections are layered on after the first local-repository v0.1 flow is proven and reuse the same Workspace identity/lifecycle.

See `source-control.md`.

## Scheduler

```text
command/request
 -> persist QUEUED Run + durable job
 -> return
 -> PostgreSQL scheduler admission
 -> reserve constraints atomically
 -> STARTING
 -> RUNNING
```

Admission composes Agent concurrency and Model Profile capacity. Capacity-only waits remain `QUEUED` rather than changing the Issue to `BLOCKED`.

Human continuation intent is durable with the Question/Review decision.

See `scheduler.md`.

## Runtime execution

```text
Executor Profile
 -> Runtime
 -> verify accessible/enabled/runnable
 -> validated Runtime Spec
 -> Runtime implementation
 -> Runtime Instance
 -> process/session
 -> Engine
```

Engine commands execute inside the selected Runtime Instance. Runtime sessions support Workspace-bounded cwd, ephemeral environment/secrets, stdout/stderr, exit status, cancellation and cleanup.

The trusted backend may access Docker. Agent Runtime Instances never receive the Docker socket.

## Execution context and secrets

One trusted Go resolver composes safe Project/Issue/Agent/Executor/Model/Provider/Runtime/Workspace data plus explicit resume/Review context.

Secret material is separate and ephemeral:

```text
encrypted secret/reference
 -> authorize
 -> resolve in trusted Go boundary
 -> inject into Runtime process/session
 -> redact before every durable sink
```

Untrusted Runtime code cannot gain control-plane privileges through network reachability or caller-controlled identity.

## Events and live updates

```text
Engine / Runtime
 -> normalize + redact
 -> persist Event
 -> projections
 -> SSE
```

The browser reconstructs live state from persisted reads plus SSE and is never the sole owner of important activity. Nuxt/Nitro process state is never authoritative for Run state.

## Provenance and Review evidence

Every Run stores immutable safe execution provenance including the direct Runtime selected through its Executor Profile. Run inspection and Review use this durable evidence rather than mutable current configuration.

Review represents the complete candidate: staged, unstaged, new/untracked, deleted/renamed files, tests, commands, Artifacts and relevant messages where available.

See `execution-evidence.md`.

## Questions and continuation

Blocking Questions move the same Run to `WAITING_FOR_INPUT` and the Issue to `BLOCKED`. `BLOCKED` is both a durable Issue state and its normal Board column projection. Answering may resume the same Run according to Project policy. Workspace state persists and a replacement Runtime Instance may be materialized from the same Runtime.

## Delivery policy

Human Review is the v0.1 delivery gate.

After authenticated Source Connections and provider actions exist, Projects may explicitly opt into an autonomous PR/MR delivery policy. That policy may create/update a PR/MR after successful verification without requiring Agent Board's internal approval first.

PR/MR creation does not imply auto-merge or deployment. Stronger delivery permissions require separate explicit policy.

## Future layers

Planning, Automations, Agent-created Issues, Source Connections, delivery automation, delegation, Squads, workers, users/groups and Plugins reuse the same Issue/Agent/Run/scheduler/Workspace model.

Plugins are deliberately late roadmap work.

## Invariants

1. Issue is the durable unit of work.
2. Agent identity is not process identity.
3. Run identity is not Runtime Instance identity.
4. Workspace lifetime is independent from Runtime Instance lifetime.
5. Runtime is selected directly by Executor Profile.
6. Runtime owns complete execution environment/policy configuration.
7. PostgreSQL owns durable scheduling state.
8. Browser/request lifetime never owns execution or continuation.
9. Engine processes execute inside the selected Runtime Instance.
10. Secrets are ephemeral and redacted before persistence.
11. Historical execution truth comes from immutable provenance.
12. Nuxt is presentation/application delivery, not a competing control plane.
13. `BLOCKED` is an Issue workflow state; Run execution states remain separate.
14. Human Review is the default v0.1 delivery gate; later autonomous delivery requires explicit Project policy.
