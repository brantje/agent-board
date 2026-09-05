# Future architecture: Agent collaboration, Squads and worker pools

This document records post-v0.1 collaboration direction. None of it should delay the first complete real coding-agent flow.

## Principle

Agent Board remains authoritative for Issues, Runs, scheduling, Workspaces, capacity, cancellation, provenance and Review. Coding Engines may request collaboration, but they do not create a second scheduler or shadow lifecycle.

## Delegation

Delegation is a subtask inside the current Issue/Run context.

An Agent has an explicit configuration option:

```text
[ ] Allow delegation
```

When disabled, Agent Board does not grant an effective delegation capability. When enabled, the assigned parent Agent may delegate explicit work to another usable Agent.

The parent Agent remains authoritative for the Issue and decides whether to use, reject, combine or follow up on delegated results. Delegates cannot independently move the Issue to Review or Done.

Delegation uses normal durable Runs/execution records and the normal scheduler. Agent concurrency and Model Profile capacity still apply.

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

## Squads

Squad is reusable team configuration layered on delegation.

```text
Squad
├── leader Agent
└── member Agents
    └── optional descriptive role
```

A Squad is assignable to an Issue. The assignment preserves Squad identity while the leader becomes the authoritative executing Agent.

The leader receives Squad member/role context and may delegate through the normal delegation capability. It need not use every member.

Do not create Squad Runs, Squad schedulers or a separate Squad lifecycle.

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
- scheduler capacity
- bounded/recoverable Workspace writes
- auditable delegation lineage
- secret isolation
- cancellation/restart safety
- human Review as delivery gate

Additional recursion/delegation-depth and rate/budget policies may be added when real usage demonstrates the need; do not overbuild them before then.

## Ordering

```text
complete v0.1 coding flow
 -> planning/automation where useful
 -> delegation
 -> Squads
 -> broader messaging/wake policy if needed
 -> worker registry/pools
 -> warm/spot optimizations
 -> users/groups/permissions and broader administration as designed
 -> Plugins last
```

This ordering may evolve, but nothing in this document is allowed to delay the v0.1 critical path.
