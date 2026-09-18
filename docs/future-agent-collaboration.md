# Agent collaboration, Squads and worker pools

This document separates the implemented Squad foundation, canonical Agent delegation, serialized Workspace handoff, and durable delegated-result/parent-continuation lifecycle from later collaboration work. Future collaboration must continue to reuse Agent Board's canonical Issue, Run, scheduler, Workspace and Git lifecycle rather than creating a second execution system.

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

Squad ownership does **not** automatically fan work out to members. Canonical Agent delegation remains explicit: the authoritative executing leader chooses a target Agent for a bounded task through the same command used by directly Agent-owned Issues. When delegation is effectively available, the trusted Engine request includes the current usable Agent target IDs/names plus the owning Squad's leader and Agent-member names/optional descriptive roles. Human User members remain visible collaboration context but are not `delegate_task` targets. Squad membership is not authorization: the canonical delegation command still revalidates the chosen Agent and no member Run exists until the leader explicitly delegates. Human handoff remains separate future behavior.

## Canonical delegation and phase-2 Workspace handoff

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
- self-delegation and nested delegation are rejected by the current implementation;
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

OpenCode receives `delegate_task(targetAgentId, task)` only when the trusted Engine request contains the delegation capability. The model does not choose the parent identity or idempotency key: the adapter uses the durable native tool-part ID as the request key. Canonical acceptance happens through the same backend command, while execution ownership is yielded only after the matching native `delegate_task` completion is durably observable. Reconnect/recovery reconstructs that completion from native tool history without creating another delegation.

The public API exposes parent-scoped delegation listing and child-Run lineage inspection. Delegation inspection reports the effective Workspace access mode as `WRITE`; scheduler state continues to be read from the linked ordinary Run/job rather than copied into delegation responses.

## Workspace inheritance and handoff

Delegated execution reuses the exact durable Issue Workspace and existing Issue branch. There is no child Workspace, delegation branch or merge/reconciliation layer in this phase.

`WRITE` delegation is serialized. The scheduler and existing execution-session Workspace ownership rules ensure that only one Run can be the authoritative writer at a time. A delegation request may create the child Run immediately, but its normal `START` job is durably held until the parent has safely returned its effective Workspace state and yielded execution ownership.

The parent-to-delegate handoff is:

1. the Engine completes the accepted `delegate_task` call and signals a handoff;
2. the parent uses the existing Runtime finalization or Runner sync-back path to make its current Issue-branch state authoritative;
3. the delegated job is marked handoff-ready but remains non-runnable;
4. the parent's fenced scheduler transition to `PAUSED` atomically releases the ready child and releases the parent's scheduler ownership;
5. the delegated Run enters the normal scheduler/execution path against the same Workspace and branch.

This means the delegate inherits all state that the normal Workspace/Git boundary can safely make authoritative, including changes committed by Runner synchronization and server-side execution finalization. The implementation does not add a second copying or merge system specifically for delegation.

If parent finalization/sync fails, the child remains held and the parent fails instead of starting a delegate from stale state. If cancellation wins before the parent yields, synchronization may preserve trustworthy work but the delegated job remains held. Project/Issue/parent/child/Workspace identity is revalidated when the handoff is marked ready so cross-Project or substituted-Run handoffs fail closed.

Delegate work returns through the ordinary execution finalization/sync path and therefore advances the same authoritative Issue Workspace and branch. After the delegated Run reaches a trustworthy terminal outcome, the result lifecycle atomically records the bounded delegation outcome and queues a normal `RESUME` for the same paused parent Run. The continuation starts from the current authoritative Workspace revision through the existing transfer/materialization path.

## Delegated execution lifecycle

The implemented delegation lifecycle is:

```text
Parent Run (RUNNING)
   |
   +--> canonical delegation request
   |       |
   |       +--> lineage + ordinary child Run/START job (held)
   |
   +--> completed delegate_task
   |
   +--> normal Workspace finalize/sync
   |
   +--> PAUSED + atomic child release
           |
           v
      delegated Run
           |
           +--> normal scheduler/execution
           |
           +--> normal Workspace finalize/sync
           |
           +--> COMPLETED / FAILED / CANCELLED subtask outcome
           |       |
           |       +--> bounded result + existing evidence reference
           |       +--> current Issue Workspace remains authoritative
           |
           +--> atomic parent RESUME intent
                   |
                   v
              same parent Run continues
```

The parent and delegate remain ordinary Runs and the scheduler remains the only execution owner. A successful delegated Run terminates as `COMPLETED`; it does not create the Issue's final Review candidate. Failure or cancellation is also returned as a durable delegation outcome rather than implicitly failing or cancelling the parent. The parent remains responsible for evaluating delegated work and eventually reaching the ordinary `READY_FOR_REVIEW` path.

Delegation persistence stores only the bounded result metadata needed for continuation and inspection: terminal outcome, concise result, evidence reference, whether trustworthy Workspace changes were accepted, continuation job identity, and completion time. Detailed commands, tests, file output and raw logs remain in the delegated Run's existing evidence.

The delegated terminal transition and parent continuation intent share one database transaction. The same parent Run moves from `PAUSED` to `QUEUED`, and a deterministic delegation-scoped `RESUME` job makes reconciliation idempotent. On resume, the Engine request receives the delegation ID, target Agent, bounded original task/result, delegated Run/evidence reference, accepted-Workspace flag and the **current** authoritative Workspace revision. This execution-time continuation context is not written into immutable Run provenance.

Parent cancellation propagates to pending/running delegated work through the canonical Run-cancellation boundary. A terminal parent is a continuation fence: if cancellation wins first, later child completion can persist its own outcome but cannot schedule or resurrect parent execution. If the child terminal transaction wins first, later parent cancellation cancels the queued parent continuation normally.

Restart reconciliation uses durable Run/scheduler/Execution Session evidence and the same terminalization transaction. A delegated terminal outcome is recovered only when Workspace hand-back is trustworthy; disconnected or ambiguous Runner/Workspace authority remains `UNKNOWN` and fail-closed rather than releasing another writer. This preserves the single authoritative Workspace writer and prevents orphan leases/reservations after terminal paths.

A Squad leader uses the same canonical request command as any other authoritative Agent. Server-owned prompt context gives the leader real usable target identities and identifies configured Squad Agent members/roles without restricting delegation to the roster or granting permission through membership. Human User members remain collaboration identities unless a separate human-handoff/notification feature explicitly defines behavior for them.

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

The reusable mixed-membership Squad/ownership foundation, canonical delegation request/lineage layer, serialized Workspace handoff, delegated outcome/parent-continuation lifecycle, and Squad-aware trusted target context are implemented on the same execution model.

```text
complete v0.1 coding flow
 -> planning/automation where useful
 -> canonical delegation request/lineage [implemented]
 -> serialized Workspace handoff [implemented]
 -> delegated outcomes + durable parent continuation/recovery [implemented]
 -> Squad-aware delegation/member context [implemented]
 -> broader messaging/wake policy if needed
 -> worker registry/pools
 -> warm/spot optimizations
 -> broader identity/administration extensions as designed
 -> Plugins last
```

This ordering may evolve, but nothing in this document is allowed to delay the v0.1 critical path.
