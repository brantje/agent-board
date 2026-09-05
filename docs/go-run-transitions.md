# Go Run transition migration

This slice moves the generic Run state transition command from the TypeScript fallback to the native Go backend while deliberately leaving Run scheduling and execution authority in TypeScript.

## Go-owned route

- `POST /api/projects/:projectId/runs/:runId/transition`
- native `OPTIONS` preflight for that exact route

Run reads, inspection views, and Run creation remain Go-owned from earlier slices. `start`, `resume`, durable scheduling/claiming, scheduler leases, worker recovery, Runtime Instance lifecycle, Workspace execution, engines, and providers remain TypeScript-owned.

## HTTP contract

The native handler preserves the existing public behavior:

- validates Project and Run ids as UUIDs
- accepts the existing Run statuses except `QUEUED`
- ignores unknown JSON object properties, matching the current Zod object contract
- returns the updated Run with `200`
- returns `run_not_found` with `404` for missing or cross-Project Runs
- returns `invalid_run_transition` with `409` when the requested state edge is not allowed
- preserves the existing validation and internal-error envelopes

`QUEUED` remains intentionally unavailable through this generic command. A queued Run is durable scheduling state and must only be created by the dedicated start/resume scheduling flow, which remains TypeScript-owned.

## Transaction and state-machine parity

PostgreSQL remains the durable source of truth. Each transition runs in one database transaction and locks the target Run row before evaluating the state edge.

The Go store preserves the canonical transition matrix:

- `QUEUED -> STARTING | CANCELLED`
- `STARTING -> RUNNING | FAILED | CANCELLED`
- `RUNNING -> WAITING_FOR_INPUT | PAUSED | READY_FOR_REVIEW | COMPLETED | FAILED | CANCELLED`
- `WAITING_FOR_INPUT -> QUEUED | PAUSED | CANCELLED | FAILED`
- `PAUSED -> QUEUED | CANCELLED | FAILED`
- `READY_FOR_REVIEW -> COMPLETED | CANCELLED`
- terminal Runs cannot transition again

`started_at` is set only on the first transition into `STARTING` or `RUNNING`. `completed_at` is set when transitioning into `COMPLETED`, `FAILED`, or `CANCELLED`.

For execution handoff states (`STARTING` and `RUNNING`), the owning Issue row is also locked and a Done Issue rejects the transition. This preserves the existing execution guard against starting work after an Issue has completed.

## Issue execution-status projection

The Run update and the Issue `execution_status` projection commit atomically. The Issue projection is updated only when:

- the Issue is assigned to the same Agent as the Run
- no newer Run attempt exists for that Issue
- a Done Issue is not being projected to a non-`COMPLETED` execution status

This prevents an older Run from overwriting the state projected by a newer attempt.

## Authority boundary

This migration does not create a Go scheduler, worker, lease owner, in-memory queue, Runtime execution loop, or recovery process. The TypeScript scheduler/worker remains authoritative for start/resume, claiming, concurrency, leases, restart recovery, Runtime lifecycle, and actual execution.
