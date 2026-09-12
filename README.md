# Agent Board

Agent Board is a genuinely free and open-source, self-hosted work board for autonomous software agents.

It is built around durable Issues, server-owned scheduling, connected Runners, persistent Git-native Issue state, structured human input, complete execution evidence, and configurable delivery policy.

## Priority: reach the complete v0.1 flow first

The near-term milestone is one complete real coding-agent flow:

```text
Project source (local or git)
  -> Issue
  -> Agent
       -> Engine
       -> Model Profile -> Provider
  -> durable scheduler
  -> selected connected Runner
  -> deterministic agent-board/<issue-key> branch
  -> source-aware checkout/worktree
  -> Execution Session
  -> coding Engine
  -> commands / Git changes / tests / Artifacts
  -> Question / resume when needed
  -> Review pinned to Git SHAs
  -> human approval
  -> Done / delivery according to source semantics
```

Work that does not directly prove or harden this flow must not delay it.

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

### Runners

Execution happens on a connected `agent-runner`, never directly inside the trusted Go backend process. The scheduler selects an eligible Runner and binds each Execution Session directly to that Runner.

External Runner hosts are persistent user-managed Linux machines. They connect outbound to Agent Board over protocol v2 and may have any supported coding CLIs installed. See [`docs/external-runner-setup.md`](./docs/external-runner-setup.md).

Agent Board can also supervise an internal Runner using the same Runner protocol and Execution Session path. Project policy decides whether that internal Runner is eligible.

Agent Board never sends Docker daemon credentials or a Docker socket through the Runner protocol. External Runner hosts own their host-level tooling and security policy.

### Provider credentials for OpenCode

OpenCode Runs require encrypted Provider credential storage. With the default Compose setup, the deployment encryption key and secret-write token are generated automatically on first startup.

When `OPENROUTER_API_KEY` is set in the Compose environment (for example via `.env`), the server creates a global Provider named **OpenRouter** on startup if one does not already exist. If that provider already exists but is missing its stored credential, startup completes that step. An existing provider with a credential is left unchanged. This env var is creation/resume-only; rotate an existing key from **Settings → Providers**. Recreate the server container after adding env vars so Compose injects them.

When `OPENROUTER_MODELS` is set to a comma-separated list of OpenRouter model IDs, the server also creates global Model Profiles linked to the OpenRouter provider on startup if they do not already exist. Each profile uses the full model ID as its model field and a display name derived from the last path segment. Existing profiles with the same name or model are left unchanged. `OPENROUTER_MODELS` requires an existing OpenRouter provider or `OPENROUTER_API_KEY` on the same startup.

Save each Provider credential from **Settings → Providers**:

1. Create or edit a Provider.
2. Expand **Set or replace API key** and paste the Provider API key.
3. Save the Provider.

The API key is encrypted server-side and never shown again after save.

### Create a runnable Agent

1. In **Settings**, create a **Provider** and **Model Profile**.
2. Create and enable an **Agent**, selecting an Engine and Model Profile.
3. Create a **Project** and configure its local repository or remote Git source.
4. Ensure an eligible Runner is connected.
5. Create an Issue and assign the Agent.
6. Agent Board creates/schedules the Run and selects a Runner.
7. Inspect the Run, answer Questions if needed, then Review the result.

The scripted Engine is a deterministic walking skeleton. v0.1 is complete only when a real coding Engine can modify real Project source on a selected Runner and produce trustworthy Review evidence.

## Issue flow

```text
BACKLOG / TODO
      |
      | assign runnable Agent
      v
IN_PROGRESS + QUEUED Run
      |
      v
scheduler -> selected Runner -> Execution Session -> Engine
      |
      +--> success -----------------------> REVIEW
      |                                     |
      |                                     +--> Approve -> DONE
      |                                     +--> Request changes -> new attempt
      |                                                           same Issue branch
      |
      +--> blocking Question ------------> BLOCKED
      |                                     |
      |                                     +--> answer -> resume same Run/session
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
| **Agent** | Durable worker identity/configuration with Engine and Model Profile. |
| **Run** | One execution attempt for an Issue by an Agent. |
| **Provider** | Configured model connection and credentials. |
| **Model Profile** | Reusable model selection/settings and optional capacity. |
| **Runner** | Connected execution host selected by the scheduler. |
| **Workspace / Issue branch** | Durable Git-native source state owned by an Issue. |
| **Execution Session** | One Runner-supervised process-tree execution. |
| **Question** | Structured request for human input. |
| **Decision** | Durable human/product outcome. |
| **Event** | Append-only execution/audit history. |
| **Artifact** | First-class Run output stored outside oversized Event payloads. |

Canonical configuration:

```text
Provider -> Model Profile
Engine + Model Profile -> Agent
```

Runner placement is scheduler-owned; Agents do not select execution hosts.

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
   +--> Runner registry / protocol v2
             |
             v
        agent-runner
             |
             v
      Execution Session
             |
             v
           Engine
```

Runner, Execution Session and Run are separate identities. A Runner may execute many sessions over time, and each session is durably bound to exactly one selected Runner.

The browser never owns long-running execution. PostgreSQL is authoritative for durable state and scheduler/session ownership. Git branches and commits are authoritative for source-tree identity.

### Stack

```text
Frontend         Nuxt 4 + Vue 3 + TypeScript + Tailwind CSS + Nuxt UI v4
Backend          Go
Agent runner     Go
HTTP             chi
Database         PostgreSQL + pgvector
Live updates     Server-Sent Events
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
- Runner eligibility/capacity is scheduler-owned
- each Execution Session has an immutable Runner binding
- one Runner may execute many Execution Sessions over time
- default Runner capacity is 5 concurrent Execution Sessions; the protocol remains capacity-extensible
- Engine adapters remain server-side
- Engine processes execute on the selected Runner through `agent-runner`
- Runner/server transport is versioned protocol-v2 WebSocket
- local Projects transfer the exact Issue branch and import the returned branch before acknowledgement
- remote Git Projects use Runner-side cache/worktrees and normal non-force Issue-branch publication
- secrets are resolved only in trusted code and injected ephemerally
- Events are persisted before live publication
- large raw output and Artifacts use durable output storage
- Runs retain immutable safe execution provenance including selected Runner identity
- Review pins exact Git SHAs and is reproducible from Git history
- human Review is the default v0.1 delivery gate

## Planned product depth

After the complete v0.1 coding flow:

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
pnpm test:coverage
pnpm build
```

Read `AGENTS.md`, [`docs/testing.md`](./docs/testing.md), [`docs/frontend-implementation.md`](./docs/frontend-implementation.md), and [`docs/frontend-theme.md`](./docs/frontend-theme.md) before implementation.

## Documentation

- [`docs/product-v0.1.md`](./docs/product-v0.1.md) — v0.1 product behavior and critical path
- [`docs/roadmap.md`](./docs/roadmap.md) — product ordering
- [`docs/domain-model.md`](./docs/domain-model.md) — durable concepts and invariants
- [`docs/architecture.md`](./docs/architecture.md) — implementation architecture
- [`docs/scheduler.md`](./docs/scheduler.md) — durable scheduling and capacity
- [`docs/source-control.md`](./docs/source-control.md) — local and remote Git source behavior
- [`docs/execution-context.md`](./docs/execution-context.md) — resolved execution context and secrets
- [`docs/execution-evidence.md`](./docs/execution-evidence.md) — provenance, logs, Artifacts and Review evidence
- [`docs/agent-runner.md`](./docs/agent-runner.md) — Runner identity, enrollment, protocol, session lifecycle and Git synchronization
- [`docs/external-runner-setup.md`](./docs/external-runner-setup.md) — external Runner installation and operation
- [`docs/frontend-implementation.md`](./docs/frontend-implementation.md) — clean-room Nuxt/Nuxt UI implementation rules
- [`docs/frontend-theme.md`](./docs/frontend-theme.md) — component and theme rules
- [`docs/automations.md`](./docs/automations.md) — scheduled work and Agent-created Issues
- [`docs/planning.md`](./docs/planning.md) — planning strategy and Plan artifacts
- [`docs/future-agent-collaboration.md`](./docs/future-agent-collaboration.md) — delegation, Squads and worker topology
- [`docs/event-protocol.md`](./docs/event-protocol.md) — Event contract
- [`docs/plugins.md`](./docs/plugins.md) — deliberately late roadmap Plugin architecture

## License

A final license has not yet been selected.

The project goal is genuinely free/open-source self-hosting without artificial agent/runner/seat limits or a crippled community edition.
