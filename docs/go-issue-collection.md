# Go Issue API ownership

The Go backend owns the Project Issue collection, individual Issue, assignment, execution-state, Start Run, and relationship boundaries used by the current product.

## Go-owned Issue routes

- `GET/HEAD /api/projects/:projectId/issues`
- `POST /api/projects/:projectId/issues`
- `GET/HEAD /api/projects/:projectId/issues/:issueId`
- `PATCH /api/projects/:projectId/issues/:issueId`
- `POST /api/projects/:projectId/issues/:issueId/assignment`
- `GET /api/projects/:projectId/issues/:issueId/execution-state`
- `POST /api/projects/:projectId/issues/:issueId/runs`
- Issue relationship list/create/delete routes
- native `OPTIONS` handling for these Go-owned paths

PostgreSQL is authoritative for Issue state, ownership, Run creation, scheduler jobs, and Events. There is no TypeScript fallback ownership model for these Issue operations.

## Creation

Issue creation allocates the Project-scoped Issue number and inserts the Issue in one PostgreSQL transaction. The public create request contains Issue metadata only: title, description, status, and priority. It does not accept an assignee.

New Issues are therefore unassigned through the HTTP creation route. Ownership is changed separately through the assignment endpoint. Internal application/store callers may create an Issue with ownership when required by an already-authoritative workflow; those callers still use the shared enqueue policy.

## Partial updates and explicit Board status

`PATCH` accepts any non-empty subset of:

- `title`
- `description`
- `status`
- `priority`

Field presence is preserved through the HTTP and application boundaries. PostgreSQL locks the current Issue row, applies only the fields supplied by the request, and emits the canonical mutation Event in the same transaction. A metadata-only PATCH therefore cannot restore a stale status observed before a concurrent explicit status change.

Board status is an explicit Issue workflow decision. There are no protected Review/DONE transition gates and no optimistic full-Issue compare-and-swap contract. Run completion, Review approval, Question waiting/resume, assignment changes, and scheduler state do not project Board status.

A no-op PATCH commits without producing a mutation Event.

## Ownership and automatic Run creation

Issue ownership is exactly one User, one Agent, or nobody. Ownership changes do not change Board status and never cancel Runs.

User assignees are active Users whose effective Project role is `member` or `admin`. Effective access is the highest direct/group Project role, with deployment administrators receiving effective Project admin access. The assignee directory and assignment validation use the same authoritative effective-role query.

Enabled Project-visible Agents are valid owners independently of execution readiness. An Agent can remain assigned while its Engine, Model Profile, or Provider configuration is unavailable.

The shared automatic enqueue matrix is:

- creating an internally Agent-owned Issue: no Run in `BACKLOG`; other statuses may enqueue
- changing ownership to an Agent: no Run in `BACKLOG`; other statuses may enqueue
- status mutation on an already Agent-owned Issue: only leaving `BACKLOG` for `TODO`, `IN_PROGRESS`, `BLOCKED`, or `REVIEW` may enqueue
- `BACKLOG -> DONE` does not enqueue
- other non-Backlog status transitions do not enqueue
- explicit Start Run is rejected in `BACKLOG` and otherwise uses the current Agent owner

The same Issue/Agent pair is transactionally deduplicated while an active Run exists. Different Agents may have independent active attempts for one Issue.

## Execution configuration versus scheduler admission

A Run can be created only when the current Agent configuration is execution-valid:

- Agent is enabled and visible to the Project
- Agent Engine is registered/supported by the server/runner protocol
- Model Profile is enabled and visible to the Project
- Provider is enabled and not unhealthy

Runner connection, Runner/source availability, Agent/Model capacity, and Workspace admission are scheduler concerns. They are deliberately not Run-creation checks. A valid configuration may therefore create a durable `QUEUED` Run even when no Runner is currently available.

The execution-state read, automatic enqueue, explicit Start Run, and configuration-recovery reconciliation all use the same execution-validity predicate.

## Events and concurrency

Issue mutation, ownership mutation, Run creation, and scheduler START-job creation are transactional with their canonical Events. Events are durable product history; `slog` is diagnostic only.

Status-change Events carry `previousStatus` explicitly in their payload. The transient Go `Issue` model does not carry a `PreviousStatus` field.

Issue relationships remain Project-scoped canonical records with source/target non-self validation, relationship-type validation, and uniqueness for `(project_id, source_issue_id, target_issue_id, type)`.

## HTTP validation

Issue creation and update retain strict request decoding: unknown fields are rejected, title/description/status/priority use the canonical validation rules, and PATCH requires at least one editable field. Missing or cross-Project resources preserve the normal not-found isolation behavior.
