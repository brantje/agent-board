# Test-driven development

Agent Board uses TDD for backend, persistence, runtime orchestration, contracts and frontend behavior.

## Core loop

1. **Red** — write the smallest failing test for the desired behavior/bug.
2. **Green** — implement the minimum production change.
3. **Refactor** — improve structure with the relevant suite green.

Bug fixes begin with a regression test.

## Backend testing

The production backend is Go under `apps/server`.

Backend behavior should be covered with Go unit/integration tests. PostgreSQL-dependent behavior uses real PostgreSQL rather than mocked SQL.

Required concerns include:

- Project isolation
- scheduler claiming/leases/capacity/restart recovery
- Workspace repository bootstrap/reuse/path safety
- Runtime Instance lifecycle/cancellation/cleanup
- agent-runner WebSocket/session behavior
- canonical execution context
- secret non-disclosure
- immutable provenance
- raw-output/Artifact storage
- Question/Review continuation durability
- complete Review candidate evidence
- authentication/authorization negative cases

### Core Go commands

```bash
cd apps/server
go test ./...
go vet ./...
go build ./...
```

Runner changes also run the equivalent Go test/vet/build commands under `apps/agent-runner` once that package exists.

Use targeted package/test invocations during Red/Green iteration, then run broader relevant suites before completion.

Docker integration tests should be explicit/gated where they require a live Docker daemon.

## PostgreSQL

`packages/database/schema.sql` is the canonical pre-release schema.

For schema changes:

1. write/update failing integration tests for final behavior
2. edit `packages/database/schema.sql`
3. apply it to a fresh isolated PostgreSQL database
4. run relevant Go/database integration tests
5. recreate stale local development databases when necessary

Do not introduce a production upgrade framework merely to preserve pre-release local data.

## Frontend testing

Frontend targets Nuxt 4 + Vue 3 + TypeScript + Nuxt UI v4.

Test user-visible behavior, Vue components/composables, validation, routing/navigation, loading/empty/error/reconnect/permission states and Project isolation where practical.

Prefer behavior-focused Vue tests over implementation-detail snapshots. For interactive Nuxt UI components, test user interaction and accessibility behavior rather than generated class lists or internal Reka UI structure.

Run as applicable from repository root:

```bash
pnpm install
pnpm typecheck
pnpm test
pnpm build
```

If a dedicated frontend lint/theme/accessibility check exists, run it when relevant. Frontend policy follows `frontend-implementation.md` and `frontend-theme.md`.

## API/contracts

Go OpenAPI/public DTO behavior must have request/response/validation tests. Frontend types/consumers must remain aligned with intentional public API shapes.

Do not expose database row structs as accidental API contracts.

## Scheduler tests

At minimum cover:

- concurrent workers claiming one Run exactly once
- Agent concurrency 1 and N
- Model Profile capacity 1, N and unlimited
- combined constraints without partial leaks
- release on success/fail/cancel/pause/reconciliation
- browser/request disconnect independence
- backend restart reconciliation
- durable Question/Review continuation across crash immediately after decision commit

See `scheduler.md`.

## Workspace/source tests

For the first v0.1 path cover:

- real local fixture Git repository materialized before Engine execution
- local source path validation against authorized roots
- same Issue reuses Workspace across attempts
- Runtime Instance remains bound to exactly one Workspace for its lifetime
- same-Workspace Runtime Instance reuse does not reset/corrupt Workspace state
- replacement Runtime Instance can mount the same durable Workspace
- cross-Project Workspace/repository isolation
- bootstrap concurrency/retry safety
- repository failure does not silently create an unrelated empty repository

When remote Source Connections are implemented, extend coverage with authentication failure behavior and credential non-disclosure in Git config/logs/provenance.

See `source-control.md`.

## Runtime and agent-runner tests

Runtime/runner contract and integration tests cover:

- Runtime create/start/inspect/stop/destroy
- runner starts from the official Runtime image
- versioned WebSocket handshake and incompatible-version failure
- explicit Execution Session identity
- one active session per runner in v0.1
- multiple sequential sessions over one runner lifetime
- stdout/stderr channel fidelity
- stdin forwarding/close
- non-zero exit status
- one process tree per Execution Session
- `/workspace` working directory/write persistence
- same Workspace remains visible across sequential sessions
- cancellation/graceful/forced process-tree cleanup
- runner/session disconnect reconciliation without blindly duplicating execution
- cleanup after backend restart
- runner capability advertisement
- network/resource/policy enforcement
- secret values are not reflected in runner protocol output
- Docker socket absence inside Agent Runtime

A real-Docker flow should prove:

```text
Runtime Instance
 -> agent-runner connects
 -> Execution Session 1 writes Workspace
 -> session completes
 -> Execution Session 2 sees same Workspace state
 -> Runtime Instance destroyed
 -> Workspace survives
```

See `runtime-contract.md`, `runtime-execution.md` and `agent-runner.md`.

## Evidence/Review tests

At minimum prove:

- historical provenance unaffected by later config edits
- raw output is bounded/chunked and redacted
- Artifacts are first-class and Project scoped
- complete candidate includes staged + unstaged + new/untracked files
- deletes/renames represented correctly
- missing tests are not shown as success
- prior attempt evidence remains inspectable after later Workspace changes

See `execution-evidence.md`.

## Frontend architecture tests

Where practical, cover these boundaries:

- frontend reads/mutates through the intentional Go API contract
- Nuxt/Nitro server facilities do not become an alternate durable product-state path
- live Run/Issue UI reconnects to persisted API/SSE state after remount/reload
- Nuxt UI components preserve keyboard/focus/accessibility behavior
- generic controls use Nuxt UI instead of parallel custom primitives
- dark and light theme behavior remains complete when theme logic changes
- loading/error/empty/permission states are represented explicitly

## Security tests

Security-sensitive behavior requires negative tests first where practical.

Cover:

- cross-Project access
- forged actor headers/identity cannot gain privilege
- untrusted Runtime/runner cannot invoke privileged control-plane actions anonymously
- secret values absent from DB Events, runner responses, raw logs, Artifacts, provenance, HTTP responses and application logs
- path traversal
- unsafe source/server URLs/SSRF where applicable
- forbidden Docker/host access
- Runtime Instance cannot be rebound to another Workspace

## Definition of done

A change is not complete until:

- behavior was driven through Red/Green/Refactor where automatable
- meaningful regression/error/isolation/security cases are covered
- targeted tests pass
- relevant broader Go/frontend suites pass
- Go vet/build pass when backend or runner changed
- frontend typecheck/build pass when frontend changed
- schema applies cleanly to a fresh database when database structure changed
- Docker/integration suites pass when the change affects those boundaries

PR descriptions should state which tests were written first and which verification commands were run.

## v0.1 end-to-end gate

The repository must eventually have an integration path proving:

```text
Local Project repository
 -> Issue assignment
 -> durable scheduler
 -> Issue Workspace
 -> Runtime Instance
 -> agent-runner
 -> Execution Session
 -> real coding Engine
 -> real Workspace changes
 -> durable evidence
 -> Review-ready result
```

Green unit tests alone are not proof that the v0.1 flow is complete.
