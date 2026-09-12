# Architecture

Agent Board is a Go-backed, server-owned work board for autonomous software agents. This document is authoritative for implementation architecture. `product-v0.1.md` defines user-facing behavior and `roadmap.md` defines ordering.

## Architectural priority

Reach the complete v0.1 coding flow as quickly as possible:

```text
Project source (local or git)
 -> Issue
 -> Agent
      -> Engine
      -> Model Profile -> Provider
 -> durable scheduler claim
 -> selected connected Runner
 -> deterministic agent-board/<issue-key> branch
 -> source-aware Issue checkout/worktree
 -> Execution Session
 -> Engine process
 -> Git finalization
 -> durable execution evidence
 -> Question/resume when needed
 -> Review pinned to Git SHAs
 -> human delivery gate
```

Do not add parallel schedulers, Run lifecycles, Engine-owned product state, source-provider abstractions or code-state layers that do not solve a current product need.

## Stack

```text
Frontend         Nuxt 4 + Vue 3 + TypeScript + Tailwind CSS + Nuxt UI v4
Backend          Go (`apps/server`)
Runner           Go (`apps/agent-runner`)
HTTP             chi
Database         PostgreSQL + pgvector
Live updates     Server-Sent Events
Runner transport WebSocket (runner -> server protocol v2)
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
- Project source configuration (`local` or `git`)
- Provider/Model Profile configuration
- Engine registry/adapters
- durable Run scheduling/claiming/reconciliation
- Runner selection/admission and Project Runner policy
- canonical execution-context resolution
- durable Issue branch/Workspace orchestration
- Execution Session orchestration
- Engine execution orchestration
- shared Git finalization and local Review delivery
- Questions/Decisions/Review commands
- Event persistence and SSE
- provenance, raw output and Artifact metadata
- authentication/authorization and secret resolution

HTTP handlers are adapters around separable application/domain/store/execution logic.

### Agent runner

`agent-runner` is the Engine-neutral execution-plane binary. Production v0.1 prefers external persistent hosts; the server also supervises an internal Runner. Both connect outbound to Agent Board over protocol v2 and use the same Runner/Execution Session path.

For local Projects the Runner materializes a branch-only transfer from the server. For remote Git Projects it owns an execution-side bare cache plus per-Run worktrees and publishes the Agent Board Issue branch with normal Git push.

Runner, Execution Session and Run are separate identities. One Runner may execute many sessions over time. Default advertised `max_active_sessions` is 5.

See `agent-runner.md`.

### PostgreSQL

PostgreSQL is authoritative for durable structured state and scheduler/session ownership. `packages/database/schema.sql` is the canonical pre-release schema.

Git remains authoritative for source-tree identity. PostgreSQL records the branch/revision identities required to prove which Git state belongs to a Workspace, Run and Review.

### Blob/output storage

Large stdout/stderr, protocol output and Artifacts use durable opaque blob/output references instead of oversized Event rows. Blob storage is evidence storage, not an alternate source-code history.

## Canonical configuration hierarchy

```text
Provider -> Model Profile
Engine + Model Profile -> Agent
```

Agents do not configure execution hosts. Runner placement is scheduler-owned. Project policy may restrict eligible external Runners, but that is not Agent configuration.

## v0.1 execution path

```text
Run
 -> existing durable scheduler
 -> selected connected Runner
 -> Execution Session
 -> source-aware Issue checkout/worktree
 -> server-side Engine adapter
 -> Engine process on Runner host
```

Runner selection uses live authenticated protocol-v2 capability state plus Project policy. External persistent Runners are preferred for production. The server-managed internal Runner is fallback when `allow_internal_runner` permits it. The same protocol/session path is used for both.

Engine adapters receive only the selected Runner execution capability; they never receive Docker clients, Docker sockets or raw WebSocket framing. External Runner hosts own any host-level tooling they choose to install, while Agent Board never forwards daemon credentials through the Runner protocol.

## Project source and Git-native Issue state

Project source configuration is provider-neutral:

```text
local -> repository_path + default/base branch
git   -> clone_url + optional ref
```

Every Issue has one deterministic durable branch:

```text
agent-board/<issue-key>
```

Git branches and commits are the only durable representation of Issue code state. There is no parallel Review filesystem snapshot, candidate snapshot or synthetic Base/Index/Worktree state model.

At every real execution hand-back boundary shared Git logic verifies the checkout is still on the expected Issue branch, rejects unresolved conflicts, proves the recorded start revision remains an ancestor, preserves Engine-created commits, commits remaining non-ignored work when necessary and requires a clean checkout.

### Local source edge

A local Project uses the backend-owned Project Workspace as its current accepted target-branch checkout. Per-Issue Workspaces hold the persistent `agent-board/<issue-key>` branches.

Runner execution reuses the #71 transport, simplified to branch-only Git history. The server transfers the exact Issue branch HEAD, the Runner executes/finalizes it, and the finalized branch is returned/imported before apply acknowledgement. The old staging-preservation and synthetic transport-commit model is superseded.

Local Review approval integrates the exact pinned reviewed commit into the current Project target under the Project lock using Git merge semantics. Conflicts fail explicitly and leave the target clean at its prior revision.

Local repository paths remain validated against deployment-authorized roots. Project configuration must not become arbitrary filesystem access.

### Remote Git source edge

A remote Git Project does not transfer a server filesystem checkout to the Runner. The Runner maintains a bare cache per repository and per-Run worktrees.

Fetched source refs live under:

```text
refs/remotes/origin/*
```

Agent Board Issue branches live under:

```text
refs/heads/agent-board/*
```

Cache mutations are serialized per repository; unrelated repository caches remain independent. Distinct Issues use distinct branches/worktrees and cannot silently reset or rebase one another.

The Runner uses host Git authentication only. No provider API or Source Connection abstraction is introduced by this execution path.

Issue publication uses a normal non-force push. The Runner retains the worktree until the server durably records the published revision and sends acknowledgement. If the push succeeded but its response/persistence/ack was lost, the control plane may retry publication on that same retained session; republishing the same finalized HEAD is idempotent. Unexpected external branch advancement is still rejected.

Publishing a remote Issue branch is not remote target integration and is not equivalent to creating/merging a PR/MR.

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

Human continuation intent is durable with the Question/Review decision. Native blocking Questions keep the same live Runner/native Engine session and checkout attached through `WAITING_FOR_INPUT`; scheduler Agent/Model capacity policy is separate from Runner session ownership.

See `scheduler.md`.

## Execution context and secrets

One trusted Go resolver composes safe Project/Issue/Agent/Engine/Model/Provider/Workspace data plus explicit resume/Review context. Runner identity is selected by scheduling, not read from Agent configuration.

Secret material is separate and ephemeral:

```text
encrypted secret/reference
 -> authorize
 -> resolve in trusted Go boundary
 -> inject into assigned Runner Execution Session
 -> redact before every durable sink
```

Untrusted execution code cannot gain control-plane privileges through network reachability or caller-controlled identity. Remote Git authentication is provided by the Runner host's normal Git configuration rather than forwarded provider credentials.

## Events and live updates

```text
Engine / Runner
 -> normalize + redact
 -> persist Event
 -> projections
 -> SSE
```

The browser reconstructs live state from persisted reads plus SSE and is never the sole owner of important activity. Nuxt/Nitro process state is never authoritative for Run state.

## Provenance and Review evidence

Every Run stores immutable safe execution provenance including the selected Runner and resolved Engine/Model/Provider configuration.

Review pins exact Git identity:

```text
base_revision   = Issue base SHA
review_revision = reviewed Issue branch HEAD SHA
```

The source diff is reproducible with `git diff <base_revision>..<review_revision>`. Tests, commands, messages, Artifacts, usage and provenance are evidence about that Git state, not a duplicate source representation.

Request Changes advances the same Issue branch; old Reviews remain immutable because their SHAs remain pinned.

See `execution-evidence.md`.

## Questions and continuation

Blocking Questions move the same Run to `WAITING_FOR_INPUT` and the Issue to `BLOCKED`. `BLOCKED` is both a durable Issue state and its normal Board column projection. Answering may resume the same Run according to Project policy.

For native OpenCode Questions, the same live Runner Execution Session, native Engine session and checkout remain attached while waiting. The answer continues that native session; Agent Board does not finalize or publish/return the Issue branch merely because input is pending. If the Runner/transport actually fails, the server reconciles the durable Execution Session before deciding a terminal outcome or safe recovery.

## Delivery policy

Human Review is the v0.1 delivery gate.

For local Projects, approval integrates the exact pinned Review commit into the current Project Workspace target state.

For remote Git Projects, Issue-branch publication is durable unmerged work, not target integration. Provider PR/MR creation and remote target merging remain future Source Connection/provider-action work.

After authenticated Source Connections and provider actions exist, Projects may explicitly opt into an autonomous PR/MR delivery policy. PR/MR creation does not imply auto-merge or deployment. Stronger delivery permissions require separate explicit policy.

## Future layers

Planning, Automations, Agent-created Issues, Source Connections, delivery automation, delegation, Squads, workers, users/groups and Plugins reuse the same Issue/Agent/Run/scheduler/Workspace/Git-branch model.

Future Worker/Pool selection may supply or place Runner capacity above the existing protocol. Worker identity remains separate from Agent, Runner and Execution Session identity.

Plugins are deliberately late roadmap work.

## Invariants

1. Issue is the durable unit of work.
2. Agent identity is not process identity.
3. Run identity is not Runner or Execution Session identity.
4. Workspace lifetime is independent from Runner transport lifetime.
5. Every Issue has one deterministic `agent-board/<issue-key>` branch; Git commits/branches are the durable code state.
6. One logical writer owns an Issue checkout during an active Runner execution lifecycle.
7. One Runner may execute many Execution Sessions over time; default advertised capacity is 5 concurrent sessions.
8. One Execution Session owns one process tree.
9. Runner placement is scheduler-owned; Agents select Engine and Model Profile, not Runner.
10. PostgreSQL owns durable scheduling/session state and records Git identity; it does not replace Git source history.
11. Browser/request lifetime never owns execution or continuation.
12. Engine processes execute on the selected Runner through `agent-runner`.
13. Engine adapters remain server-side.
14. Runner/server transport is protocol-v2 WebSocket and session-scoped.
15. Shared finalization enforces expected Issue branch, ancestry, conflict safety and clean boundaries for local and remote source paths.
16. Remote Issue publication is normal/non-force and is not remote target integration.
17. Review identity is `base_revision` + `review_revision`; no duplicate Review filesystem snapshot is authoritative.
18. Secrets are ephemeral and redacted before persistence.
19. Historical execution truth comes from immutable provenance plus pinned Git identities.
20. Nuxt is presentation/application delivery, not a competing control plane.
21. `BLOCKED` is an Issue workflow state; Run execution states remain separate.
22. Human Review is the default v0.1 delivery gate; later autonomous delivery requires explicit Project policy.

Earlier #15 snapshot/staging assumptions are superseded where they conflict with these Git-native invariants.
