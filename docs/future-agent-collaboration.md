# Agent collaboration, Squads and worker pools

This document separates the implemented Squad foundation and canonical phase-1 Agent delegation from later collaboration work. Future collaboration must continue to reuse Agent Board's canonical Issue, Run, scheduler, Workspace and Git lifecycle rather than creating a second execution system.

## Principle

Agent Board remains authoritative for Issues, Runs, scheduling, Workspaces, capacity, cancellation, provenance and Review. Coding Engines may request collaboration, but they do not create a second scheduler or shadow lifecycle.

## Implemented Squad foundation

Squads are durable, reusable Project-scoped collaboration configuration with one authoritative leader Agent that is enabled and usable in the Project/global Agent scope, plus optional Agent/User members.

```text
Squad
├── leader Agent
└── members
    ├── Agent
    └── User
        └── optional descriptive role
```

A Squad can be created, edited and deleted through Project configuration. It has exactly one enabled leader Agent usable in the Project/global Agent scope and zero or more additional members. Additional members may be usable Agents or active human Users with effective Project member/admin workflow access. Human Groups remain a separate deployment-global access concept: Groups grant Project access, while Squad membership is collaboration context and grants no Project permission by itself.

A Squad is assignable to an Issue. The Issue persists the Squad ID as canonical ownership. Execution resolves the Squad's current leader through the same backend execution-target path used by normal Agent execution:

```text
Owner: Squad X
Executing Agent: Leader Y
```

Additional Squad members never become implicit Run targets. Adding, removing or re-roleing either an Agent member or a human User member does not fan out execution or create a Run. If a human member later loses effective Project access or becomes disabled, the persisted roster entry remains collaboration history/context but confers no access and does not affect leader execution.

Changing the leader does not rewrite Issue ownership and does not create a Squad-specific Run, scheduler or Workspace. Existing Runs retain their historical executing Agent; eligible future/recovered execution resolves the current leader through the normal Run/scheduler path.

The web UI reads ownership and execution context from shared backend read models. Squad updates publish durable Project events so open Issue views re-read backend truth after leader changes.

Current Squad behavior does **not** automatically fan work out to members. Canonical Agent delegation is explicit: the authoritative executing Agent chooses a target Agent for a bounded task. Squad-aware member targeting and human handoff remain separate future behavior.

## Canonical delegation phase 1

Delegation is a bounded subtask inside the current Issue/Run context. It is not a new Issue and is not implied by assigning an Issue to a Squad.

Agents have an explicit configuration option:

```text
[ ] Allow delegation
```

The default is disabled. When disabled, an authoritative Run for that Agent does not receive the delegation capability. When enabled, an authoritative running parent may request bounded work from another usable Agent in the same Project scope.

The canonical request command is server-owned and enforces the same rules regardless of caller:

- parent Project, Issue, Run and Agent identity come from durable execution state rather than model-supplied identity;
- the parent Agent must have `Allow delegation` enabled and the parent Run must still be authoritative and running;
- the target must be a usable Agent in the Project/global Agent scope;
- self-delegation and nested delegation are rejected in phase 1;
- an existing active Run for the same Issue + target Agent is a conflict rather than an implicit coalescing rule;
- a stable request key makes an accepted logical request idempotent.

Acceptance creates exactly one ordinary target `Run` and the normal scheduler `START` job. Attempt numbering, Agent/model/Runner capacity, source availability, queue/wait state and execution evidence remain owned by the existing Run/scheduler lifecycle. Delegation persistence stores only lineage and request identity:

```text
parent Run/Agent
Issue + Project
target Agent
bounded task
linked delegated Run
request key + timestamps
```

There is no delegation-specific Run status, scheduler, queue or Workspace type.

Issue ownership and Board status are not changed by accepting a delegation request. A delegated Run is durably recognizable from its lineage. Trusted execution context therefore withholds authoritative Issue-status and further-delegation capabilities from the delegate. The parent remains the Issue authority.

OpenCode receives `delegate_task(targetAgentId, task)` only when the trusted Engine request contains the delegation capability. The model does not choose the parent identity or idempotency key: the adapter uses the durable native tool-part ID as the request key and replays completed tool parts safely through the canonical idempotent command after reconnect/recovery.

The public API exposes parent-scoped delegation creation/listing and child-Run lineage inspection. Scheduler state continues to be read from the linked ordinary Run/job rather than copied into delegation responses.

## Workspace inheritance

Phase 1 links the delegated Run to the same durable Issue Workspace as the parent. The normal scheduler therefore remains responsible for Workspace admission and prevents this feature from inventing a second execution path.

The next Workspace collaboration phase must make handoff semantics explicit so delegates work against the parent's effective Issue Workspace state rather than an implicit clean checkout. Required inherited state can include:

- tracked modifications;
- staged changes;
- unstaged changes;
- relevant untracked files;
- current branch/base state;
- changes produced earlier in the same Issue workflow.

Authoritative writes must not race. Explicit recoverable handoff/write-lease behavior, and any future isolated child/sandbox Workspace model, belong to the Workspace-handoff phase rather than the canonical request model.

If child Workspaces are introduced for parallel coding, they must derive from the current effective Issue Workspace state and return explicit durable changes for validation/application. A fresh clone at only the last commit is insufficient.

## Delegated execution lifecycle

Phase 1 establishes the left side of the collaboration lifecycle:

```text
Parent Run
   |
   +--> canonical delegation request
           |
           +--> durable lineage
           |
           v
        ordinary Run
           |
           v
      normal scheduler
           |
           v
      delegated execution
```

Structured delegated outcomes, result application, parent continuation/resume, cancellation propagation and richer inspection are later lifecycle phases. They must extend the durable parent/child identity above rather than create a parallel orchestration system.

Once Squad-aware delegation is implemented, a Squad leader may use the same canonical request command with suitable Agent members. Human User members remain collaboration identities unless a separate human-handoff/notification feature explicitly defines behavior for them.

## Agent-created follow-up work

Delegation is not the same as creating a new Issue.

- delegation = subtask inside current Issue;
- Agent-created follow-up Issue = new durable Board work with its own Workspace/Runs/Review.

When Project policy allows Agent-created Issues, that capability uses the canonical Issue creation/assignment path.

## Durable Agent messaging

General Agent-to-Agent messaging may be useful later, but it is not a prerequisite for delegation.

If added, messages are server-owned, durable, Project-scoped and correlated with Issues/Runs. Direct Runtime-to-Runtime connectivity is never required for correctness.

## Worker pools

Workers are compute, not Agents.

Future pools may include permanent, warm and spot/ephemeral capacity. Scheduling may choose a worker without changing Agent/Run identity.

```text
Run
 -> scheduler
 -> Worker/Pool
 -> Runtime Instance
```

Spot recovery is possible only because Run history and Workspace state are durable outside the worker.

## Safety

Collaboration must preserve:

- Project boundaries;
- explicit Agent usability/authorization;
- human Project authorization independently of Squad membership;
- scheduler capacity;
- bounded/recoverable Workspace writes;
- auditable delegation lineage;
- secret isolation;
- cancellation/restart safety;
- human Review as delivery gate.

Additional recursion/delegation-depth and rate/budget policies may be added when real usage demonstrates the need; do not overbuild them before then.

## Ordering

The reusable mixed-membership Squad/ownership foundation and canonical delegation request/lineage layer are implemented independently. Remaining collaboration work can build on those primitives without coupling basic Squad management to delegation.

```text
complete v0.1 coding flow
 -> planning/automation where useful
 -> canonical delegation request/lineage [implemented]
 -> Workspace handoff + delegated outcomes/continuation
 -> Squad-aware delegation/member collaboration
 -> broader messaging/wake policy if needed
 -> worker registry/pools
 -> warm/spot optimizations
 -> broader identity/administration extensions as designed
 -> Plugins last
```

This ordering may evolve, but nothing in this document is allowed to delay the v0.1 critical path.
