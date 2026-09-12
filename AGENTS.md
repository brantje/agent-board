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
- `docs/agent-runner.md` — Runner identity, enrollment, protocol-v2 session contract and Git synchronization
- `docs/external-runner-setup.md` — external Runner installation and operation
- `docs/event-protocol.md` — Event contract
- `docs/testing.md` — mandatory TDD workflow
- `docs/frontend-implementation.md` — clean-room Nuxt/Nuxt UI implementation rules
- `docs/frontend-theme.md` — frontend composition/theme rules

GitHub issues track implementation work. Canonical docs define durable product behavior.

## Highest priority

Reach the complete v0.1 coding flow as quickly as possible:

```text
Project source (local or git)
 -> Issue
 -> Agent
      -> Engine
      -> Model Profile -> Provider
 -> durable scheduler
 -> selected connected Runner
 -> deterministic Issue branch
 -> source-aware checkout/worktree
 -> Execution Session
 -> real coding Engine
 -> durable execution evidence
 -> Question/resume when needed
 -> Review pinned to Git SHAs
 -> human approval
```

Do not let roadmap features delay this path.

## Planning and clarification questions

During a non-trivial planning phase, do not jump directly from the request to a detailed implementation plan. First identify the small number of product or architecture decisions that materially affect the result.

Before finalizing the plan, ask concise clarification questions when those decisions are not already settled by the issue or canonical docs.

Rules:

- normally ask **1–10 questions in one batch**
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
- Run is a durable execution attempt, not a Runner or Execution Session.
- Workspace/Git branch state survives Runner transport loss and is reused per Issue.
- Runner, `agent-runner`, Execution Session and Run are separate identities.
- One Runner may execute many Execution Sessions over time; each Runner Execution Session uses its own source-aware checkout/materialization.
- One Execution Session owns one process tree and has an immutable Runner binding.
- Default Runner capacity is 5 concurrent Execution Sessions; the protocol remains extensible beyond that.
- PostgreSQL is authoritative for structured state and scheduler/session ownership.
- Git branches/commits are authoritative for source-tree identity.
- Browser/request lifetime never owns execution or continuation.
- Engine adapters remain server-side; `agent-runner` stays Engine-neutral.
- Engine processes execute on the selected Runner through `agent-runner`.
- Server/Runner transport is explicitly versioned WebSocket (Runner -> server protocol v2).
- Model inference is independent from Runner compute.
- Agents select Engine and Model Profile; the scheduler selects an eligible Runner.
- Agent concurrency, Model Profile capacity and live Runner capacity are scheduler constraints.
- Questions and Decisions are first-class durable objects.
- Events are append-only and persist-before-publish.
- Secrets are ephemeral and redacted before every durable sink.
- Run history truth comes from immutable execution provenance/evidence and pinned Git identity.
- `BLOCKED` is a durable Issue state and its normal Board column projection.
- Human Review is the default v0.1 delivery gate.

Canonical configuration:

```text
Provider -> Model Profile
Engine + Model Profile -> Agent
```

Agents do not configure Runners or other execution-host profiles. Project Runner restrictions are scheduler policy, not Agent configuration.

Board workflow:

```text
BACKLOG -> TODO -> IN_PROGRESS -> BLOCKED -> REVIEW -> DONE
```

Board state is separate from Run state. Capacity-only waiting remains queued and is not a board blocker.

## Stack

- Backend: Go (`apps/server`)
- Agent runner: Go (`apps/agent-runner`)
- HTTP: chi
- Runner transport: WebSocket protocol v2, initiated outbound by the Runner
- Frontend: Nuxt 4 + Vue 3 + TypeScript + Tailwind CSS + Nuxt UI v4
- Database: PostgreSQL + pgvector
- API contracts: OpenAPI + intentional public DTOs/frontend types
- Live updates: SSE
- Blob/output storage: local filesystem first, S3-compatible later

The Go backend is the production control plane. `agent-runner` is the narrow Engine-neutral execution-plane binary running on the scheduler-selected Runner host. External persistent Runners are the preferred production path; the server-managed internal Runner uses the same protocol-v2 path. Nuxt handles web rendering, routing and browser interaction; durable product state, scheduling, execution authorization and evidence remain Go-server-owned.

## Mandatory TDD

Read `docs/testing.md`. Work Red -> Green -> Refactor. Bug fixes start with a regression test.

**Test coverage is a delivery gate:** affected executable apps/modules must maintain at least **85% automated test coverage**, with **90%+ as the normal target**. Work below 85% is not complete, review-ready, or merge-ready. Agents and reviewers must measure coverage with the language/framework coverage tooling for every affected app/module and report the measured percentage before marking work complete. Do not game the threshold with trivial tests or unjustified exclusions, and do not treat numeric coverage as a substitute for meaningful regression, security, isolation, concurrency, persistence, and integration tests.

Backend verification includes as applicable:

```bash
cd apps/server
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
go vet ./...
go build ./...
```

Confirm the reported total coverage is at least 85% before completion; 90%+ remains the target.

Runner verification includes as applicable:

```bash
cd apps/agent-runner
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
go vet ./...
go build ./...
```

Confirm the reported total coverage is at least 85% before completion; 90%+ remains the target.

Frontend verification from the repository root includes:

```bash
pnpm typecheck
pnpm test:coverage
pnpm build
```

`pnpm test:coverage` is the canonical frontend coverage command and is required in CI. It uses the Vitest V8 coverage configuration in `apps/web/vitest.config.ts`, which enforces at least 85% for statements, branches, functions, and lines; 90%+ remains the normal target.

## Backend rules

- Keep HTTP adapters separate from application/domain/store/execution logic.
- Every Project-scoped operation verifies ownership; IDs are not authorization.
- Use explicit validated request/response contracts.
- PostgreSQL owns durable scheduling state; process-local semaphores/maps are not authoritative.
- Human decisions requiring continuation persist that continuation durably before success returns.
- Do not create parallel schedulers, Run lifecycles, Workspaces or Engine-owned authoritative state.
- Do not introduce Executor Profile or Runner Profile domain/API/persistence/UI layers; Agents select Engine and Model Profile, and the scheduler selects Runners.
- Keep Engine adapters server-side and independent from raw Runner WebSocket framing.
- Keep `agent-runner` Engine-neutral and free of PostgreSQL, scheduler, Review and control-plane authorization logic.

## Maintainability: DRY and YAGNI

Write maintainable code. Follow DRY and YAGNI by default.

- Reuse existing logic before adding new code paths.
- If behavior is needed by multiple adapters or interfaces, move it into shared application/domain code and have all callers reuse it.
- Do not duplicate business logic across HTTP, MCP, workers, runners, CLI, frontend server routes or other transports.
- Do not introduce abstractions, services, interfaces, models, extension points or configuration for hypothetical future use.
- Extract shared code only when there is a concrete current need.
- Prefer the smallest maintainable change that preserves existing architectural invariants.
- Before adding a new concept, check whether the existing domain model, command, query, service or transport already supports the requirement.
- Keep transport-specific code thin; durable product behavior belongs in the authoritative backend/application layer.
- When reviewing or planning work, actively flag unnecessary duplication and speculative architecture.

In short: do not repeat yourself, and do not build things until the product actually needs them.

## Database

`packages/database/schema.sql` is the one canonical pre-release schema. Do not add numbered/versioned migrations. Recreate development databases when incompatible schema changes require it.

Execution Sessions bind directly to selected Runners; persistence must not invent secondary managed-compute ownership identities.

## Source control / Workspace

A Project source is either a trusted backend-visible local Git repository or a remote Git clone URL with an optional ref.

Validate local repository paths against deployment-authorized roots. Project configuration must not become arbitrary filesystem access.

Every Issue uses a deterministic durable branch:

```text
agent-board/<issue-key>
```

For local Projects, the backend owns the persistent Issue Workspace. Before Runner execution Agent Board transfers the exact Issue branch history using the bounded/checksummed transfer protocol. The Runner finalizes that branch and returns it; the server verifies/imports and durably records the returned revision before acknowledgement.

For remote Git Projects, the Runner owns an execution-side bare cache plus per-Run worktree. Source refs live under `refs/remotes/origin/*`; Agent Board Issue branches live under `refs/heads/agent-board/*`. Publication uses a normal non-force push, and the worktree is retained until the server durably records the revision and acknowledges it.

Remote Git authentication is the Runner host's own Git configuration. Agent Board does not persist credentials in clone URLs or forward provider tokens through the Runner protocol.

## Runner execution and security

Agent-executed code is untrusted.

Canonical v0.1 execution:

```text
Agent
 -> Engine + Model Profile
 -> Run
 -> existing scheduler
 -> selected connected Runner
 -> Execution Session
 -> Engine
```

External persistent Runners are preferred production execution. The server-managed internal Runner uses the same outbound protocol-v2 path and the same Execution Session contract.

Never execute coding-agent CLIs directly in the trusted backend process. Never provide Docker daemon credentials/socket access through Agent Board's Runner protocol. Never give the Runner PostgreSQL credentials, server encryption keys or broad control-plane credentials.

Control-plane authorization derives trusted actor identity; caller-controlled headers cannot grant human/admin privileges.

## Events and execution evidence

Persist important activity before live publication. Do not store hidden chain-of-thought.

Large output belongs in durable raw-output/blob storage; Artifacts are first-class records. Run provenance records the selected Runner and resolved Engine/Model/Provider configuration. Review/Run inspection shows Git-native candidate evidence and pinned source identity.

Runner output is normalized/redacted before durable persistence. A WebSocket disconnect is an infrastructure signal and is not, by itself, durable proof that a Run failed.

## Questions and Review

Blocking Questions pause the same Run. For native OpenCode Questions, the same live Runner Execution Session and checkout remain attached through `WAITING_FOR_INPUT`; answering continues the same native Engine session without a replacement prompt/process. The Issue moves to the `BLOCKED` state/column when human input is required according to workflow policy.

Resume/Question state is server-owned. If the Runner transport disconnects, reconcile the same durable Execution Session during the configured reconnect grace before declaring infrastructure failure or allowing retry; do not blindly start duplicate work.

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

Planning strategy, Automations, Agent-created Issues, Source Connections, delivery automation, delegation, Squads and worker pools reuse the canonical Issue/Run/scheduler/Workspace/Git model.

Future Worker Pools supply/place Runner capacity but do not replace Agent, Runner or Execution Session identities.

Users, groups, roles/permissions and broader multi-user administration are expected later but are not yet designed; do not invent their product contracts.

## Plugins

Plugins are deliberately late roadmap work. They come after the complete v0.1 flow and, unless explicitly reprioritized, after foundational users/groups/permissions work.

Plugin expansion must not take priority over the v0.1 execution path.

## Scope discipline

Do not silently invent product behavior. If canonical docs and the relevant issue leave a material ambiguity unresolved, continue independent work and surface the ambiguity rather than creating a new architecture by accident.
