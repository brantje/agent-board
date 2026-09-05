# Go Run scheduling command migration

This slice moves the public Run `start` and `resume` scheduling commands from the TypeScript fallback to the native Go backend while deliberately leaving claim/lease/concurrency/recovery and actual Run execution with the existing TypeScript `RunWorker`.

## Go-owned routes

- `POST /api/projects/:projectId/runs/:runId/start`
- `POST /api/projects/:projectId/runs/:runId/resume`
- native `OPTIONS` preflight for those exact routes

Core Run reads, inspection views, creation, generic transitions, and these scheduling commands are Go-owned. Scheduler claiming, queue-reason refresh, leases/heartbeats, stale-claim recovery, stranded-queue recovery, Runtime Instance lifecycle, Workspace execution, engines, and execution orchestration remain TypeScript-owned.

## Durable scheduling boundary

PostgreSQL remains the authoritative queue. Go writes the existing `run_execution_jobs` table; it does not introduce an in-memory queue or a second worker.

The existing database trigger on `run_execution_jobs` inserts the canonical `run.queued` Event in the same transaction as a newly created scheduler job. The TypeScript `RunWorker` continues polling the shared durable scheduler table and remains the only process that claims and executes jobs.

This means moving the HTTP scheduling command does not create duplicate execution authority. At most it removes the old in-process `worker.wake()` optimization; the authoritative TypeScript worker still polls on its existing interval.

## Start contract

`POST .../start` preserves the existing behavior:

- validates Project and Run ids
- returns `project_not_found` for a missing Project
- returns `run_not_found` for a missing/cross-Project Run or missing owning Issue
- rejects a Done Issue with `issue_done`
- requires the Run to be `QUEUED`
- coalesces onto an existing active durable execution job for the Run
- otherwise inserts one `START` job atomically
- projects the owning Issue `execution_status` to `QUEUED` only for the matching Agent/latest attempt
- returns the Run with `202 Accepted`

## Resume contract

`POST .../resume` preserves the public behavior while making its persistence boundary crash-safe:

- accepts `WAITING_FOR_INPUT` and `PAUSED`
- remains idempotent if a previous resume already transitioned the Run to `QUEUED` and an active `RESUME` job exists
- rejects open blocking Questions for that Project/Run with `unresolved_blocking_question`
- waits for the previous active scheduler job to finish before queueing the resume, using the existing 500 x 10 ms settle window
- re-checks Run state, Issue state, blocking Questions, and active scheduler work inside the resume transaction
- transitions the Run to `QUEUED`, projects Issue `execution_status`, moves a still-`BLOCKED` Issue to `IN_PROGRESS`, and inserts the durable `RESUME` job in one PostgreSQL transaction
- rolls the entire resume back if the job/event insert or any other write fails, so a retry cannot observe a stranded `QUEUED` Run without resume intent
- returns the queued Run with `202 Accepted`

Invalid state edges continue returning `invalid_run_transition` with `409`.

## Scheduler persistence parity

The Go store preserves the important enqueue invariants from the TypeScript scheduler repository:

- the target Run row is locked before queueability is checked
- only a `QUEUED` Run may receive a scheduling job
- only one active (`PENDING`, `CLAIMED`, or `RELEASING`) job exists per Run
- repeat start enqueue requests return that active job rather than inserting a duplicate
- resume idempotency coalesces only with an already-active `RESUME` job for a `QUEUED` Run
- Project scoping is enforced when resolving the Run
- Issue execution-status projection ignores older Run attempts and does not regress a Done Issue
- the `run.queued` Event remains database-triggered and atomic with a newly inserted job

## Authority boundary

This slice does **not** migrate `claimNext`, capacity/concurrency admission, queue-reason refresh, lease ownership, heartbeat, cancellation handoff, recovery, reconciliation, or Run execution. Those behaviors are still consumed from the TypeScript scheduler repository by the TypeScript `RunWorker`.

There must remain exactly one authoritative Run execution worker until the future Go scheduler/worker slice has complete concurrency, restart, stale-ownership, recovery, and duplicate-execution parity coverage.
