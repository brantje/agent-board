# Go Run scheduling and admission

The Go backend owns durable Run creation, scheduler jobs, admission, lease/recovery state, and execution orchestration for the current product. PostgreSQL is the authoritative queue; there is no TypeScript RunWorker or parallel scheduler authority.

## Issue-level Start Run

The public explicit start command is:

- `POST /api/projects/:projectId/issues/:issueId/runs`

It uses the Issue's current Agent assignee. The command preserves Issue ownership and Board status.

Start Run is rejected when:

- the Issue is `BACKLOG`
- the Issue is unassigned or User-owned
- the current Agent execution configuration is unavailable

All other Board statuses, including `DONE`, are eligible. An existing active Run for the same Issue/Agent pair is returned instead of creating a duplicate.

Successful creation atomically persists:

- the new `QUEUED` Run
- its durable `START` scheduler job
- the canonical `run.created` Event

Run creation does not require a connected Runner, materialized source, or free Agent/Model capacity.

## Execution configuration boundary

Run creation validates the shared execution-configuration predicate: enabled/visible Agent, registered Engine, enabled/visible Model Profile, and enabled/non-unhealthy Provider.

Runner/source availability, Workspace serialization, Runner capacity, Agent concurrency, and Model Profile capacity belong to scheduler admission. Keeping those concerns separate allows valid work to queue durably while infrastructure is temporarily unavailable.

Automatic enqueue, explicit Start Run, execution-state reads, and configuration-recovery reconciliation use the same execution-validity policy.

## Durable scheduler

`scheduler_jobs` is the durable execution queue. Production scheduler workers use `AdmitNextJob`; there is no claim-only compatibility path.

Admission performs the authoritative scheduling decision in one PostgreSQL transaction. It:

1. locks the next eligible queued job and Run with `SKIP LOCKED`;
2. confirms the persisted Agent/Model relationship still exists;
3. enforces Issue Workspace exclusivity against claimed peer Runs and live Execution Sessions;
4. checks Agent and Model Profile capacity;
5. selects an eligible connected Runner according to Project Runner policy and advertised Engine capability;
6. reserves Runner, Agent, and Model capacity;
7. creates the scheduler lease;
8. marks the job `CLAIMED`; and
9. moves the Run from `QUEUED` to `STARTING`.

If configuration, Workspace, Runner, or capacity is temporarily unavailable, the queued job receives a durable wait reason/backoff and holds no accidental execution ownership.

## Leases and recovery

Lease renewal is scoped to the Project/job/token and serializes with the Run row. Expired claims are recovered through the scheduler reconciliation path, which re-evaluates persisted configuration and either restores queued work or resolves the durable Run/job state according to the observed execution outcome.

Scheduler restart does not depend on in-memory wakeups or ownership. Queued jobs, leases, capacity reservations, Runs, Workspaces, Execution Sessions, and Events are durable PostgreSQL state.

## Questions and continuation

A native blocking Question may move a Run to `WAITING_FOR_INPUT` while the same Runner, Execution Session, native Engine session, and Workspace remain attached. Answering the Question continues that same execution path when possible; it does not project Issue Board status.

Review Request Changes is a separate continuation command. It completes the reviewed attempt and creates the next queued Run/START job transactionally, preserving the reviewed Agent and Workspace and using the Issue-wide attempt allocator. The continuation emits its own canonical `run.created` Event.

## Board-state independence

Run and scheduler lifecycle never implicitly mutate the Issue Board status. Run completion, failure, Review approval, Review Request Changes, Question waiting/resume, scheduler admission, and scheduler recovery are independent from explicit Issue status mutations.
