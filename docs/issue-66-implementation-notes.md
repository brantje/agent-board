# Issue #66 implementation notes

This branch replaces request-bound Run execution with a PostgreSQL-backed execution-job scheduler and server-owned worker.

## Invariants

- HTTP assignment/start/resume/review-change commands persist scheduling intent and return without owning Engine lifetime.
- `Run` remains execution-attempt identity. Internal worker leases are separate durable records.
- `WAITING_FOR_INPUT` resumes the same Run by moving it back through `QUEUED -> STARTING -> RUNNING`.
- Run transitions project `Issue.executionStatus` in the same transaction, guarded by current Agent/current attempt.
- PostgreSQL `FOR UPDATE SKIP LOCKED` is the race-safe claim point.
- Model Profile and Agent capacity are intentionally not implemented here; #64/#73 extend the same claim transaction.
- Before the execution fence, an expired claim is safe to requeue. After the execution fence, an expired owner fails the Run as `execution_interrupted` rather than replaying unknown side effects.
- Runtime cleanup/reconciliation preserves the durable Issue Workspace.
- A replacement Run may be queued while the prior execution releases, but the scheduler will not claim both against the same Issue concurrently.
- Worker/lease tokens are internal and must never become Run identity or public API fields.
