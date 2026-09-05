# Event Protocol

Agent Board uses an append-only structured Event stream as the durable execution and audit history for Runs.

The protocol exists so the Nuxt UI, Go backend, Runtime implementations, Engine adapters, audit views, and future integrations all speak the same language.

## Goals

The Event stream must make it possible to reconstruct:

- what work was requested
- which Agent/Run handled it
- which Runtime Instance executed it
- what the Agent said and did
- which tools/commands/tests ran
- what files changed
- which Questions were asked
- what humans answered
- which Decisions were made
- which permissions/secrets were requested or used
- how delegation happened
- how the Run ended

## Envelope

All persisted Events use a common language-neutral envelope.

Conceptual shape:

```json
{
  "id": "uuid",
  "schemaVersion": 1,
  "type": "run.started",
  "timestamp": "RFC3339 timestamp",
  "projectId": "uuid",
  "issueId": "uuid or null",
  "runId": "uuid or null",
  "agentId": "uuid or null",
  "workspaceId": "uuid or null",
  "runtimeInstanceId": "uuid or null",
  "actor": {},
  "correlationId": "uuid or null",
  "parentEventId": "uuid or null",
  "sequence": 0,
  "payload": {}
}
```

The Go backend owns runtime validation/persistence of this protocol. Public HTTP representations are documented through intentional API/OpenAPI contracts. Exact field naming may evolve before release, but these concepts should remain stable.

## Ordering

Events are append-only and ordered per Run.

For v0.1:

- assign a monotonically increasing `sequence` within each Run
- persist before or atomically with publishing to live subscribers
- clients use sequence to order/reconcile reconnects
- duplicate live delivery must be harmless; Event `id` is the idempotency key

Do not rely only on wall-clock timestamps for ordering.

Project-level audit Events without a Run may use their own ordered stream or database ordering, but Run execution ordering must be deterministic.

## Persistence and live delivery

The database is authoritative.

Preferred v0.1 flow:

```text
producer
  -> validate Event contract
  -> redact sensitive content
  -> persist Event
  -> publish to live subscribers
  -> Nuxt client receives over SSE
```

On browser load/reconnect:

1. fetch persisted history
2. open SSE stream from the last known sequence/Event
3. de-duplicate by Event ID
4. append new Events in sequence order

The UI must never depend on browser memory or Nuxt/Nitro process memory for durable history.

## Event families

The initial protocol should support these families.

### Issue

```text
issue.created
issue.updated
issue.assigned
issue.status_changed
```

### Run

```text
run.created
run.queued
run.started
run.paused
run.resumed
run.waiting_for_input
run.ready_for_review
run.completed
run.failed
run.cancelled
```

### Runtime

```text
runtime.provisioning
runtime.started
runtime.stopping
runtime.stopped
runtime.failed
runtime.destroyed
```

### Agent

```text
agent.started
agent.message
agent.status
agent.question_requested
agent.completed
agent.failed
```

Do not attempt to persist private hidden chain-of-thought. Store user-visible agent messages, structured summaries, plans where explicitly emitted, Decisions, tool activity, and other operationally relevant information.

### Question

```text
question.created
question.answered
question.cancelled
```

The `question.created` payload includes the structured options/recommendation. The `question.answered` payload records the chosen option(s) or text answer and actor attribution.

Secret values are never included.

### Decision

```text
decision.recorded
```

### Tool / command

```text
tool.started
tool.output
tool.completed
tool.failed
```

Typical payload metadata:

- tool kind
- command/tool name
- sanitized arguments where safe
- working directory
- start/end/duration
- exit code
- output blob references
- summary

Large stdout/stderr belongs in blob storage rather than oversized Event payloads.

### File

```text
file.created
file.modified
file.deleted
file.renamed
```

Payloads should identify paths and lightweight change metadata. Large file contents/patches may be stored as blob references.

### Test

```text
test.started
test.completed
test.failed
```

Prefer structured counts/results where adapters can provide them.

### Git

```text
git.branch_created
git.commit_created
git.push_completed
```

GitHub/PR integration can extend this family later.

### Artifact

```text
artifact.created
artifact.deleted
```

### Review

```text
review.requested
review.approved
review.changes_requested
```

Human approval is an explicit attributable Event.

### Audit / permissions / secrets

```text
permission.requested
permission.granted
permission.denied
secret.requested
secret.injected
secret.revoked
```

These Events describe use and authorization only. They never contain the secret value.

### Delegation (future squads)

```text
delegation.created
delegation.completed
delegation.failed
```

Delegation carries parent/child Run relationships so the execution tree is reconstructable.

## Payload contracts

Each Event type should have an explicit Go-side payload contract/validation path and an intentional public representation where the Event is exposed through the API.

Prefer typed/discriminated event handling rather than one unstructured payload bag throughout the application.

Unknown/new Event types must not crash historical viewers. The Nuxt frontend should have a safe fallback renderer for Events whose type/schema is newer than the current UI understands.

## Versioning

- The envelope has a `schemaVersion`.
- Breaking persisted payload changes require an explicit compatibility strategy.
- Prefer additive payload changes where possible.
- Event type names are stable protocol identifiers, not UI labels.

## Redaction

Redaction occurs before persistence.

Never persist:

- API keys
- passwords
- access tokens
- private keys
- secret environment variable values
- credentials returned by providers

When possible, represent secret access as metadata:

```json
{
  "secretRef": "npm-token",
  "action": "injected"
}
```

not the value.

Runtime and Engine adapter output must pass through a centralized redaction layer before being persisted as raw or structured logs.

## Raw logs

Structured Events do not replace raw logs.

Raw channels may include:

- stdout
- stderr
- Agent protocol stream
- Runtime implementation logs
- adapter diagnostics

Store raw data in chunked blob storage and reference chunks from metadata/Events. Apply the same secret-redaction rules before persistence.

## Correlation and causality

Use `correlationId` and `parentEventId` where useful.

Example:

```text
tool.started (A)
  -> tool.output (parent A)
  -> tool.completed (parent A)
```

For future squads:

```text
delegation.created
  -> child Run created
      -> child Events
```

The Issue timeline may flatten these Events visually, but the underlying relationships remain available.

## Retention

Retention policy can evolve independently by data class.

Recommended architecture:

- structured Events: long-lived / effectively durable
- security/audit Events: long-lived
- raw logs: configurable retention
- artifacts: configurable retention
- Workspace: separate cleanup policy

Retention deletes must themselves be auditable where relevant.

## v0.1 minimum

The walking skeleton only needs a compact subset to prove the system:

```text
issue.assigned
run.created
run.started
runtime.provisioning
runtime.started
agent.message
tool.started
tool.completed
file.modified
question.created
run.waiting_for_input
question.answered
run.resumed
run.ready_for_review
runtime.stopped
```

Implement that subset well before expanding the vocabulary.
