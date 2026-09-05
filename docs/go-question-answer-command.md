# Go Question answer command

This migration slice moves the human Question-answer command to the Go gateway while preserving the strangler boundary around TypeScript Run execution.

## Go-owned behavior

Go owns:

- `GET/HEAD /api/questions`
- `GET/HEAD /api/projects/:projectId/issues/:issueId/questions`
- `GET/HEAD /api/projects/:projectId/questions/:questionId`
- `POST /api/projects/:projectId/questions/:questionId/answer`

The answer command preserves the existing contract and durable effects:

1. validate the strict discriminated Question-answer shape;
2. verify the Question belongs to the requested Project;
3. verify answer kind and choice IDs against the persisted Question;
4. lock the Question row and change only `OPEN` Questions to `ANSWERED`;
5. persist human API provenance and `answered_at`;
6. merge `questionAnswer` into the existing Run `executor_resume` JSON without discarding unrelated resume state;
7. append durable `question.answered` and `decision.recorded` Events to PostgreSQL;
8. when the answered Question is blocking, no other blocking Question remains, the Run is `WAITING_FOR_INPUT`, and Project workflow policy enables automatic continuation, wait for the previous execution claim to settle and atomically queue the same Run with a durable `RESUME` execution job.

A newly inserted scheduler job continues to create its canonical `run.queued` Event through the existing PostgreSQL trigger. The TypeScript `RunWorker` polls the shared durable scheduler table and remains authoritative for claiming, leases, concurrency, recovery, Runtime lifecycle, engines, and execution.

Automatic resume scheduling failures are logged after the Question answer is durable and do not change a successful answer response, matching the existing TypeScript behavior.

## Deliberately still TypeScript-owned

Question creation remains TypeScript-owned in this slice. The authoritative TypeScript `RunExecutor` directly creates structured Questions when an engine requests human input. Moving only the HTTP creation route to Go would leave two live production implementations of the same mutation while execution is still TypeScript-owned, so creation stays on the compatibility fallback until the Run execution subsystem migrates.

The TypeScript backend therefore still owns:

- `POST /api/projects/:projectId/runs/:runId/questions`;
- engine-driven Question creation;
- the blocking transition to `WAITING_FOR_INPUT` and Issue `BLOCKED` state performed during that creation path;
- the authoritative Run worker/executor that later consumes the durable Go-created RESUME job.

This boundary avoids duplicate authoritative Question creation and does not introduce an in-memory Go queue or a second Run worker.
