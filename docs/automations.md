# Automations and Agent-created follow-up work

These are post-v0.1 product features. They must reuse normal Issues, assignment and scheduling rather than create parallel execution systems.

## Scheduled Project Automations

An Automation is Project-scoped recurring configuration that creates normal Issues.

Conceptual model:

```text
Automation
├── id
├── projectId
├── name
├── enabled
├── agentId
├── schedule expression
├── timezone
├── issue title
├── issue instructions
├── lastTriggeredAt
└── nextTriggerAt
```

When due:

1. resolve Project + configured Agent
2. validate they are usable
3. create exactly one Issue for the occurrence
4. assign the configured Agent
5. use the normal Issue -> Agent -> Run pipeline
6. preserve Automation + occurrence provenance

The Automation layer stops after creating/assigning normal work.

### Scheduling rules

- recurring cron-like schedule
- explicit timezone
- restart-safe
- idempotent per occurrence
- disabled Automations do not trigger
- missed-occurrence behavior is explicit and bounded; prefer latest due occurrence over unbounded backlog replay

## Agent-created follow-up Issues

A separate Project workflow setting controls whether executing Agents may create and assign new follow-up Issues:

```text
[ ] Allow agents to create & assign Issues
```

Default is off.

When enabled, an executing Agent may create a new Issue in the same Project and optionally assign the newly-created Issue to a usable Agent or, once available, Squad.

This capability does not allow arbitrary editing/reassignment/deletion of existing Issues.

## Delegation vs follow-up Issue

Use delegation for work that remains part of the current Issue.

Use a follow-up Issue for genuinely new durable Board work with its own lifecycle, Workspace, Runs and Review.

## Server-owned capability

The Runtime/Engine does not receive a broad Agent Board credential.

The server derives origin context from the active execution:

- Project
- origin Issue
- origin Run
- origin Agent

Caller-supplied values cannot forge these identities.

## Idempotency

Agent execution can retry/replay tool actions. Issue creation therefore uses a durable idempotency identity bound to the originating Run/action.

Replaying a successful action returns the same created Issue rather than creating duplicates.

## Provenance

Automation-created and Agent-created Issues preserve structured origin metadata.

Agent-created provenance includes at least:

- origin kind: Agent
- origin Agent
- origin Run
- origin/parent Issue
- created timestamp

Automation-created provenance includes Automation identity and scheduled occurrence.

This metadata remains historical even after reassignment or later configuration edits.

## Assignment

All created work uses the canonical assignment service/path.

Normal rules continue to apply:

- Agent/Squad usability
- Project isolation
- workflow settings
- scheduler capacity
- repository-backed Workspace creation
- Questions
- Review

## Ordering

Automations and Agent-created follow-up Issues are planned after the complete v0.1 coding flow. They should be implemented before plugin scheduling/product extensions if/when they become a priority.
