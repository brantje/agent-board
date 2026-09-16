# Scheduler and capacity

The scheduler is part of the v0.1 critical path. It is backend-owned, PostgreSQL-backed and independent from browser/request lifetime.

## Command vs execution

Issue ownership and Board status are persisted independently from scheduler admission. When an automatic lifecycle trigger, execution-configuration reconciliation, or explicit Start Run creates work, the backend persists a normal `QUEUED` Run and durable scheduling intent before returning.

```text
Issue mutation / explicit Start Run / configuration reconciliation
 -> validate execution configuration when a Run trigger applies
 -> persist QUEUED Run + execution job
 -> return
 -> scheduler claims/admission
 -> select eligible connected Runner
 -> STARTING
 -> RUNNING
```

Runner connectivity, repository-source availability and capacity are scheduler concerns after Run creation; they are not ownership or Board-status validity checks. No HTTP handler owns long-running execution lifetime.

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

A Runner advertises `max_active_sessions`. The default advertised capacity is 5 concurrent Execution Sessions per Runner. Missing or zero advertised capacity is treated as 5.

The scheduler admits a Run only against a live authenticated Runner that advertises the Agent's Engine, satisfies Project runner policy, and is below active-session capacity. External persistent Runners are preferred. The server-managed internal Runner is fallback when `allow_internal_runner` is true. Persisted last-seen/capabilities never make a disconnected Runner eligible.

External Runners have exactly two scopes. A shared Runner has no owner Project and remains deployment-global capacity subject to the existing `project_runners` allowlist. A Project-owned Runner stores exactly one owner Project and is eligible only for Runs from that Project; it never participates in the shared allowlist. Project ownership is checked again when a Runner-owned Execution Session is created, so a direct caller cannot bind a foreign Project-owned Runner even if scheduler admission is bypassed. The internal Runner remains deployment-global/server-managed and cannot be Project-owned.

An empty shared-runner allowlist retains its existing meaning: any eligible shared external Runner may execute. One or more allowlist rows restrict shared capacity to those Runners. Project-owned Runners are separate from that policy and require no allowlist row.

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

Issue ownership and Board status mutations never cancel Runs. Stopping execution requires an explicit Run command.

The shared `store.ShouldAutoEnqueueIssue` policy is applied inside the Issue mutation transaction:

- assigning/reassigning an Agent or Squad (including initial ownership on creation) enqueues in every status except `BACKLOG`; a Squad resolves its current leader Agent before normal Run creation;
- an already Agent-assigned Issue leaving `BACKLOG` enqueues for `TODO`, `IN_PROGRESS`, `BLOCKED` or `REVIEW`, but not `DONE`;
- all other status changes, User assignment and unassignment do not enqueue;
- unchanged ownership is an idempotent no-op, including after a prior Run has finished.

Assignment preserves Board status. An ownership-eligible Agent remains assigned even when its execution configuration cannot currently create a Run; the mutation succeeds without a pending-execution flag. Execution-configuration reconciliation uses the same enqueue path (see below).

The Issue row lock serializes automatic enqueue, pair-scoped active-Run suppression and Issue-wide attempt numbering. Different Agents can have active Runs on one Issue; automatic enqueue never creates a second active Run for the same Issue/Agent. All attempts reuse the authoritative Issue Workspace and existing scheduler jobs. Workspace execution ownership still serializes access to its checkout.

New automatic Runs atomically persist `run.created` with Run, Agent and Workspace identity. Duplicate suppression and execution-configuration skips create no Run Events. Issue mutation Events remain owned by the canonical Issue commands.

## Execution configuration recovery and Start Run

Configuration recovery derives work from current executable ownership (direct
Agent ownership or Squad ownership resolved to its current leader Agent), Board
status, active Issue/Agent Runs and the existing Agent/Model Profile/Provider
validity checks. It persists no pending-execution flag or recovery history.
Startup runs this reconciliation before starting the scheduler; configuration
updates and Provider recovery invoke the same reconciliation for affected ownership.

Recovery and explicit Start Run reuse the assignment eligibility policy: BACKLOG
stays parked; TODO, IN_PROGRESS, BLOCKED, REVIEW and DONE are eligible. Candidate
ownership is re-read under the Issue lock, so an intervening User assignment,
unassignment or Agent change cannot enqueue stale work. Active Runs for the same
Issue/Agent suppress duplicates. Different Agents retain their existing Runs.

`POST /api/projects/{projectID}/issues/{issueID}/runs` requires Project member
access, accepts no Agent selection, and preserves ownership and Board status.
Unassigned/User-assigned Issues, BACKLOG and invalid execution configuration
return conflict. A duplicate request returns the active Run while configuration
remains valid. After that Run is terminal, Start Run can create the next attempt.
Repeating the same assignment remains a no-op.

Both commands reuse the normal Workspace, queued Run, START job and durable
`run.created` transaction. Live Events publish after commit; rejected/skipped
attempts produce no lifecycle Events. Runner/source availability and capacity
never gate Run creation here: the existing scheduler owns those waits. Recovery
scans omit pairs with active Runs, including queued Runs waiting for admission.
Routine healthy Provider probes do not trigger assignment scans.

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
