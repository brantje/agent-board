# Agent collaboration, Squads and worker pools

This document separates the implemented Squad foundation from later Agent collaboration work. Future collaboration must continue to reuse Agent Board's canonical Issue, Run, scheduler, Workspace and Git lifecycle rather than creating a second execution system.

## Principle

Agent Board remains authoritative for Issues, Runs, scheduling, Workspaces, capacity, cancellation, provenance and Review. Coding Engines may request collaboration, but they do not create a second scheduler or shadow lifecycle.

## Implemented Squad foundation

Squads are durable, reusable Project-scoped collaboration configuration with one authoritative leader Agent and optional Agent/User members.

```text
Squad
├── leader Agent
└── members
    ├── Agent
    └── User
        └── optional descriptive role
```

A Squad can be created, edited and deleted through Project configuration. It has exactly one leader Agent and zero or more additional members. Additional members may be usable Agents or active human Users with effective Project member/admin workflow access. Human Groups remain a separate deployment-global access concept: Groups grant Project access, while Squad membership is collaboration context and grants no Project permission by itself.

A Squad is assignable to an Issue. The Issue persists the Squad ID as canonical ownership. Execution resolves the Squad's current leader through the same backend execution-target path used by normal Agent execution:

```text
Owner: Squad X
Executing Agent: Leader Y
```

Additional Squad members never become implicit Run targets. Adding, removing or re-roleing either an Agent member or a human User member does not fan out execution or create a Run. If a human member later loses effective Project access or becomes disabled, the persisted roster entry remains collaboration history/context but confers no access and does not affect leader execution.

Changing the leader does not rewrite Issue ownership and does not create a Squad-specific Run, scheduler or Workspace. Existing Runs retain their historical executing Agent; eligible future/recovered execution resolves the current leader through the normal Run/scheduler path.

The web UI reads ownership and execution context from shared backend read models. Squad updates publish durable Project events so open Issue views re-read backend truth after leader changes.

Current Squad behavior does **not** fan work out to members and does not grant delegation. Member roles are descriptive configuration only until canonical delegation or other explicit collaboration behavior is implemented.

## Delegation

Delegation is future work and is a subtask inside the current Issue/Run context. It is not implied by assigning an Issue to a Squad.

An Agent may later have an explicit configuration option:

```text
[ ] Allow delegation
```

When disabled, Agent Board does not grant an effective delegation capability. When enabled, the assigned/executing parent Agent may delegate explicit work to another usable Agent.

The parent Agent remains authoritative for the Issue and decides whether to use, reject, combine or follow up on delegated results. Delegates cannot independently move the Issue to Review or Done.

Delegation must use normal durable Runs/execution records and the normal scheduler. Agent concurrency and Model Profile capacity still apply.

## Workspace inheritance

Delegates must work against the parent's effective Issue Workspace state, not an implicit clean checkout.

Required inherited state can include:

- tracked modifications
- staged changes
- unstaged changes
- relevant untracked files
- current branch/base state
- changes produced earlier in the same Issue workflow

Read-only delegates may inspect the current Workspace concurrently where safe.

Authoritative writes must not race. Initial implementation should serialize writers through a recoverable exclusive write lease unless an isolated child/sandbox Workspace model is explicitly implemented.

If child Workspaces are used for parallel coding, they must derive from the current effective Issue Workspace state and return explicit durable changes for validation/application. A fresh clone at only the last commit is insufficient.

## Delegated execution lifecycle

```text
Parent Run
   |
   +--> delegation request
           |
           v
      durable scheduler
           |
           v
      delegated execution
           |
           +--> result/finding/change set
           |
           v
      parent resumes/continues
```

Parent cancellation/failure must not leave orphan delegated executions or permanent Workspace locks.

Run/Issue inspection should show delegated Agent, task, state, result and Workspace access mode.

Once delegation exists, a Squad leader may use the normal delegation capability with suitable Agent members. Human User members remain collaboration identities unless a separate future human-handoff/notification feature explicitly defines behavior for them. The delegation design must extend the existing Squad identity/ownership model rather than changing it.

## Agent-created follow-up work

Delegation is not the same as creating a new Issue.

- delegation = subtask inside current Issue
- Agent-created follow-up Issue = new durable Board work with its own Workspace/Runs/Review

When Project policy allows Agent-created Issues, that capability uses the canonical Issue creation/assignment path.

## Durable Agent messaging

General Agent-to-Agent messaging may be useful later, but it is not a prerequisite for the first delegation implementation.

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

- Project boundaries
- explicit Agent usability/authorization
- human Project authorization independently of Squad membership
- scheduler capacity
- bounded/recoverable Workspace writes
- auditable delegation lineage
- secret isolation
- cancellation/restart safety
- human Review as delivery gate

Additional recursion/delegation-depth and rate/budget policies may be added when real usage demonstrates the need; do not overbuild them before then.

## Ordering

The reusable mixed-membership Squad/ownership foundation is already implemented independently of delegation. Remaining collaboration work can build on it without coupling basic Squad management to delegation.

```text
complete v0.1 coding flow
 -> planning/automation where useful
 -> delegation
 -> Squad-aware delegation/member collaboration where useful
 -> broader messaging/wake policy if needed
 -> worker registry/pools
 -> warm/spot optimizations
 -> broader identity/administration extensions as designed
 -> Plugins last
```

This ordering may evolve, but nothing in this document is allowed to delay the v0.1 critical path.