# Agent Board agent/developer guide

Agent Board is a genuinely free/open-source, self-hosted work board for autonomous software agents.

## Read first

For implementation work, read the relevant canonical docs:

- `docs/product-v0.1.md` — fastest path to the usable v0.1 product flow
- `docs/roadmap.md` — current/planned/later ordering
- `docs/domain-model.md` — durable concepts/invariants
- `docs/architecture.md` — Go implementation architecture
- `docs/scheduler.md` — durable scheduling/capacity
- `docs/source-control.md` — repositories and Source Connections
- `docs/execution-context.md` — canonical execution context/secrets
- `docs/execution-evidence.md` — provenance/logs/Artifacts/Review evidence
- `docs/runtime-contract.md` — Runtime boundary/security
- `docs/runtime-execution.md` — Runtime/runner Engine execution
- `docs/agent-runner.md` — runner identity, WebSocket/session contract and same-Workspace reuse
- `docs/event-protocol.md` — Event contract
- `docs/testing.md` — mandatory TDD workflow
- `docs/frontend-implementation.md` — clean-room Nuxt/Nuxt UI implementation rules
- `docs/frontend-theme.md` — frontend composition/theme rules

GitHub issues track implementation work. Canonical docs define durable product behavior.

## Highest priority

Reach the complete v0.1 coding flow as quickly as possible:

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
 -> agent-runner
 -> Execution Session
 -> real coding Engine
 -> durable execution evidence
 -> Question/resume when needed
 -> Review
 -> human approval
```

Do not let roadmap features delay this path.

## Planning and clarification questions

During a non-trivial planning phase, do not jump directly from the request to a detailed implementation plan. First identify the small number of product or architecture decisions that materially affect the result.

Before finalizing the plan, ask concise clarification questions when those decisions are not already settled by the issue or canonical docs.

Rules:

- normally ask **1–3 questions in one batch**
- prefer concrete choices or multiple-choice options when useful, and include a recommended default
- focus on decisions that affect scope, UX, domain behavior, API/contracts, persistence, security or architecture
- keep each question short; explain why it matters only when that is not obvious
- do not ask the user to decide low-level implementation details that can be resolved from the codebase, tests or official documentation
- do not recursively drill deeper unless an answer exposes a genuinely important ambiguity
- do not re-ask questions already answered by canonical docs, the issue or prior explicit decisions
- for low-impact/reversible ambiguity, choose a reasonable default and state it in the plan instead of blocking
- questions should help the user make product decisions, not transfer implementation work back to them
- if there is no material ambiguity, proceed without inventing questions just to satisfy the process

After the answers, finalize the plan and proceed. Avoid extended question trees or endless planning loops.

## Product invariants

- Multi-project isolation is mandatory.
- Issue is the durable unit of work.
- Agent is durable configuration, not a process/container.
- Run is a durable execution attempt, not a Runtime Instance, runner or Execution Session.
- Workspace survives Runtime Instances and is reused per Issue.
- A Runtime Instance is bound to exactly one Workspace for its lifetime.
- Runtime Instance, `agent-runner`, Execution Session and Run are separate identities.
- One runner may execute many Execution Sessions over time against its bound Workspace.
- One Execution Session owns one process tree.
- v0.1 allows one active Execution Session per runner; the protocol must remain extensible for more.
- PostgreSQL is authoritative for structured state and scheduler ownership.
- Browser/request lifetime never owns execution or continuation.
- Engine adapters remain server-side; `agent-runner` stays Engine-neutral.
- Engine processes execute inside the selected Runtime Instance through `agent-runner`.
- Server/runner transport is explicitly versioned WebSocket.
- Model inference is independent from Runtime compute.
- Executor Profile references Runtime directly.
- Runtime owns the complete execution environment/policy configuration.
- Agent concurrency and Model Profile capacity are scheduler constraints.
- Questions and Decisions are first-class durable objects.
- Events are append-only and persist-before-publish.
- Secrets are ephemeral and redacted before every durable sink.
- Run history truth comes from immutable execution provenance/evidence.
- `BLOCKED` is a durable Issue state and its normal Board column projection.
- Human Review is the default v0.1 delivery gate.

Canonical configuration:

```text
Provider -> Model Profile
Engine + Model Profile + Runtime -> Executor Profile
Agent -> Executor Profile
```

Board workflow:

```text
BACKLOG -> TODO -> IN_PROGRESS -> BLOCKED -> REVIEW -> DONE
```

Board state is separate from Run state. Capacity-only waiting remains queued and is not a board blocker.

## Stack

- Backend: Go (`apps/server`)
- Agent runner: Go (`apps/agent-runner`)
- HTTP: chi
- Runner transport: WebSocket
- Frontend: Nuxt 4 + Vue 3 + TypeScript + Tailwind CSS + Nuxt UI v4
- Database: PostgreSQL + pgvector
- API contracts: OpenAPI + intentional public DTOs/frontend types
- Live updates: SSE
- Runtime: Docker first
- Blob/output storage: local filesystem first, S3-compatible later

The Go backend is the production control plane. `agent-runner` is a narrow execution-plane binary inside Runtime Instances. Nuxt handles web rendering, routing and browser interaction; durable product state, scheduling, execution authorization and evidence remain Go-server-owned.

## Mandatory TDD

Read `docs/testing.md`. Work Red -> Green -> Refactor. Bug fixes start with a regression test.

**Test coverage is a delivery gate:** affected executable apps/modules must maintain at least **85% automated test coverage**, with **90%+ as the normal target**. Work below 85% is not complete, review-ready, or merge-ready. Do not game the threshold with trivial tests or unjustified exclusions, and do not treat numeric coverage as a substitute for meaningful regression, security, isolation, concurrency, persistence, and integration tests.

Backend verification includes as applicable:

```bash
cd apps/server
go test ./...
go vet ./...
go build ./...
```

Runner verification includes as applicable once implemented:

```bash
cd apps/agent-runner
go test ./...
go vet ./...
go build ./...
```

Frontend changes run typecheck/tests/build as applicable.

## Backend rules

- Keep HTTP adapters separate from application/domain/store/runtime logic.
- Every Project-scoped operation verifies ownership; IDs are not authorization.
- Use explicit runtime-validated request/response contracts.
- PostgreSQL owns durable scheduling state; process-local semaphores/maps are not authoritative.
- Human decisions requiring continuation persist that continuation durably before success returns.
- Do not create parallel schedulers, Run lifecycles, Workspaces or Engine-owned authoritative state.
- Do not introduce a Runtime Profile domain/API/persistence/UI layer; Executor Profiles select Runtime directly.
- Keep Engine adapters server-side and independent from raw runner WebSocket framing.
- Keep `agent-runner` Engine-neutral and free of PostgreSQL, scheduler, Review and control-plane authorization logic.

## Database

`packages/database/schema.sql` is the one canonical pre-release schema. Executor Profile persists `runtime_id` directly. Recreate development databases when incompatible schema changes require it.

## Source control / Workspace

The first v0.1 repository source is a local Git repository accessible to the trusted backend. Project stores the local source/path and base/default branch.

Validate local repository paths against deployment-authorized roots. Project configuration must not become arbitrary filesystem access.

Normal Run orchestration materializes or reuses the Issue Workspace from that repository. One authoritative Workspace exists per Issue in v0.1.

A Runtime Instance mounts exactly one Workspace for its lifetime. It may be reused for later sessions/Runs only against that same Workspace. Cross-Workspace Runtime Instance reuse is not allowed in v0.1.

Authenticated GitHub/GitLab/Bitbucket/Forgejo Source Connections are later work and must not block the first local-repository v0.1 flow. When implemented, credentials remain ephemeral and never appear in durable clone URLs or Git configuration.

## Runtime and security

Agent-executed code is untrusted.

```text
Executor Profile
 -> Runtime
 -> validated Runtime Spec
 -> Runtime implementation
 -> Runtime Instance
 -> agent-runner
 -> Execution Session
 -> Engine
```

Official v0.1 Runtime images contain `agent-runner`. The server and runner use a versioned WebSocket protocol. Runner connection/session state does not become authoritative durable product state.

Never execute coding-agent CLIs directly in the trusted backend process. Never mount the Docker socket into Agent Runtime Instances. Never give the runner PostgreSQL credentials, server encryption keys or broad control-plane credentials.

Control-plane authorization derives trusted actor identity; caller-controlled headers cannot grant human/admin privileges.

## Events and execution evidence

Persist important activity before live publication. Do not store hidden chain-of-thought.

Large output belongs in durable raw-output/blob storage; Artifacts are first-class records. Run provenance records the selected Runtime directly. Review/Run inspection shows complete candidate evidence, including staged and new/untracked files where applicable.

Runner output is normalized/redacted before durable persistence. A WebSocket disconnect is an infrastructure signal and is not, by itself, durable proof that a Run failed.

## Questions and Review

Blocking Questions pause the same Run and may release its Runtime Instance while preserving Workspace state. The Issue moves to the `BLOCKED` state/column when human input is required according to workflow policy.

Resume state is server-owned. A later continuation may reuse a healthy same-Workspace Runtime Instance or create a replacement Runtime Instance against the same durable Workspace.

Human Review is the v0.1 shipping gate.

After Source Connections/provider actions exist, Projects may explicitly opt into autonomous PR/MR delivery. That opt-in may create/update a PR/MR after successful verification without Agent Board internal approval. It does not imply auto-merge, deploy, release, or bypassing provider branch protection.

## Frontend clean-room implementation

`apps/web` is implemented from Agent Board's canonical docs/contracts/tests plus official Nuxt 4, Vue 3, TypeScript, Tailwind CSS and Nuxt UI v4 documentation.

Read `docs/frontend-implementation.md` before frontend work.

## Mandatory Nuxt UI-first behavior

Nuxt UI is the required generic component foundation.

Before creating a generic UI primitive or interaction pattern, every frontend agent must:

1. inspect existing `apps/web` shared components/composables and Nuxt configuration
2. consult `https://nuxt.com/llms.txt` for unfamiliar Nuxt behavior
3. consult the current Nuxt UI component catalog
4. use the configured Nuxt UI MCP server for component search, props, slots, events, docs and examples
5. compose Agent Board product UI from Nuxt UI components
6. create a custom low-level primitive only when no suitable Nuxt UI component/composition exists

Do not recreate buttons, forms, overlays, dropdowns, tabs, tables, dashboard shells, empty/loading states, color-mode controls or similar generic UI when Nuxt UI provides them.

A custom primitive requires an implementation/PR note listing the Nuxt UI components/compositions checked, why they do not fit, and the accessibility/interaction behavior provided.

The project configures the Nuxt UI MCP server in `.cursor/mcp.json` at `https://ui.nuxt.com/mcp`. Use it instead of guessing component APIs.

## Frontend architecture

- Nuxt/Nitro is presentation/application delivery, not the Agent Board control plane.
- Nitro server routes, server middleware, process memory and framework caches do not own Agent Board domain state.
- Product mutations and authoritative durable reads go through the Go API unless a narrow delivery-only exception is explicitly documented.
- Live Run/Issue state reconciles against durable API/SSE state.
- Prefer Nuxt UI components and semantic colors/theme configuration over duplicated custom primitives and raw palette classes.
- Use `app/app.config.ts` for appropriate shared Nuxt UI defaults/theme configuration.
- Dark mode is default; light mode remains complete.
- Preserve keyboard/focus/accessibility behavior provided by Nuxt UI/Reka UI.

## Planned features

Planning strategy, Automations, Agent-created Issues, Source Connections, delivery automation, delegation, Squads and worker pools reuse the canonical Issue/Run/scheduler/Workspace model.

Future Worker Pools supply compute capacity but do not replace Agent, Runtime Instance, runner or Execution Session identities.

Users, groups, roles/permissions and broader multi-user administration are expected later but are not yet designed; do not invent their product contracts.

## Plugins

Plugins are deliberately late roadmap work. They come after the complete v0.1 flow and, unless explicitly reprioritized, after foundational users/groups/permissions work.

Plugin expansion must not take priority over the v0.1 execution path.

## Scope discipline

Do not silently invent product behavior. If canonical docs and the relevant issue leave a material ambiguity unresolved, continue independent work and surface the ambiguity rather than creating a new architecture by accident.
