# Agent Board

Agent Board is a self-hosted work board and execution control plane for autonomous software agents.

It turns Issues into durable coding work: Agents execute against real Git repositories on connected Runners, can ask humans for input, produce inspectable execution evidence, collaborate through Squads and delegation, and hand completed work back for Review.

```text
Project
  -> Issue
  -> Agent / Squad
  -> Run
  -> Scheduler
  -> Runner
  -> Execution Session
  -> Engine
  -> Git changes
  -> Review
```

Agent Board is built so execution belongs to the server, not the browser. Runs, Workspaces, Questions, Reviews, scheduling state and execution evidence survive browser disconnects and process restarts.

> **Status:** Agent Board is under active development. The goal is a genuinely free and open-source multitasking platform without artificial Agent, Runner or seat limits. A final open-source license has not yet been selected.

## What Agent Board does

### Durable agent execution

Issues can be assigned to Agents and executed asynchronously through the durable scheduler.

Agents are configured from:

```text
Provider -> Model Profile
Engine + Model Profile -> Agent
```

The scheduler chooses where work executes. Agents do not select individual Runners.

Runs have durable lifecycle state, capacity admission, execution provenance, logs, Artifacts and Git evidence.

### Real Git workspaces

Projects can use:

- a local Git repository available to Agent Board; or
- a remote Git repository URL.

Each Issue gets its own durable branch:

```text
agent-board/<issue-key>
```

The same Issue branch continues across attempts, questions, reviews and requested changes.

Execution happens against that Git state rather than against a synthetic copy of the candidate changes.

### Runners

Agent code executes on `agent-runner`, not inside the trusted server process.

Agent Board supports persistent connected Runners and scheduler-controlled placement. Runners connect outbound to the server and may execute multiple isolated Execution Sessions up to their advertised capacity.

Projects can restrict which Runners may execute their work.

A server-managed internal Runner is available as a fallback when allowed by Project policy.

### Questions and human input

Agents can ask structured blocking Questions during execution.

A blocking Question pauses the Run without implicitly changing the Issue's Board status.

For Engines that support native continuation, such as OpenCode, answering a Question resumes the same live Engine session instead of starting the task over.

### Review

Completed coding Runs produce inspectable Git and execution evidence.

Reviews are pinned to exact Git revisions, so the reviewed change remains reproducible even when later attempts continue the Issue branch.

Review can include:

- Git diff and revisions
- tests and checks
- execution timeline
- commands and tool activity
- raw output
- Artifacts
- Runner and Engine provenance

Human Review is the default delivery gate.

### Users and Project access

Agent Board has built-in human authentication and Project authorization.

Deployment roles:

```text
admin
member
```

Project roles:

```text
admin
member
viewer
```

Projects are private by default.

Groups can grant Project access to Users, while Project authorization remains enforced by the Go backend rather than by frontend visibility.

### Squads

Squads are reusable Project-scoped collaboration configurations.

```text
Squad
├── leader Agent
└── members
    ├── Agent
    └── User
```

A Squad can own an Issue while its current leader Agent performs execution.

Additional Agent or User members provide collaboration context; they do not implicitly create Runs or receive Project permissions.

Changing a Squad leader changes who executes future work without rewriting Issue ownership.

### Agent delegation

Agents can optionally be allowed to delegate bounded subtasks to other Agents.

```text
Parent Run
   |
   +--> delegate task
           |
           v
      delegated Run
           |
           v
      normal scheduler
```

Delegation deliberately reuses the normal Agent Board execution model:

- ordinary Runs
- the existing scheduler
- normal Agent and Model capacity
- normal Runner selection
- the Issue Workspace
- normal execution evidence

There is no separate delegation scheduler, queue or Run type.

Canonical delegation provides durable parent/child lineage, explicit delegation policy, and serialized Workspace handoff on the existing Issue Workspace and branch. Accepting a delegation request creates the ordinary child Run but keeps its START job held. After the matching native `delegate_task` completes, the parent uses the normal Workspace/Git hand-back path and yields scheduler ownership; only then does the existing child become eligible for ordinary execution. Delegate changes return through the same Workspace/Git path, so later normal execution for the Issue sees the updated authoritative branch.

Delegated outcome/result handling, automatic parent continuation, parent/child cancellation propagation, and Squad-aware delegation remain later lifecycle work. Delegation does not create parallel Workspaces, delegation branches, or merge/rebase orchestration.

### HTTP API and MCP

The Go server exposes the Agent Board API and an MCP endpoint at:

```text
https://<agent-board-server>/mcp
```

MCP is another transport over the same application/domain behavior used by the normal API. It does not implement a second Issue, Run or authorization model.

See [`docs/mcp.md`](./docs/mcp.md).

## Getting started

### Requirements

For the default deployment you need:

- Docker
- Docker Compose
- Git

For real model-backed coding Runs you also need a configured model Provider and an Engine such as OpenCode.

### Start Agent Board

```bash
git clone https://github.com/brantje/agent-board.git
cd agent-board
docker compose up --build
```

Then open:

- Web UI: http://localhost:3000
- API: http://localhost:3001

On a new deployment, the first registered User becomes the deployment administrator.

### Configure your first Agent

In the UI:

1. Create a **Provider**.
2. Create a **Model Profile** for that Provider.
3. Create and enable an **Agent** with an Engine and Model Profile.
4. Create a **Project** and configure its Git repository.
5. Ensure an eligible Runner is available.
6. Create an Issue and assign the Agent.
7. Inspect the resulting Run and answer any Questions.
8. Review the resulting Git changes.

The scheduler handles execution placement and capacity automatically.

## Provider bootstrap

Providers can be configured through the UI.

For local deployments, OpenRouter and LiteLLM can also be bootstrapped through environment variables.

### OpenRouter

Add to `.env`:

```bash
OPENROUTER_API_KEY=...
OPENROUTER_MODELS=anthropic/claude-sonnet-4-5,openai/gpt-5
```

On startup Agent Board can create the OpenRouter Provider and the configured Model Profiles if they do not already exist.

### LiteLLM

```bash
LITELLM_ENDPOINT=http://your-litellm-host:4000
LITELLM_API_KEY=...
LITELLM_MODELS=anthropic/claude-sonnet-4-5,openai/gpt-5
```

Existing credentials are not silently rotated by changing these variables. Manage existing Provider credentials through **Settings → Providers**.

See [`.env.example`](./.env.example) for the available deployment settings.

## Core concepts

| Concept | Purpose |
| --- | --- |
| **Project** | Board, repository and execution-policy boundary |
| **Issue** | Durable unit of work |
| **Agent** | Worker identity configured with an Engine and Model Profile |
| **Squad** | Project-scoped Agent/User collaboration configuration |
| **Run** | One execution attempt for an Issue |
| **Delegation** | Parent/child lineage for a bounded delegated task |
| **Provider** | Model provider connection and credentials |
| **Model Profile** | Reusable model configuration and capacity |
| **Runner** | Connected execution host |
| **Workspace** | Durable Git state associated with an Issue |
| **Execution Session** | One Runner-supervised Engine process tree |
| **Question** | Structured request for human input |
| **Review** | Human inspection and delivery decision |
| **Event** | Append-only execution/activity history |
| **Artifact** | Durable output produced by a Run |

## Architecture

```text
                    ┌──────────────┐
                    │   Nuxt Web   │
                    └──────┬───────┘
                           │
                    HTTP / SSE
                           │
                           v
┌──────────────┐    ┌──────────────────────┐
│ MCP clients  │───>│      Go server       │
└──────────────┘    │                      │
                    │  domain/application  │
                    │  auth                │
                    │  scheduler           │
                    │  Git/Workspace       │
                    │  Review/evidence     │
                    └───────┬──────────────┘
                            │
             ┌──────────────┼──────────────┐
             │              │              │
             v              v              v
       PostgreSQL      Blob/output     Runner transport
                                            │
                                            v
                                      agent-runner
                                            │
                                            v
                                   Execution Session
                                            │
                                            v
                                          Engine
                                            │
                                            v
                                           Git
```

The Go backend is the control plane.

PostgreSQL is authoritative for durable state and scheduling. Nuxt is the web application layer, not a second backend control plane.

`agent-runner` is intentionally narrow and Engine-neutral. It does not own scheduler, Review or database state.

## Stack

| Area | Technology |
| --- | --- |
| Frontend | Nuxt 4, Vue 3, TypeScript, Tailwind CSS, Nuxt UI v4 |
| Backend | Go |
| Runner | Go |
| HTTP | chi |
| Database | PostgreSQL + pgvector |
| Live updates | Server-Sent Events |
| Runner transport | WebSocket protocol v2 |
| Agent execution | `agent-runner` |
| API contracts | OpenAPI |
| MCP | Streamable HTTP |
| Output storage | Local filesystem |

Legacy Docker Runtime/Runtime Instance support remains in the backend for existing managed-compute compatibility, but it is not part of normal Agent configuration or the preferred execution path.

## Repository layout

```text
agent-board/
├── apps/
│   ├── server/          # Go control plane
│   ├── agent-runner/    # execution-plane Runner
│   └── web/             # Nuxt application
├── packages/
│   └── database/        # canonical pre-release schema
├── docs/
├── examples/
├── compose.yaml
├── AGENTS.md
└── README.md
```

## Development

### Backend

```bash
cd apps/server
go test ./...
go vet ./...
go build ./...
```

### Runner

```bash
cd apps/agent-runner
go test ./...
go vet ./...
go build ./...
```

### Frontend

From the repository root:

```bash
pnpm install
pnpm typecheck
pnpm test
pnpm build
```

For local development with hot reload:

```bash
docker compose -f compose.yaml -f docker-compose.dev.yml up --build
```

See [`AGENTS.md`](./AGENTS.md) and [`docs/testing.md`](./docs/testing.md) before making implementation changes.

## Documentation

The README intentionally stays high-level. Product behavior and architectural invariants belong in the canonical documentation.

Start with:

- [`docs/product-v0.1.md`](./docs/product-v0.1.md) — product behavior
- [`docs/architecture.md`](./docs/architecture.md) — control-plane and execution architecture
- [`docs/domain-model.md`](./docs/domain-model.md) — durable concepts and invariants
- [`docs/scheduler.md`](./docs/scheduler.md) — scheduling and capacity
- [`docs/source-control.md`](./docs/source-control.md) — repository and Git behavior
- [`docs/source-providers.md`](./docs/source-providers.md) — GitHub, GitLab and Forgejo connections, PR/MR state and delivery
- [`docs/agent-runner.md`](./docs/agent-runner.md) — Runner protocol and execution sessions
- [`docs/execution-evidence.md`](./docs/execution-evidence.md) — logs, provenance and Artifacts
- [`docs/authorization.md`](./docs/authorization.md) — Users, Groups and Project roles
- [`docs/future-agent-collaboration.md`](./docs/future-agent-collaboration.md) — Squads, delegation and collaboration
- [`docs/mcp.md`](./docs/mcp.md) — MCP interface
- [`docs/roadmap.md`](./docs/roadmap.md) — implementation ordering

The complete documentation lives in [`docs/`](./docs/).

## Project principles

Agent Board is built around a few constraints:

- durable product state belongs in the backend;
- browser/request lifetime never owns execution;
- Agents, Runs, Runners and Execution Sessions remain separate identities;
- normal execution goes through the scheduler;
- collaboration reuses the normal Run and Workspace lifecycle;
- Agent-executed code is treated as untrusted;
- secrets remain in trusted infrastructure and are injected only when needed;
- important execution evidence is persisted before publication;
- human Review remains the default delivery gate;
- product behavior is not duplicated between HTTP, MCP, frontend or Runner transports.

## Roadmap

Agent Board is being built incrementally around the existing Issue -> Run -> Runner -> Git -> Review lifecycle.

Current collaboration work builds from:

```text
Squads [implemented]
  -> canonical delegation [implemented]
  -> serialized delegated Workspace handoff [implemented]
  -> delegated outcomes / parent continuation [later]
  -> Squad-aware collaboration [later]
```

After delegated outcomes / parent continuation, GitHub, GitLab and Forgejo are the next major product area as Tier-1 Source Providers: repository discovery, ephemeral Git credentials, webhooks, PR/MR state, CI/mergeability and provider-aware delivery all build on the same Git-native core. Planning, Automations, worker pools and broader integrations continue from there without introducing parallel execution systems.

See [`docs/roadmap.md`](./docs/roadmap.md) for current ordering.

## Contributing

Contributions should preserve the existing domain and execution boundaries rather than creating transport-specific implementations of product behavior.

In particular:

- prefer the smallest maintainable change;
- reuse existing domain/application behavior;
- keep HTTP, MCP and Runner transports thin;
- avoid duplicate schedulers, Run lifecycles and Workspace models;
- follow DRY and YAGNI;
- write regression tests for bug fixes;
- maintain the repository's required test coverage.

See [`AGENTS.md`](./AGENTS.md) for the full implementation rules.

## License

A final license has not yet been selected.

The project intends to be genuinely free and open source, without artificial Agent, Runner or seat limits. Selecting and adding the actual open-source license is still required before the repository can make that guarantee legally.
