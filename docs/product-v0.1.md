# Agent Board v0.1 product specification

This document defines the user-facing v0.1 product. The primary goal is one complete, trustworthy real coding-agent flow as quickly as possible.

## v0.1 success condition

A user configures a local Project Git repository and runnable Agent, assigns an Issue, closes the browser, and later reviews real repository changes produced by a real coding Engine in the selected Runtime.

```text
Local Project repository
  -> Issue
  -> Agent
  -> Executor Profile
       -> Engine
       -> Model Profile -> Provider
       -> Runtime
  -> QUEUED Run
  -> durable scheduler
  -> durable Issue Workspace
  -> Runtime Instance
  -> coding Engine
  -> commands / files / tests / Artifacts
  -> Question / resume when needed
  -> Review
  -> Approve
  -> Done
```

The scripted Engine is a walking-skeleton/test tool; it is not the v0.1 destination.

## Product principle

Keep normal configuration understandable:

```text
Provider -> Model Profile
Engine + Model Profile + Runtime -> Executor Profile
Agent -> Executor Profile
Issue -> assignee -> Run
```

Do not expose implementation layers unless they solve a concrete user problem.

The web product is a clean-room implementation using **Nuxt 4 + Vue 3 + TypeScript + Tailwind CSS + Nuxt UI v4**. The Go backend remains authoritative for durable product state and execution.

Frontend implementation follows `frontend-implementation.md` and `frontend-theme.md`.

## Scope model

Shared/global or Project-scoped:

- Agents
- Model Profiles
- Runtimes
- Executor Profiles

Providers are global in v0.1. Project repository configuration belongs to Project.

Inside a Project, shared resources are visible/read-only and Project-owned resources remain isolated.

## Navigation

Global:

```text
Projects
Agents
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
Infrastructure
  Runtimes
Execution
  Agents
  Executor Profiles
Projects configure repository/source + workflow behavior
```

Plugins are not part of the v0.1 critical path.

## Project

A Project owns the Board plus repository context used by its Issues.

The first v0.1 repository source is a local Git repository accessible to the backend deployment:

```text
Local repository path/source
Default/base branch
```

The source path is validated against deployment-authorized repository roots; Project configuration is not arbitrary filesystem access.

When an Issue first executes, Agent Board automatically materializes the configured repository into its durable Workspace.

Authenticated remote repositories and Source Connections are implemented after the first complete local-repository coding flow is proven.

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

## Runtime

Runtime is the reusable configured execution environment and owns the full execution policy:

```text
Name
Scope
Kind (Docker first)
Image
CPU / memory / PID limits
Timeout
Network policy
Workspace policy
Allowed secret refs
Tooling / capabilities
Enabled state
```

Runtime health/preflight distinguishes saved configuration from operational executability.

## Executor Profile

```text
Name
Engine
Model Profile
Runtime
```

Executor Profile references Runtime directly. An Agent references exactly one Executor Profile.

Built-in Engine settings use the Agent Board Nuxt/Nuxt UI frontend. Plugin-provided Engine settings belong to the later sandboxed Plugin boundary.

## Agent

Agent form:

```text
Name
Role / instructions
Executor Profile
```

Operational fields such as concurrency limit may live under Advanced.

Draft, disabled or archived Agents cannot be assigned as runnable Agents. Agent concurrency is enforced by the scheduler.

## Board workflow

```text
BACKLOG -> TODO -> IN_PROGRESS -> BLOCKED -> REVIEW -> DONE
```

`BLOCKED` is both a durable Issue state and a normal Board column. The column is simply the visual projection of that Issue state.

Board status and Run status are separate. Run states such as `QUEUED`, `RUNNING`, `WAITING_FOR_INPUT` and `FAILED` do not become additional Board columns.

Assigning a runnable Agent normally creates/schedules a Run and moves the Issue into active work. The request returns after durable scheduling; execution continues server-side.

Changing assignee during active work cancels the current attempt and starts a new attempt on the same Issue Workspace.

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

- blocks
- depends on
- related to
- duplicates

## Questions and Inbox

Blocking Question:

```text
Issue -> BLOCKED
Run   -> WAITING_FOR_INPUT
```

Questions are answered from Issue detail. Inbox contains blocking Questions, Review requests, failed Runs and other human-attention items.

Any continuation required by a Question answer is durable before the answer command returns success.

## Runs

Run detail is first-class and exposes persisted evidence:

- status/timestamps/attempt
- Issue/Agent/Workspace
- immutable execution provenance
- queue/wait reason
- timeline/messages
- commands/tools
- complete changed-file evidence/diff
- tests/checks
- selected Runtime + Runtime Instance lifecycle
- raw logs when needed
- Artifacts
- failure/cancellation/blocking reason

Raw Event JSON is diagnostic fallback, not the primary UX.

## Review

Human Review is the v0.1 delivery gate.

Review shows the complete candidate produced by the attempt, including staged, unstaged and new/untracked files where applicable, plus tests, Artifacts and relevant execution evidence.

Approve -> Done. Request changes may create a new attempt on the same Issue Workspace according to Project policy.

Automatic external delivery is not required for the first v0.1 proof.

## Future Project delivery policy

After remote Source Connections/source-provider actions exist, a Project may choose an explicit delivery policy.

The safe default remains human-gated delivery. A later autonomous policy may automatically create or update a pull/merge request after a successful verified candidate without requiring Agent Board's internal approval first.

Automatic merge/deploy is a separate, stronger permission and must not be implied by PR/MR creation.

## Execution preflight

The product distinguishes `configured` from `runnable`. Preflight evaluates Agent, Executor Profile, Engine, Model Profile/Provider credentials/health, direct Runtime health/policy and local Project repository prerequisites.

## Explicitly after the v0.1 flow

- planning strategy: Auto / Always plan / Skip planning
- scheduled Automations creating normal Issues
- Agent-created follow-up Issues
- authenticated remote Source Connections
- Project delivery policy and source-provider PR/MR actions
- Agent delegation
- Squads
- worker pools / warm / spot execution
- users, groups, permissions and broader multi-user administration
- additional provider/account integrations
- Plugins and plugin ecosystem work last

## Implementation priority

1. direct Executor Profile -> Runtime configuration
2. durable async scheduler/restart-safe continuation
3. local repository-backed Issue Workspaces
4. Runtime process/session execution and Docker operability
5. canonical execution context + secure Provider credentials
6. immutable provenance + durable raw logs/Artifacts
7. complete Run/Review evidence
8. operational preflight/runtime-policy truthfulness
9. first real coding Engine (OpenCode first)
10. prove local repository -> Run -> Runtime -> changes -> Review end to end
11. only then broaden repository integrations and the roadmap

Frontend implementation uses Nuxt 4 + Nuxt UI v4 and remains within this v0.1 product scope. Plugin work is deliberately last, including after future users/groups/permissions work unless explicitly reprioritized.
