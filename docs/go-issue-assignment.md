# Go Issue assignment ownership

The Go strangler backend owns the Issue assignment command:

- `POST /api/projects/:projectId/issues/:issueId/assignment`
- native `OPTIONS` preflight for that exact route

Other Issue, Run, Review, Question, Workspace, Runtime, Engine, and worker routes retain their existing ownership unless another migration document marks them Go-owned. The TypeScript Run worker remains the single authoritative execution worker in this slice.

## Command behavior

The request remains the strict `{ "agentId": <uuid> }` contract. Project, Issue, and Agent lookups preserve the existing Project/shared scope rules and the existing `project_not_found`, `issue_not_found`, `issue_done`, `agent_not_found`, `agent_unavailable`, and `execution_configuration_invalid` errors.

Assignment reuses the Go execution-configuration checks already used by Review continuation. The Agent must be enabled, have an accessible Executor Profile, resolve its Model/Profile/Provider and Runtime Profile/Runtime within scope, and resolve a currently supported executable configuration. This does not move Engine execution into Go.

A successful first assignment atomically:

1. assigns the Agent to the Issue and appends `issue.assigned`;
2. checks unresolved `blocks`/`depends_on` relationships;
3. when workflow policy forbids starting blocked work, moves the Issue to `BLOCKED`, clears `execution_status`, and returns `run: null`;
4. otherwise moves the Issue to `IN_PROGRESS`, creates the next `QUEUED` Run attempt, appends `run.created`, and inserts the durable `START` execution job; and
5. projects the Issue execution status to `QUEUED` through the same durable scheduling transaction.

PostgreSQL is authoritative for the entire command. No in-memory assignment queue or second execution worker is introduced.

## Idempotency and reassignment

Assigning the same Agent again while that Agent already owns the latest active Run returns the existing Run instead of creating another attempt. A queued Run is re-enqueued idempotently through the durable scheduling table, so a missing scheduling intent can be repaired without duplicating the Run.

When assignment changes to a different Agent, the previous active Run is fenced before the replacement is created. Pending execution jobs are finished as `replaced`; claimed/releasing jobs receive `cancel_requested_at`, which the authoritative TypeScript worker observes through its existing heartbeat/lease protocol. Open Questions for the replaced Run are cancelled and a `run.cancelled` Event is appended. The replacement Run then receives its own durable `START` job.

This preserves the strangler invariant that Go may own the scheduling decision while TypeScript remains the only process that claims and executes Run jobs.

## Migration boundary

This slice does not migrate Run claiming, leases, heartbeats, recovery, Engine execution, Runtime materialization, automatic Workspace orchestration, or process cancellation into Go. Those remain TypeScript-owned until their own parity and durability work is proven. The compatibility fallback remains required for those capabilities.
