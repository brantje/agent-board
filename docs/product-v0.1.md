# Agent Board v0.1 product specification

This document defines the user-facing v0.1 product. The primary goal is one complete, trustworthy real coding-agent flow as quickly as possible.

## v0.1 success condition

A user configures a Project Git source and runnable Agent, assigns an Issue, closes the browser, and later reviews real repository changes produced by a real coding Engine on a selected Runner.

```text
Project source (local or git)
  -> Issue
  -> Agent
       -> Engine
       -> Model Profile -> Provider
  -> QUEUED Run
  -> durable scheduler
  -> selected connected Runner
  -> deterministic agent-board/<issue-key> branch
  -> source-aware checkout/worktree
  -> Execution Session
  -> coding Engine
  -> commands / Git changes / tests / Artifacts
  -> Git finalization + branch return/publication
  -> Question / resume when needed
  -> Review pinned to Git SHAs
  -> Approve
  -> Done / delivery according to source semantics
```

The scripted Engine is a walking-skeleton/test tool; it is not the v0.1 destination.

## Product principle

Keep normal configuration understandable:

```text
Provider -> Model Profile
Engine + Model Profile -> Agent
Issue -> assignee -> Run
```

`Runner`, `agent-runner` and Execution Session are infrastructure concepts, not additional Agent configuration layers. Runner placement is scheduler-owned.

Do not expose implementation layers unless they solve a concrete user problem.

The web product is a clean-room implementation using **Nuxt 4 + Vue 3 + TypeScript + Tailwind CSS + Nuxt UI v4**. The Go backend remains authoritative for durable product state and execution.

Frontend implementation follows `frontend-implementation.md` and `frontend-theme.md`.

## Scope model

Shared/global or Project-scoped product configuration:

- Agents
- Providers
- Model Profiles

Project source configuration belongs to Project. Project Runner allowlisting is execution policy, not Agent configuration.

Inside a Project, shared resources are visible/read-only and Project-owned resources remain isolated.

## Navigation

Global:

```text
Projects
Runs
Inbox
Settings
```

Project:

```text
Board
Agents
Runs
Settings
```

Settings:

```text
Models
  Providers
  Model Profiles
Execution
  Agents (global/shared only)
Infrastructure
  Runners (global only)
Project page for repository/workflow
```

Shared Agents live under global Settings. Project Agents stay in the project primary menu, not in the project settings sidebar.

Plugins are not part of the v0.1 critical path.

## Project

A Project owns the Board plus repository context used by its Issues.

Source configuration is:

```text
local
  -> local/server-visible repository path
  -> default/base branch

git
  -> clone URL
  -> optional source ref
```

Issue prefix is immutable and globally unique and forms public Issue keys such as `AB-12`.

Local source paths are validated against deployment-authorized repository roots; Project configuration is not arbitrary filesystem access.

A remote Git source is saved provider-neutrally. The Runner uses its host Git authentication to fetch/push; v0.1 does not require a GitHub/GitLab/etc. Source Connection merely to execute a remote Git Project.

Every Issue uses a deterministic branch `agent-board/<issue-key>` and continues that branch across attempts.

## Provider and Model Profile

Provider is the configured model connection and owns credentials, health and model discovery. Credentials are encrypted at rest and never returned plaintext after save.

Model Profile contains:

```text
Name
Provider
Model
Temperature
Max tokens
Capacity (optional max concurrent Runs)
```

Empty Capacity means unlimited. The durable scheduler enforces capacity.

## Runner execution

The preferred v0.1 production execution architecture is a persistent external Linux Runner host running the standalone `agent-runner` binary. The Runner connects outbound to Agent Board over protocol v2 and advertises its installed Engine capabilities.

The existing scheduler chooses an eligible connected Runner. External Runners are preferred. The server-managed internal Runner is fallback only when Project policy permits `allow_internal_runner`.

A Project may restrict external Runner eligibility with its Runner allowlist. Agents never select or configure a Runner.

Runner, Execution Session and Run remain separate identities. Default Runner capacity is 5 concurrent Execution Sessions.

For local Projects, the Runner receives/returns the Issue branch using the existing bounded #71 transfer. For remote Git Projects, the Runner uses a bare repository cache plus a per-Run worktree and publishes the Issue branch with a normal non-force push.

## Agent

Agent form:

```text
Name
Role / instructions
Engine
Model Profile
```

Operational fields such as concurrency limit may live under Advanced. Engine settings are an optional JSON object on the Agent.

Agents select Engine and Model Profile. Runner placement remains scheduler-owned; Agents do not select execution hosts. Draft, disabled or archived Agents cannot be assigned as runnable Agents. Agent concurrency is enforced by the scheduler.

Built-in Engine settings use the Agent Board Nuxt/Nuxt UI frontend. Plugin-provided Engine settings belong to the later sandboxed Plugin boundary.

## Board workflow

```text
BACKLOG -> TODO -> IN_PROGRESS -> BLOCKED -> REVIEW -> DONE
```

`BLOCKED` is both a durable Issue state and a normal Board column. The column is simply the visual projection of that Issue state.

Board status and Run status are separate. Run states such as `QUEUED`, `RUNNING`, `WAITING_FOR_INPUT` and `FAILED` do not become additional Board columns.

Issue priority uses one numeric v0.1 vocabulary: `0`, `1`, `2`, `3`, `4`. `0` is the default/base priority and larger values mean higher relative priority. Priority is durable Issue metadata; by itself it does not bypass blockers, change Board state, create/cancel a Run or override scheduler admission policy.

Assigning a runnable Agent normally creates/schedules a Run and moves the Issue into active work. The request returns after durable scheduling; execution continues server-side.

Changing assignee during active work cancels the current attempt and starts a new attempt on the same Issue branch/Workspace identity.

Done Issues cannot start Runs. Reopen returns Done -> Todo without auto-starting.

## Project workflow settings

Configurable behavior includes:

- successful Run -> auto Review or manual transition
- failed Run board behavior
- blocking Question answer -> auto-resume or manual Resume
- Review changes -> auto new attempt or Todo/manual restart
- manual execution override for eligible blocked Issues
- assignment behavior with unresolved blockers
- whether Todo allows unassigned Issues

Scheduler capacity waiting is not a board blocker.

## Issue relationships

Issue relationships are durable, Project-scoped, server-authoritative records. Every stored relationship has an explicit source Issue and target Issue. v0.1 does not synthesize an inverse relationship row, and the frontend must not infer or persist one.

The canonical relationship types and source -> target meanings are:

- `blocks`: the source Issue declares that it blocks the target Issue.
- `depends_on`: the source Issue declares that it depends on the target Issue.
- `related_to`: the source Issue records the target as related. The durable record is still directional even though the human meaning may be symmetric.
- `duplicates`: the source Issue declares that it duplicates the target Issue.

A relationship record does not itself mutate Issue status, start/cancel Runs, or perform browser-side orchestration. Any workflow effect from blockers or dependencies is evaluated by trusted server-side policy. Self-links, duplicates and cross-Project targets are rejected by the server.

## Questions and Inbox

Blocking Question:

```text
Issue -> BLOCKED
Run   -> WAITING_FOR_INPUT
```

Questions are answered from Issue detail. Inbox contains blocking Questions, Review requests, failed Runs and other human-attention items.

Any continuation required by a Question answer is durable before the answer command returns success.

The Runner owns no durable Question/resume product state. For native OpenCode Questions, the same live Runner Execution Session, native Engine session and checkout remain attached through `WAITING_FOR_INPUT`; answering continues that same native session. Agent Board does not finalize or publish/return the Issue branch merely because input is pending.

Recovery after an actual Runner/transport failure reconciles that durable session rather than starting duplicate work.

## Runs

Run detail is first-class and exposes persisted evidence:

- status/timestamps/attempt
- Issue/Agent/Workspace
- immutable execution provenance
- queue/wait reason
- timeline/messages
- commands/tools
- Git branch/change evidence and diff
- tests/checks
- selected Runner and Execution Session diagnostics/provenance
- raw logs when needed
- Artifacts
- failure/cancellation/blocking reason

Raw Event JSON is diagnostic fallback, not the primary UX.

## Git-native execution result

At a real completion/failure/cancellation hand-back boundary Agent Board requires the checkout to still be on the expected `agent-board/<issue-key>` branch and to retain the recorded execution-start revision in history. Engine-created commits are preserved. Remaining non-ignored changes are committed when necessary so the boundary is a clean Git HEAD.

Local Runner execution returns that branch to the server and waits for apply acknowledgement. Remote Git execution publishes that branch normally/non-force and retains the worktree until the server durably records the revision and acknowledges it.

A successful remote push whose response/database write/ack is lost can be retried from the same retained session without rerunning the Engine. Arbitrary external advancement is still rejected.

## Review

Human Review is the v0.1 delivery gate.

Review pins the exact Issue base and reviewed Issue branch HEAD:

```text
base_revision
review_revision
```

The reviewed source change is reproducible with:

```bash
git diff <base_revision>..<review_revision>
```

Review also shows tests, Artifacts and relevant execution evidence. Those evidence objects do not form a duplicate filesystem/candidate snapshot used for delivery.

Request Changes continues the same Issue branch. Prior Reviews remain immutable because their pinned SHAs do not change.

For a local Project, Approve integrates the exact pinned reviewed commit into the current local Project Workspace target state and then completes the Issue when durable approval finalization succeeds. A real target conflict is explicit and does not silently discard either side.

For a remote Git Project, publishing the Issue branch is **not** remote target integration. v0.1 does not pretend that internal Review approval merged a remote target branch. Provider PR/MR actions remain later Source Connection work.

## Future Project delivery policy

After Source Connections/source-provider actions exist, a Project may choose an explicit delivery policy.

The safe default remains human-gated delivery. A later autonomous policy may automatically create or update a pull/merge request after a successful verified Review without requiring Agent Board's internal approval first.

Automatic merge/deploy is a separate, stronger permission and must not be implied by PR/MR creation.

## Execution preflight

The product distinguishes `configured` from `runnable`. Preflight evaluates Agent, Engine, Model Profile/Provider credentials/health, live Runner protocol compatibility/availability and Project source prerequisites.

For remote Git sources, execution-time Git authentication failures are explicit; Agent Board does not silently fall back to local transfer.

## Explicitly after the v0.1 flow

- planning strategy: Auto / Always plan / Skip planning
- scheduled Automations creating normal Issues
- Agent-created follow-up Issues
- authenticated Source Connections and provider-specific repository actions
- Project PR/MR delivery policy
- Agent delegation
- Squads
- worker pools / warm / spot execution
- higher concurrent Execution Session capacity where safe/useful
- users, groups, permissions and broader multi-user administration
- additional provider/account integrations
- Plugins and plugin ecosystem work last

## Implementation priority

1. Agent Engine + Model Profile configuration (no Agent host selection)
2. durable async scheduler/restart-safe continuation with live Runner admission
3. Project source configuration and Git-native Issue branches
4. external and internal `agent-runner` hosts over protocol v2
5. local branch-only transfer and remote Runner Git cache/worktree execution
6. canonical execution context + secure Provider credentials
7. immutable Runner provenance + durable raw logs/Artifacts
8. SHA-pinned Run/Review evidence
9. operational preflight/Runner-availability truthfulness
10. first real coding Engine (OpenCode first)
11. prove Project source -> Run -> Runner -> Execution Session -> Engine -> Git changes -> Review end to end
12. only then broaden provider-specific repository integrations and the roadmap

Frontend implementation uses Nuxt 4 + Nuxt UI v4 and remains within this v0.1 product scope. Plugin work is deliberately last, including after future users/groups/permissions work unless explicitly reprioritized.

Earlier #15 candidate snapshot/staging assumptions are superseded by this Git-native source/Review model.
