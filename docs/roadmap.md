# Product roadmap

This roadmap defines ordering, not release dates. The guiding rule is: **reach the complete v0.1 coding-agent flow as quickly as possible.**

## Phase 0 — complete v0.1 flow

Nothing below this phase should delay it.

```text
Project source (local or git)
 -> Issue
 -> Agent
      -> Engine
      -> Model Profile -> Provider
 -> durable scheduler
 -> selected connected Runner
 -> durable Issue branch/Workspace state
 -> Execution Session
 -> real coding Engine
 -> durable execution evidence
 -> Question/resume when needed
 -> Review pinned to Git SHAs
 -> human approval
```

Required work includes:

- Agent Engine + Model Profile configuration
- durable asynchronous/restart-safe scheduling
- Agent concurrency + Model Profile capacity admission
- Project Runner eligibility and live Runner capacity admission
- local repository-backed Issue Workspaces and remote Git sources
- standalone external `agent-runner` plus server-managed internal Runner
- versioned server/Runner WebSocket transport
- separate Runner / Execution Session / Run identities
- default Runner capacity of 5 concurrent Execution Sessions, with protocol support for advertised `max_active_sessions`
- local branch-only transfer and remote Git cache/worktree execution
- canonical execution context and ephemeral Provider secrets
- immutable Run/Runner provenance
- durable raw logs and first-class Artifacts
- complete Run inspection and SHA-pinned Review evidence
- Runner/source preflight truthfulness
- first real coding Engine, OpenCode first
- clean-room Nuxt 4 + Vue 3 + TypeScript + Tailwind CSS frontend using Nuxt UI v4 as the required component foundation
- end-to-end crash/restart/security/integration proof

Frontend implementation follows `frontend-implementation.md` and `frontend-theme.md`.

The scripted Engine is infrastructure validation, not the v0.1 destination.

## Phase 1 — initiation, work structure, and source-provider access

After the v0.1 flow is proven:

- planning strategy: Auto / Always plan / Skip planning
- Plan artifacts/read model
- scheduled Project Automations creating normal Issues
- Agent-created follow-up Issues under explicit Project policy
- authenticated Source Connections for GitHub, GitLab, Bitbucket and Forgejo
- provider-specific repository actions and trusted ephemeral credentials where needed
- improved retry/operational UX

These reuse normal Issues/Runs/scheduling and the same Git-native Issue branch model.

## Phase 2 — delivery automation and Agent collaboration

- explicit Project delivery policy
- source-provider PR/MR creation and update actions
- optional autonomous PR/MR delivery after a successful verified candidate
- Agent delegation with per-Agent `Allow delegation`
- durable delegated results/inspection
- safe Workspace inheritance and write leases
- Squads with one leader + reusable members
- broader Agent messaging/wake semantics where useful

The default delivery policy remains human-gated. Creating/updating a PR/MR does not imply auto-merge or deployment; those require separate explicit policy.

Do not create a Squad scheduler or parallel Run lifecycle.

## Phase 3 — execution topology and scale

- Worker registry
- Worker Pools
- warm/permanent workers
- spot/ephemeral execution and recovery
- scheduling preferences/classes
- higher Runner session capacity where safe/useful

Agents remain independent from Worker and Runner identity. Worker, Runner and Execution Session identities remain separate.

Future fleets should reuse the same scheduler -> Runner -> Execution Session contract rather than introduce a second execution lifecycle.

## Phase 4 — multi-user administration

Future areas include:

- users
- groups
- roles and permissions
- organization/deployment administration
- sharing/access policy
- audit/admin UX

These are not yet fully designed. Do not invent detailed contracts from this roadmap entry alone.

## Phase 5 — integrations/product breadth

Examples:

- richer provider/account authorization
- additional coding Engines
- auto-merge/deploy policies where explicitly designed
- external triggers/integrations
- broader automation policy

Exact ordering follows demonstrated user value.

## Phase 6 — Plugins last

Plugin expansion is deliberately the final major roadmap area and does not compete with the v0.1 critical path or foundational users/groups/permissions work.

Later Plugin work may include:

- installation/activation management UX
- typed Actions and triggers
- sandboxed UI extensions
- MCP tools/resources/skills
- SDK/packaging/ecosystem work

## Roadmap rule

GitHub issues track implementation. Canonical docs define the intended product/architecture. A change to durable product behavior updates the relevant canonical docs in the same work.
