# Scheduler and capacity

The scheduler is part of the v0.1 critical path. It is backend-owned, PostgreSQL-backed and independent from browser/request lifetime.

## Command vs execution

Starting/assigning work persists a Run and durable scheduling intent, then returns promptly.

```text
HTTP/UI command
 -> persist QUEUED Run + execution job
 -> return
 -> scheduler claims/admission
 -> STARTING
 -> RUNNING
```

No HTTP handler owns long-running execution lifetime.

## Durable ownership

PostgreSQL is authoritative for:

- queued jobs
- claim/lease ownership
- attempt/admission identity
- wait reasons
- capacity reservations
- restart/reconciliation state

Process-local worker counts/semaphores may optimize but are never authoritative.

## Capacity constraints

### Agent concurrency

Each Agent may define a concurrency limit. Only actively executing Runs consume Agent capacity.

Runs waiting for human input, in Review, terminal or cancelled do not consume an Agent slot.

### Model Profile capacity

A Model Profile may define optional `max_concurrent` capacity.

- unset/null = unlimited
- otherwise integer >= 1

Capacity belongs to Model Profile, not Provider.

### Atomic admission

A Run may start only when all required constraints are available. Agent and Model capacity are reserved consistently in the same admission/claim path so workers cannot over-admit or leak partial reservations.

## Queue reasons

A queued Run exposes a machine-readable reason, for example:

- agent capacity exhausted
- model capacity exhausted
- configuration/preflight unavailable
- repository/source prerequisite unavailable
- Runtime unavailable

Capacity-only waiting remains Run `QUEUED`; it does not force Issue `BLOCKED`.

## Release and reacquisition

Capacity is released when active execution ends, fails, cancels, pauses for human input or loses/reconciles a lease according to scheduler rules.

Question resume and other continuations reacquire capacity before execution resumes.

## Human continuation durability

A Question answer or Review request-changes action may require execution continuation. The decision and the required continuation intent must be committed atomically or through an equivalent transactional outbox/job pattern.

A crash after the human decision is committed must not lose the future Run/resume.

Retrying the human command is idempotent and must not create duplicate attempts/jobs.

## Restart/reconciliation

On backend startup the scheduler reconciles persisted STARTING/RUNNING/leased work.

Goals:

- no permanent false RUNNING state
- no duplicate execution ownership
- no leaked capacity
- no stranded continuation
- Workspace contents preserved
- durable Event/reason explaining interruption/recovery

## Assignment changes

Changing an active Issue assignee cancels the current Run according to product rules and schedules a new attempt through the same scheduler. The Issue Workspace is reused.

## Delegation / Automation compatibility

Future delegated work and Automation-created Issues use this same scheduler. They do not create private queues.

## Invariants

1. PostgreSQL owns scheduling truth.
2. A Run is claimed at most once for active execution ownership.
3. Admission constraints compose atomically.
4. Capacity waits remain queue state, not board blockers.
5. Human continuation cannot be lost after a successful decision commit.
6. Browser/backend request lifetime never owns execution.
7. Restart recovery does not require process-local state to remain alive.
