# Product roadmap

This roadmap defines ordering, not release dates. The guiding rule is: **reach the complete v0.1 coding-agent flow as quickly as possible.**

## Phase 0 — complete v0.1 flow

Nothing below this phase should delay it.

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

Required work includes:

- direct Executor Profile -> Runtime configuration
- durable asynchronous/restart-safe scheduling
- Agent concurrency + Model Profile capacity admission
- local repository-backed Issue Workspaces
- Runtime Instance lifecycle with immutable same-Workspace binding
- `agent-runner` binary in official Runtime images
- versioned server/runner WebSocket transport
- separate Runtime Instance / runner / Execution Session / Run identities
- one active Execution Session per runner in v0.1, with protocol support for many sessions over runner lifetime
- canonical execution context and ephemeral Provider secrets
- immutable Run provenance
- durable raw logs and first-class Artifacts
- complete Run inspection and Review candidate evidence
- Runtime policy truthfulness and whole-Agent preflight
- first real coding Engine, OpenCode first
- clean-room Nuxt 4 + Vue 3 + TypeScript + Tailwind CSS frontend using Nuxt UI v4 as the required component foundation
- end-to-end crash/restart/security/integration proof

Frontend implementation follows `frontend-implementation.md` and `frontend-theme.md`.

The scripted Engine is infrastructure validation, not the v0.1 destination.

## Phase 1 — initiation, work structure, and remote repository access

After the local-repository v0.1 flow is proven:

- planning strategy: Auto / Always plan / Skip planning
- Plan artifacts/read model
- scheduled Project Automations creating normal Issues
- Agent-created follow-up Issues under explicit Project policy
- authenticated Source Connections for GitHub, GitLab, Bitbucket and Forgejo
- remote repository clone/fetch using trusted ephemeral credentials
- improved retry/operational UX

These reuse normal Issues/Runs/scheduling and the same durable Workspace model.

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
- higher runner session capacity where safe/useful

Agents remain independent from Worker identity. Worker, Runtime Instance, runner and Execution Session identities remain separate.

The v0.1 runner contract is intentionally compatible with future fleets: a healthy Runtime Instance may already serve many sequential sessions against its one bound Workspace, while Worker Pools later decide where Runtime capacity comes from.

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
