# Scheduler and capacity

The scheduler is part of the v0.1 critical path. It is backend-owned, PostgreSQL-backed and independent from browser/request lifetime.

## Command vs execution

Starting/assigning work persists a Run and durable scheduling intent, then returns promptly.

```text
HTTP/UI command
 -> persist QUEUED Run + execution job
 -> return
 -> scheduler claims/admission
 -> select eligible connected Runner
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
- Agent/Model capacity reservations
- selected Runner / Execution Session ownership
- restart/reconciliation state

Process-local worker counts/semaphores may optimize but are never authoritative. Live Runner connectivity is ephemeral, but the durable Execution Session prevents transport loss from becoming duplicate work.

## Capacity constraints

### Agent concurrency

Each Agent may define a concurrency limit. Only actively executing Runs consume Agent capacity.

Runs waiting for human input, in Review, terminal or cancelled do not consume an Agent slot.

### Model Profile capacity

A Model Profile may define optional `max_concurrent` capacity.

- unset/null = unlimited
- otherwise integer >= 1

Capacity belongs to Model Profile, not Provider.

### Runner capacity

A Runner advertises `max_active_sessions`. v0.1 enforces one active Execution Session per Runner.

The scheduler admits a Run only against a live authenticated Runner that advertises the Agent's Engine, satisfies Project runner policy, and is below active-session capacity. External persistent Runners are preferred. The server-managed internal Runner is fallback when `allow_internal_runner` is true. Persisted last-seen/capabilities never make a disconnected Runner eligible.

Runner placement is not Agent configuration. Agents select Engine and Model Profile; the scheduler selects the eligible Runner at execution time.

### Atomic admission

A Run may start only when all required constraints are available. Agent and Model capacity are reserved consistently in the same admission/claim path. Runner eligibility/capacity is checked by the same existing scheduler execution path; do not add a second scheduler or Runner-specific queue.

Capacity exhaustion commits only queue/wait metadata and creates no duplicate execution ownership.

## Queue reasons

A queued Run exposes a machine-readable reason, for example:

- agent capacity exhausted
- model capacity exhausted
- configuration/preflight unavailable
- repository/source prerequisite unavailable
- runner capacity exhausted / no eligible live Runner

Capacity-only waiting remains Run `QUEUED`; it does not force Issue `BLOCKED`.

## Release, waiting for input and reacquisition

Agent and Model Profile capacity is released when active model execution ends, fails, cancels, pauses for human input, or loses/reconciles a lease according to scheduler rules.

That capacity release does **not** imply releasing a live Runner Execution Session. For a native blocking Question such as OpenCode's interactive Question flow, the same Runner, Execution Session, process tree and Runner Workspace remain owned through `WAITING_FOR_INPUT`. The Runner therefore remains unavailable for another Execution Session while that native session is still alive.

Question resume continues that live native session when available. If execution genuinely ended and later continuation requires a new execution attempt/session, the existing scheduler reacquires Agent/Model and Runner capacity through the normal path.

## Human continuation durability

A Question answer or Review request-changes action may require execution continuation. The decision and the required continuation intent must be committed atomically or through an equivalent transactional outbox/job pattern.

A crash after the human decision is committed must not lose the future Run/resume.

Retrying the human command is idempotent and must not create duplicate attempts/jobs.

## Restart/reconciliation

On backend startup the scheduler reconciles persisted STARTING/RUNNING/leased work and Runner-owned Execution Sessions.

Goals:

- no permanent false RUNNING state
- no duplicate execution ownership
- no leaked capacity
- no stranded continuation
- Workspace contents preserved
- durable Event/reason explaining interruption/recovery

Lease expiry is only a reconciliation trigger. It is not evidence that external execution stopped. An expired owner is fenced with a fresh reconciliation lease while capacity remains reserved. Requeue is allowed only after reconciliation explicitly establishes that retry is safe.

A genuine `UNKNOWN` reconciliation outcome is fail-closed: the scheduler keeps ownership and required capacity reserved until a later reconciliation obtains positive evidence for `ACTIVE`, safe `RETRY`, or a terminal outcome. An attempt count or elapsed-time limit alone must not convert uncertainty into release/retry because the external execution may still be alive.

A Runner WebSocket disconnect follows the same principle. Disconnect time starts the configured reconnect grace (five minutes by default). Before that deadline the Execution Session remains uncertain/reconciling. If the same Runner reconnects and reports the durable active session, Agent Board reattaches. Only after the grace expires without reconciliation may the Execution Session become infrastructure-failed and existing Run retry behavior decide what happens next.

## Go v0.1 implementation boundary

The Go scheduler exposes one authoritative admission primitive: `SchedulerStore.AdmitNextJob`. It locks a queued scheduler job and its Run with PostgreSQL `FOR UPDATE ... SKIP LOCKED`, locks the selected Agent and Model Profile in deterministic order, checks their current reservations, and commits both reservations, the lease, the job claim, and the Run `STARTING` transition together. Capacity exhaustion commits only queue/wait metadata and creates no partial ownership.

Every post-admission Run transition is fenced by the current lease token. Agent/Model reservations are released according to Run-state policy; Runner Execution Session ownership is tracked separately so `WAITING_FOR_INPUT` may keep the same native execution alive without making the Runner schedulable for duplicate work.

`internal/scheduler.Coordinator` owns polling, lease heartbeats, restart reconciliation and bounded process-local backpressure. Its process-local concurrency bound is not an admission authority; PostgreSQL remains authoritative. The coordinator requires narrow execution `Processor` and external-state `Reconciler` dependencies, so Runner transport, legacy Docker/runtime details and Engine implementation details do not enter the scheduler package while reconciliation can never silently degrade to permanent `UNKNOWN` because a dependency was omitted.

Issue #8 deliberately did not activate the coordinator with a placeholder processor. Runner/Engine work was excluded from that scheduler slice; later execution issues supplied the real processor and reconciler and start the coordinator with the server lifecycle. Claiming work without real execution/reconciliation dependencies would create false ownership and is therefore prohibited.

## Assignment changes

Changing an active Issue assignee cancels the current Run according to product rules and schedules a new attempt through the same scheduler. The Issue Workspace is reused.

## Delegation / Automation compatibility

Future delegated work and Automation-created Issues use this same scheduler. They do not create private queues.

## Invariants

1. PostgreSQL owns scheduling truth.
2. A Run is claimed at most once for active execution ownership.
3. Agent/Model admission constraints compose atomically; Runner placement uses the same scheduler path.
4. Capacity waits remain queue state, not board blockers.
5. Human continuation cannot be lost after a successful decision commit.
6. Browser/backend request lifetime never owns execution.
7. Restart or Runner reconnect recovery does not require process-local state to remain alive and must not start duplicate Engine work while ownership is uncertain.
