# Agent Board

Agent Board is a genuinely free and open-source, self-hosted work board for autonomous software agents.

It is built around durable Issues, server-owned execution, isolated Runtimes, persistent repository-backed Workspaces, structured human input, complete execution evidence, and configurable delivery policy.

## Priority: reach the complete v0.1 flow first

The near-term milestone is one complete real coding-agent flow using a local Git repository accessible to the backend deployment:

```text
Local Project repository
  -> Issue
  -> Agent
  -> Executor Profile
       -> Engine
       -> Model Profile -> Provider
       -> Runtime
  -> durable scheduler
  -> durable Issue Workspace
  -> Runtime Instance
  -> coding Engine
  -> commands / files / tests / Artifacts
  -> Question / resume when needed
  -> Review
  -> human approval
  -> Done
```

Work that does not directly prove or harden this flow must not delay it.

Authenticated remote Source Connections are added after the first complete local-repository flow is proven.

## Getting started

```bash
git clone https://github.com/brantje/agent-board.git
cd agent-board
docker compose up --build
```

Open:

- Web UI: `http://localhost:3000`
- API: `http://localhost:3001`

On a fresh PostgreSQL volume, Compose initializes `packages/database/schema.sql`.

### Docker Runtime prerequisites

The v0.1 self-hosted deployment uses the host Docker daemon for Runtime Instances. The trusted Go backend may access Docker; Agent Runtime Instances never receive the Docker socket or equivalent daemon credentials.

Runtime Workspaces must be visible to the host Docker daemon at the same absolute path used by the server. The default Compose setup uses `/var/lib/agent-board/workspaces`.

### Create a runnable Agent

1. In **Settings**, create a **Provider** and **Model Profile**.
2. Create a **Runtime**.
3. Create an **Executor Profile** by selecting an Engine, Model Profile, and Runtime.
4. Create and enable an **Agent** using that Executor Profile.
5. Create a **Project** and configure its local Git repository/default branch.
6. Create an Issue and assign the Agent.
7. Agent Board creates/reuses the Issue Workspace and schedules a Run.
8. Inspect the Run, answer Questions if needed, then Review the result.

The scripted Engine is a deterministic walking skeleton. v0.1 is complete only when a real coding Engine can modify a real Project repository inside the selected Runtime and produce trustworthy Review evidence.

## Issue flow

```text
BACKLOG / TODO
      |
      | assign runnable Agent
      v
IN_PROGRESS + QUEUED Run
      |
      v
scheduler -> Workspace -> Runtime -> Engine
      |
      +--> success -----------------------> REVIEW
      |                                     |
      |                                     +--> Approve -> DONE
      |                                     +--> Request changes -> new attempt
      |                                                           same Workspace
      |
      +--> blocking Question ------------> BLOCKED
      |                                     |
      |                                     +--> answer -> resume same Run
      |
      +--> execution failure ------------> BLOCKED / retry
      |
      +--> capacity wait ----------------> stays queued
```

`BLOCKED` is a durable Issue state and its normal Board column. Run execution states remain separate from Board workflow states.

## Core product model

| Concept | Meaning |
| --- | --- |
| **Project / Board** | Top-level work and repository boundary. |
| **Issue** | Durable unit of work. |
| **Agent** | Durable worker identity/configuration. |
| **Run** | One execution attempt for an Issue by an Agent. |
| **Provider** | Configured model connection and credentials. |
| **Model Profile** | Reusable model selection/settings and optional capacity. |
| **Runtime** | Reusable execution environment and complete execution policy. |
| **Executor Profile** | Engine + Model Profile + Runtime. |
| **Workspace** | Durable repository state owned by an Issue. |
| **Runtime Instance** | Disposable compute materialized from Runtime. |
| **Question** | Structured request for human input. |
| **Decision** | Durable human/product outcome. |
| **Event** | Append-only execution/audit history. |
| **Artifact** | First-class Run output stored outside oversized Event payloads. |

Canonical configuration:

```text
Provider -> Model Profile
Engine + Model Profile + Runtime -> Executor Profile
Agent -> Executor Profile
```

## Architecture

```text
Nuxt web
   |
   v
Go backend (apps/server-go)
   |
   +--> PostgreSQL durable state + scheduling
   +--> blob/output storage
   +--> Workspace/Git services
   +--> Runtime implementations
             |
             v
        Runtime Instance
             |
             v
           Engine
```

The browser never owns long-running execution. PostgreSQL is authoritative for durable state and scheduler ownership.

### Stack

```text
Frontend        Nuxt 4 + Vue 3 + TypeScript + Tailwind CSS + Nuxt UI v4
Backend         Go
HTTP            chi
Database        PostgreSQL + pgvector
Live updates    Server-Sent Events
Runtime         Docker first
API contracts   OpenAPI + intentional frontend types
Blob storage    local filesystem first; S3-compatible later
```

Nuxt/Nitro is the web application layer, not a second Agent Board control plane. Durable product state, scheduling, execution and authorization remain Go-owned.

## Repository

```text
agent-board/
├── apps/
│   ├── server-go/
│   └── web/             # Nuxt application
├── packages/
│   └── database/
├── docs/
├── examples/
├── AGENTS.md
└── README.md
```

## Execution guarantees

- request/browser lifetime does not own execution
- scheduler claims are race-safe and restart-safe
- Model Profile capacity and Agent concurrency are scheduler constraints
- one durable Workspace is reused across Issue attempts
- Executor Profile selects Runtime directly
- Engine processes execute inside the selected Runtime Instance
- secrets are resolved only in trusted code and injected ephemerally
- Events are persisted before live publication
- large raw output and Artifacts use durable output storage
- Runs retain immutable safe execution provenance
- Review shows the complete candidate, including staged and new/untracked files
- human Review is the default v0.1 delivery gate

## Planned product depth

After the complete local-repository v0.1 coding flow:

- planning strategy (`Auto`, `Always plan`, `Skip planning`)
- scheduled Project Automations
- Agent-created follow-up Issues
- authenticated GitHub/GitLab/Bitbucket/Forgejo Source Connections
- explicit Project delivery policy, including optional autonomous PR/MR creation
- Agent delegation
- Squads
- worker pools / warm / spot execution
- users, groups, roles/permissions and broader administration
- additional integrations and account-level capabilities
- Plugins and plugin ecosystem work **last**

Autonomous PR/MR delivery does not imply auto-merge or deployment; stronger delivery permissions require separate explicit policy.

See [`docs/roadmap.md`](./docs/roadmap.md).

## Development

Backend:

```bash
cd apps/server-go
go test ./...
go vet ./...
go build ./...
```

Frontend:

```bash
pnpm install
pnpm typecheck
pnpm test
pnpm build
```

Read `AGENTS.md`, [`docs/testing.md`](./docs/testing.md), [`docs/frontend-implementation.md`](./docs/frontend-implementation.md), and [`docs/frontend-theme.md`](./docs/frontend-theme.md) before implementation.

## Documentation

- [`docs/product-v0.1.md`](./docs/product-v0.1.md) — v0.1 product behavior and critical path
- [`docs/roadmap.md`](./docs/roadmap.md) — product ordering
- [`docs/domain-model.md`](./docs/domain-model.md) — durable concepts and invariants
- [`docs/architecture.md`](./docs/architecture.md) — implementation architecture
- [`docs/scheduler.md`](./docs/scheduler.md) — durable scheduling and capacity
- [`docs/source-control.md`](./docs/source-control.md) — local v0.1 repositories and later Source Connections
- [`docs/execution-context.md`](./docs/execution-context.md) — resolved execution context and secrets
- [`docs/execution-evidence.md`](./docs/execution-evidence.md) — provenance, logs, Artifacts and Review evidence
- [`docs/runtime-contract.md`](./docs/runtime-contract.md) — Runtime boundary and security
- [`docs/frontend-implementation.md`](./docs/frontend-implementation.md) — clean-room Nuxt/Nuxt UI implementation rules
- [`docs/frontend-theme.md`](./docs/frontend-theme.md) — component and theme rules
- [`docs/automations.md`](./docs/automations.md) — scheduled work and Agent-created Issues
- [`docs/planning.md`](./docs/planning.md) — planning strategy and Plan artifacts
- [`docs/future-agent-collaboration.md`](./docs/future-agent-collaboration.md) — delegation, Squads and worker topology
- [`docs/event-protocol.md`](./docs/event-protocol.md) — Event contract
- [`docs/plugins.md`](./docs/plugins.md) — deliberately late roadmap Plugin architecture

## License

A final license has not yet been selected.

The project goal is genuinely free/open-source self-hosting without artificial agent/runtime/seat limits or a crippled community edition.
