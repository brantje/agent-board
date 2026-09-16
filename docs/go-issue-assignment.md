# Issue assignment and execution

Issue ownership, Board workflow state and Agent execution are separate lifecycle concepts.

The canonical ownership endpoint is:

- `POST /api/projects/{projectID}/issues/{issueID}/assignment`

`issueID` is the public Issue key. The request is the generic ownership contract:

```json
{
  "assignedTo": {
    "type": "AGENT",
    "id": "<uuid>"
  }
}
```

`type` may be `USER`, `AGENT`, or `SQUAD`. Use `{ "assignedTo": null }` to clear ownership. A Squad remains the persisted owner; its current leader Agent is resolved only when execution is created or reconciled.

## Ownership command

Assignment changes only the Issue owner. It does not imply a Board transition and it does not cancel or otherwise mutate an existing Run.

The authoritative command:

1. resolves the Issue under its Project;
2. validates the requested owner using the shared assignee-eligibility rules;
3. preserves the current Board status;
4. persists the ownership change and canonical `issue.assigned` Event atomically; and
5. applies the shared automatic-enqueue trigger after the ownership mutation.

Repeating the current ownership is a no-op. It does not append another assignment Event or create another Run.

Eligible Users are active effective Project members/admins, including inherited Group grants and implicit deployment-admin access. Eligible Agents are enabled and visible in the Project. Agent execution configuration is deliberately not part of ownership eligibility: an Issue may remain owned by an Agent whose model/provider configuration cannot currently create a Run.

## Automatic execution after Agent or Squad assignment

Changed Agent or Squad ownership is an execution trigger in every current Board status except `BACKLOG`. Squad ownership remains persisted as the Squad ID; Run creation resolves the Squad's current leader Agent.

- `BACKLOG` parks the Agent- or Squad-owned Issue without creating a Run.
- `TODO`, `IN_PROGRESS`, `BLOCKED`, `REVIEW` and `DONE` attempt normal Run creation for the direct Agent owner or resolved Squad leader.
- User ownership and unassignment never auto-enqueue.

Run creation still requires valid execution configuration for the resolved execution Agent. When that configuration is invalid or unavailable, the ownership mutation succeeds and no Run is created. Configuration reconciliation later retries eligible Agent- and Squad-owned work through the same execution path.

Scheduler availability is a separate boundary. Runner connectivity, repository-source availability and concurrency/capacity admission do not invalidate ownership and do not suppress creation of an otherwise valid Run. A successful enqueue persists a normal `QUEUED` Run, `run.created` Event and durable `START` scheduler job; the scheduler may then leave that Run queued until admission becomes possible.

## Board status triggers

Status changes do not reuse “any non-BACKLOG assignment” semantics.

For an already Agent- or Squad-owned Issue, automatic enqueue occurs only when the Issue leaves `BACKLOG` for one of:

- `TODO`
- `IN_PROGRESS`
- `BLOCKED`
- `REVIEW`

`BACKLOG -> DONE` does not auto-enqueue. Changes between non-BACKLOG statuses do not auto-enqueue. Status mutation is otherwise independent of existing Runs and never implicitly cancels them.

## Reassignment and active Runs

Ownership changes never rewrite execution history.

- Agent A -> Agent B preserves A's existing Runs and may create a new Run for B when the assignment trigger and execution configuration allow it. Squad A -> Squad B likewise preserves prior Runs and resolves Squad B's current leader for any new Run.
- assigning the same owner is a no-op; active Run suppression remains scoped to Issue + resolved Agent.
- switching to a User or clearing ownership preserves any active Agent Run and prevents future ownership-based reconciliation for that Issue;
- assignment never synthesizes `run.cancelled`.

Explicit cancellation remains a Run operation.

## Explicit Start Run

Explicit execution is a separate command from ownership:

- `POST /api/projects/{projectID}/issues/{issueID}/runs`

It uses only the Issue's current executable owner: a direct Agent owner, or a Squad owner resolved to its current leader Agent. `BACKLOG`, User-assigned and unassigned Issues are rejected; all other current Board statuses, including `DONE`, are eligible when execution configuration is valid.

Explicit Start Run preserves Issue ownership and Board status, including persisted Squad ownership. Scheduler availability does not block creation of the `QUEUED` Run and `START` job, and an already-active Run for the same Issue/resolved-Agent pair is not duplicated.

## Durable evidence

PostgreSQL is authoritative for ownership, Run creation, scheduler jobs and lifecycle Events. Product history comes from canonical persisted Events; operational diagnostics belong in structured logging and are not a second lifecycle record.
