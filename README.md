# Agent Board

Agent Board is a genuinely free and open-source, self-hosted work board for autonomous software agents.

It is built around durable Issues, server-owned execution, persistent repository-backed Workspaces, Runner-based execution, structured human input, complete execution evidence, and configurable delivery policy.

## Priority: reach the complete v0.1 flow first

The near-term milestone is one complete real coding-agent flow using a local Git repository accessible to the backend deployment:

```text
Local Project repository
  -> Issue
  -> Agent
       -> Engine
       -> Model Profile -> Provider
  -> durable scheduler
  -> selected connected Runner
  -> durable Issue Workspace transfer/materialization
  -> Execution Session
  -> coding Engine
  -> commands / files / tests / Artifacts
  -> Question / resume when needed
  -> Review
  -> human approval
  -> Done
```

External persistent Runners are preferred for normal execution. The server-managed internal Runner uses the same Runner/Execution Session protocol path. Runtime/Runtime Instance support remains only for legacy internal managed compute and is not normal Agent configuration.

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

### Legacy Docker Runtime compatibility

Normal Runner-based execution does not require users to create or select a Runtime. The repository still contains Docker Runtime/Runtime Instance support for legacy internal managed compute and integration coverage.

Where that legacy path is used, the trusted Go backend may access Docker; Agent Runtime Instances never receive the Docker socket or equivalent daemon credentials. Runtime Workspaces must be visible to the host Docker daemon at the same absolute path used by the server.

### Provider credentials for OpenCode

OpenCode Runs require encrypted Provider credential storage. With the default Compose setup, the deployment encryption key and secret-write token are generated automatically on first startup.

When `OPENROUTER_API_KEY` is set in the Compose environment (for example via `.env`), the server creates a global Provider named **OpenRouter** on startup if one does not already exist. If that provider already exists but is missing its stored credential, startup completes that step. An existing provider with a credential is left unchanged. This env var is creation/resume-only; rotate an existing key from **Settings → Providers**. Recreate the server container after adding env vars so Compose injects them.

When `OPENROUTER_MODELS` is set to a comma-separated list of OpenRouter model IDs, the server also creates global Model Profiles linked to the OpenRouter provider on startup if they do not already exist. Each profile uses the full model ID as its model field and a display name derived from the last path segment (for example `anthropic/claude-3.5-sonnet` becomes `claude-3.5-sonnet`). Existing profiles with the same name or model are left unchanged. `OPENROUTER_MODELS` requires an existing OpenRouter provider or `OPENROUTER_API_KEY` on the same startup.

When `LITELLM_ENDPOINT` and `LITELLM_API_KEY` are both set, the server creates a global Provider named **LiteLLM** (`openai-compatible`) on startup if one does not already exist, using the endpoint as its base URL. If that provider already exists but is missing its stored credential or base URL, startup completes the missing step. An existing provider with a credential and base URL is left unchanged. These env vars are creation/resume-only; rotate an existing key or change the endpoint from **Settings → Providers**. Recreate the server container after adding env vars so Compose injects them.

Save each Provider credential from **Settings → Providers**:

1. Create or edit a Provider.
2. Expand **Set or replace API key** and paste the Provider API key.
3. Save the Provider.

The API key is encrypted server-side and never shown again after save.

### Create a runnable Agent

1. In **Settings**, create a **Provider** and **Model Profile**.
2. Create and enable an **Agent**, selecting an Engine and Model Profile.
3. Ensure an eligible Runner is connected; the server-managed internal Runner is the fallback when allowed by policy.
4. Create a **Project** and configure its repository/default branch.
5. Create an Issue and assign the Agent.
6. Agent Board creates/reuses the Issue Workspace and schedules a Run onto an eligible Runner.
7. Inspect the Run, answer Questions if needed, then Review the result.

The scripted Engine remains a deterministic walking skeleton used by tests and development. Normal coding work uses a real coding Engine such as OpenCode through the same Runner/Execution Session boundary.

## Issue flow

```text
BACKLOG / TODO
      |
      | assign runnable Agent
      v
IN_PROGRESS + QUEUED Run
      |
      v
scheduler -> Runner -> Workspace materialization -> Execution Session -> Engine
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
| **Agent** | Durable worker identity/configuration, including Engine and Model Profile. |
| **Run** | One execution attempt for an Issue by an Agent. |
| **Provider** | Configured model connection and credentials. |
| **Model Profile** | Reusable model selection/settings and optional capacity. |
| **Runner** | Connected execution host selected by the scheduler. |
| **Workspace** | Durable repository state owned by an Issue. |
| **Execution Session** | One runner-supervised process-tree execution. |
| **Runtime** | Legacy reusable execution policy for internal managed compute only. |
| **Runtime Instance** | Legacy disposable compute materialized from a Runtime and bound to one Workspace. |
| **Question** | Structured request for human input. |
| **Decision** | Durable human/product outcome. |
| **Event** | Append-only execution/audit history. |
| **Artifact** | First-class Run output stored outside oversized Event payloads. |

Canonical Agent configuration:

```text
Provider -> Model Profile
Engine + Model Profile -> Agent
```

Runner placement is scheduler-owned rather than Agent configuration.

## Architecture

```text
Nuxt web
   |
   v
Go backend (apps/server)
   |
   +--> PostgreSQL durable state + scheduling
   +--> blob/output storage
   +--> Workspace/Git services
   +--> Runner selection / protocol-v2 transport
             |
             v
        agent-runner
             |
             v
      Execution Session
             |
             v
           Engine

Legacy compatibility only:
Go backend -> Runtime implementation -> Runtime Instance -> agent-runner
```

Runner, Execution Session and Run are separate identities. Legacy Runtime Instance identity remains separate where the compatibility path is used.

The browser never owns long-running execution. PostgreSQL is authoritative for durable state and scheduler ownership.

### Stack

```text
Frontend         Nuxt 4 + Vue 3 + TypeScript + Tailwind CSS + Nuxt UI v4
Backend          Go
Agent runner     Go
HTTP             chi
Database         PostgreSQL + pgvector
Live updates     Server-Sent Events
Execution        connected agent-runner hosts
Legacy compute   Docker Runtime/Runtime Instance compatibility
Runner transport WebSocket protocol v2
API contracts    OpenAPI + intentional frontend types
Blob storage     local filesystem first; S3-compatible later
```

Nuxt/Nitro is the web application layer, not a second Agent Board control plane. Durable product state, scheduling, execution and authorization remain Go-owned.

## Repository

```text
agent-board/
├── apps/
│   ├── server/
│   ├── agent-runner/
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
- scheduler placement selects an eligible live Runner rather than an Agent-selected Runtime
- one Runner may execute multiple concurrent Execution Sessions up to advertised capacity
- one Execution Session owns one process tree
- external persistent Runners are preferred; the server-managed internal Runner uses the same protocol path
- Engine adapters remain server-side
- Engine processes execute through `agent-runner`
- runner/server transport is versioned WebSocket protocol v2
- secrets are resolved only in trusted code and injected ephemerally
- Events are persisted before live publication
- large raw output and Artifacts use durable output storage
- Runs retain immutable safe execution provenance
- Review shows the complete candidate, including staged and new/untracked files
- human Review is the default v0.1 delivery gate
- legacy Runtime/Runtime Instance containment remains isolated compatibility behavior and is not exposed as normal Agent settings

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
cd apps/server
go test ./...
go vet ./...
go build ./...
```

Runner:

```bash
cd apps/agent-runner
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
- [`docs/runtime-contract.md`](./docs/runtime-contract.md) — legacy/internal Runtime boundary and security
- [`docs/runtime-execution.md`](./docs/runtime-execution.md) — Runner-first Engine execution with legacy Runtime compatibility
- [`docs/agent-runner.md`](./docs/agent-runner.md) — runner identity, WebSocket protocol, session lifecycle and Workspace transfer
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
