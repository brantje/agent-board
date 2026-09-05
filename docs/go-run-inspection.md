# Go Run inspection read migration

This slice moves the existing Run scheduling and Runtime inspection read subresources from the TypeScript fallback to the native Go backend while keeping all execution authority in TypeScript.

## Go-owned routes

- `GET/HEAD /api/projects/:projectId/runs/:runId/scheduling`
- `GET/HEAD /api/projects/:projectId/runs/:runId/runtime`
- native `OPTIONS` for both route shapes

The existing core Run read routes remain Go-owned. Run creation, `start`, `resume`, generic transitions, scheduler claiming/leases, worker recovery, Runtime Instance lifecycle, Workspace execution, and engine/provider execution remain TypeScript-owned.

## Scheduling view parity

The Go scheduling view reads canonical PostgreSQL state only. It first verifies the Run by both Project and Run identity, then reads the newest active `run_execution_jobs` row in `PENDING`, `CLAIMED`, or `RELEASING` state.

The public projection preserves the TypeScript contract:

- no active job -> `scheduled: false` with `kind`, `state`, `queuedAt`, and `reason` all `null`
- `START` -> `kind: "start"`; `RESUME` -> `kind: "resume"`
- `PENDING`, `CLAIMED`, and `RELEASING` map to `pending`, `claimed`, and `releasing`
- persisted queue reason code/details are returned unchanged
- a pending job with no explicit queue reason returns `awaiting_worker` with an empty details object
- cross-Project or missing Runs return `run_not_found`

This code never inserts, claims, heartbeats, releases, finishes, or otherwise mutates `run_execution_jobs`. The TypeScript scheduler/worker remains the only authoritative execution system.

## Runtime view parity

The Go Runtime view first verifies Project-scoped Run ownership, then reads the latest durable `runtime_instances` row for that Run. If no Runtime Instance has ever been persisted, the response remains `{ "runtime": null }`.

The response preserves the existing `RunRuntimeView` projection: Runtime Instance id, provider, state, image, command, working directory, resource limits, Workspace mount policy, network policy, secret references, and lifecycle timestamps.

Persisted Runtime Spec fields that are not part of the public inspection contract stay private. In particular, the Go projection does not return Runtime environment values or labels. It also does not expose the provider-internal external Runtime id.

## Authority boundary

PostgreSQL remains durable truth for both views. This migration does not introduce an in-memory queue, second scheduler, lease owner, execution state machine, or Runtime lifecycle implementation in Go. TypeScript may continue mutating scheduler and Runtime Instance rows while Go serves these read-only views from the same canonical database.
