# Architecture

Agent Board is a Go-backed, server-owned work board for autonomous software agents. This document is authoritative for implementation architecture. `product-v0.1.md` defines user-facing behavior and `roadmap.md` defines ordering.

## Architectural priority

Reach the complete v0.1 coding flow as quickly as possible:

```text
Local Project repository
 -> Issue
 -> Agent
      -> Engine
      -> Model Profile -> Provider
 -> durable scheduler claim
 -> selected connected Runner
 -> durable Issue Workspace
 -> Workspace transfer
 -> Execution Session
 -> Engine process
 -> durable execution evidence
 -> Question/resume when needed
 -> Review
 -> human delivery gate
```

Do not add parallel schedulers, Run lifecycles, Engine-owned Workspaces, or configuration layers that do not solve a product need.

## Stack

```text
Frontend         Nuxt 4 + Vue 3 + TypeScript + Tailwind CSS + Nuxt UI v4
Backend          Go (`apps/server`)
Runner           Go (`apps/agent-runner`)
HTTP             chi
Database         PostgreSQL + pgvector
Live updates     Server-Sent Events
Runner transport WebSocket (runner -> server protocol v2)
Legacy compute   Docker Runtime/Runtime Instance where existing internal code still uses it
API contracts    OpenAPI + intentional public DTOs
Blob storage     local filesystem first; S3-compatible later
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
- Engine registry/adapters
- durable Run scheduling/claiming/reconciliation
- Runner selection/admission and Project Runner policy
- canonical execution-context resolution
- Workspace/Git orchestration
- Execution Session orchestration
- Engine execution orchestration
- Questions/Decisions/Review commands
- Event persistence and SSE
- provenance, raw output and Artifact metadata
- authentication/authorization and secret resolution
- legacy Runtime/Runtime Instance configuration/lifecycle only where internal managed-compute code still uses it

HTTP handlers are adapters around separable application/domain/store/runtime logic.

### Agent runner

`agent-runner` is the Engine-neutral execution-plane binary. Production v0.1 prefers external persistent hosts; the server also supervises an internal runner. Both connect outbound to Agent Board over protocol v2 and use the same Runner/Execution Session path.

Runner, Execution Session and Run are separate identities. One Runner may execute many sessions over time. v0.1 allows one active Execution Session per Runner while keeping the protocol/session model extensible for later fleet capacity.

See `agent-runner.md`.

### PostgreSQL

PostgreSQL is authoritative for durable structured state and scheduler/session ownership. `packages/database/schema.sql` is the canonical pre-release schema.

### Blob/output storage

Large stdout/stderr, protocol output and Artifacts use durable opaque blob/output references instead of oversized Event rows.

## Canonical configuration hierarchy

```text
Provider -> Model Profile
Engine + Model Profile -> Agent
```

Agents do not configure Runtime, Runtime Instance, Runner, Executor Profile or Runner Profile. Runner placement is scheduler-owned. Project policy may restrict eligible external Runners, but that is not Agent configuration.

## Preferred v0.1 execution path

```text
Run
 -> existing durable scheduler
 -> selected connected Runner
 -> Execution Session
 -> server-side Engine adapter
 -> Engine process on Runner host
```

Runner selection uses live authenticated protocol-v2 capability state plus Project policy. External persistent Runners are preferred for production. The server-managed internal Runner is fallback when `allow_internal_runner` permits it. The same protocol/session path is used for both.

External Runner execution does not create a Runtime Instance.

## Legacy/internal managed compute

Runtime and Runtime Instance remain supported only where existing internal managed-compute code still uses them. They are not the normal v0.1 Agent configuration or preferred production execution path.

A Runtime describes reusable managed-compute policy such as implementation kind, image, resources, timeout, network policy, Workspace policy and allowed secret references. A Runtime Instance is disposable compute materialized from that Runtime and, on this legacy path, is bound to exactly one Workspace for its lifetime.

Engine adapters still execute through `agent-runner`; they never receive Docker clients, Docker sockets, provider-specific runtime handles or raw WebSocket framing. The trusted backend may access Docker for this internal legacy implementation, while Agent-executed code never receives Docker daemon credentials.

See `runtime-contract.md` and `runtime-execution.md` for this compatibility boundary.

## Project repository and Workspace

The first v0.1 repository source is a local Git repository accessible to the trusted backend. Project owns the validated local source/path plus base/default branch.

v0.1 uses one durable authoritative Workspace per Issue. It is materialized from that repository, has a stable Issue working branch, and is reused by later attempts.

Before Runner execution, the complete non-ignored Git state required by #68 is transferred to a per-session Runner Workspace without manufacturing visible transport commits/refs or altering authoritative staging. Returned changes are applied back to the durable Workspace while preserving server-authoritative staging.

Local repository paths are validated against deployment-authorized repository roots. Project repository configuration must not become arbitrary filesystem access.

A repository/bootstrap failure is actionable; execution never silently falls back to an unrelated empty repository.

Authenticated remote Source Connections are layered on after the first local-repository v0.1 flow is proven and reuse the same Workspace identity/lifecycle.

See `source-control.md` and `agent-runner.md`.

## Scheduler

```text
command/request
 -> persist QUEUED Run + durable job
 -> return
 -> PostgreSQL scheduler admission
 -> reserve Agent/Model capacity
 -> select eligible connected Runner
 -> create/own Execution Session
 -> STARTING
 -> RUNNING
```

Admission composes Agent concurrency, Model Profile capacity and live Runner eligibility/capacity. Capacity-only waits remain `QUEUED` rather than changing the Issue to `BLOCKED`.

Human continuation intent is durable with the Question/Review decision. Native blocking Questions keep the same live Runner/native Engine session attached through `WAITING_FOR_INPUT`; scheduler Agent/Model capacity policy is separate from Runner session ownership.

See `scheduler.md`.

## Execution context and secrets

One trusted Go resolver composes safe Project/Issue/Agent/Engine/Model/Provider/Workspace data plus explicit resume/Review context. Runner identity is selected by scheduling, not read from Agent configuration. Legacy Runtime data is included only when the internal managed-compute path actually uses it.

Secret material is separate and ephemeral:

```text
encrypted secret/reference
 -> authorize
 -> resolve in trusted Go boundary
 -> inject into assigned Runner Execution Session
 -> redact before every durable sink
```

Untrusted execution code cannot gain control-plane privileges through network reachability or caller-controlled identity.

## Events and live updates

```text
Engine / Runner
 -> normalize + redact
 -> persist Event
 -> projections
 -> SSE
```

Legacy Runtime lifecycle may also emit infrastructure Events when that path is used.

The browser reconstructs live state from persisted reads plus SSE and is never the sole owner of important activity. Nuxt/Nitro process state is never authoritative for Run state.

## Provenance and Review evidence

Every Run stores immutable safe execution provenance including the selected Runner and resolved Engine/Model/Provider configuration. Runtime/Runtime Instance provenance is recorded only for an attempt that actually used legacy internal managed compute. Run inspection and Review use this durable evidence rather than mutable current configuration.

Review represents the complete candidate: staged, unstaged, new/untracked, deleted/renamed files, tests, commands, Artifacts and relevant messages where available.

See `execution-evidence.md`.

## Questions and continuation

Blocking Questions move the same Run to `WAITING_FOR_INPUT` and the Issue to `BLOCKED`. `BLOCKED` is both a durable Issue state and its normal Board column projection. Answering may resume the same Run according to Project policy.

For native OpenCode Questions, the same live Runner Execution Session and Runner Workspace remain attached while waiting. The answer continues that native session; Agent Board does not create a replacement Engine process merely to continue the Question. If the Runner/transport actually fails, the server reconciles the durable Execution Session before deciding any terminal outcome or safe recovery.

## Delivery policy

Human Review is the v0.1 delivery gate.

After authenticated Source Connections and provider actions exist, Projects may explicitly opt into an autonomous PR/MR delivery policy. That policy may create/update a PR/MR after successful verification without requiring Agent Board's internal approval first.

PR/MR creation does not imply auto-merge or deployment. Stronger delivery permissions require separate explicit policy.

## Future layers

Planning, Automations, Agent-created Issues, Source Connections, delivery automation, delegation, Squads, workers, users/groups and Plugins reuse the same Issue/Agent/Run/scheduler/Workspace model.

Future Worker/Pool selection may supply or place Runner capacity above the existing protocol. Worker identity remains separate from Agent, Runner and Execution Session identity.

Plugins are deliberately late roadmap work.

## Invariants

1. Issue is the durable unit of work.
2. Agent identity is not process identity.
3. Run identity is not Runner or Execution Session identity.
4. Workspace lifetime is independent from Runner transport lifetime and any legacy Runtime Instance lifetime.
5. One logical writer owns an Issue Workspace during an active Runner execution lifecycle.
6. One Runner may execute many Execution Sessions over time; v0.1 permits one active session per Runner.
7. One Execution Session owns one process tree.
8. Runner placement is scheduler-owned; Agents select Engine and Model Profile, not Runner or Runtime.
9. Runtime/Runtime Instance are legacy internal managed-compute concepts, not canonical production placement.
10. PostgreSQL owns durable scheduling/session state.
11. Browser/request lifetime never owns execution or continuation.
12. Engine processes execute on the selected Runner through `agent-runner`.
13. Engine adapters remain server-side.
14. Runner/server transport is protocol-v2 WebSocket and session-scoped.
15. Secrets are ephemeral and redacted before persistence.
16. Historical execution truth comes from immutable provenance.
17. Nuxt is presentation/application delivery, not a competing control plane.
18. `BLOCKED` is an Issue workflow state; Run execution states remain separate.
19. Human Review is the default v0.1 delivery gate; later autonomous delivery requires explicit Project policy.
