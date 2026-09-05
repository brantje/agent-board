# Agent Runner

`agent-runner` is the small execution-plane binary that runs inside an Agent Board Runtime Instance. It provides a stable, Runtime-neutral process/session boundary between the trusted Go control plane and coding Engine processes such as OpenCode.

The runner is part of the v0.1 execution architecture. It is not an Agent, Run, Runtime, Runtime Instance or future Worker.

## Execution model

```text
Agent Board server
 -> Runtime implementation
 -> Runtime Instance
 -> agent-runner
 -> Execution Session
 -> Engine process
```

These identities are separate from the start:

```text
Runtime Instance != agent-runner != Execution Session != Run
```

Required relationships:

- one Runtime Instance is bound to exactly one Workspace for its lifetime
- one Runtime Instance has one active runner
- one runner may execute many Execution Sessions over time
- one Execution Session owns one process tree
- one Run may use one or more Execution Sessions
- one Run may use multiple Runtime Instances over its lifetime
- one Workspace may use multiple Runtime Instances over time

The Workspace binding of a Runtime Instance is immutable. v0.1 does not reuse one Runtime Instance across different Workspaces.

## Workspace reuse

The durable Issue Workspace remains authoritative and outlives Runtime compute.

```text
Workspace A
 -> Runtime Instance 1
      -> agent-runner
           -> Execution Session 1
           -> Execution Session 2
 -> Runtime Instance 2, if replacement is needed
      -> agent-runner
           -> Execution Session 3
```

A healthy Runtime Instance may remain available for later Execution Sessions against the same Workspace. This supports retries, follow-up commands, tests and later warm/fleet behavior without requiring cross-Workspace container reuse.

If a Runtime Instance is destroyed or becomes unusable, Agent Board may materialize a replacement Runtime Instance against the same durable Workspace.

## Transport

The server and runner communicate over WebSocket.

The protocol is explicitly versioned from day one and every execution message is scoped to a server-issued Execution Session identity. The wire protocol must support multiple sessions over the lifetime of one runner connection even when the configured concurrency limit is one.

The exact connection-initiation/registration mechanism and final wire schema are implementation details, but the protocol must support at least:

- runner handshake and protocol-version negotiation
- runner capability advertisement
- session start
- stdin streaming and stdin close
- stdout/stderr streaming with channel identity
- exit/result reporting
- graceful termination
- forced kill
- runner/session errors
- liveness/connection reconciliation

A WebSocket disconnect is an infrastructure signal, not by itself durable proof that a Run or Engine process failed. The server reconciles durable Run/Runtime state with external execution state.

## Concurrency and future fleets

v0.1 uses:

```text
max active Execution Sessions per runner = 1
```

The protocol and capability model must not assume that limit is permanent. A runner advertises its supported session capacity so future Runtime/fleet implementations may raise it without changing the identity model or transport contract.

Workspace write safety remains separate from runner transport concurrency. Supporting several protocol sessions does not imply that several authoritative writers may mutate one Workspace concurrently.

## Engine ownership

Engine adapters stay in the trusted server.

Engine adapters own:

- Engine-specific command/invocation construction
- protocol parsing
- Model Profile/Provider configuration materialization
- mapping visible Engine activity into canonical Agent Board evidence

The runner is Engine-neutral. It receives an authorized execution request and executes the resulting process tree inside its Runtime Instance.

This keeps Runtime implementations and `agent-runner` independent from OpenCode-specific behavior.

## Runner responsibilities

The runner owns only execution-plane behavior:

- accept authorized, versioned session requests
- enforce the provided Workspace-bounded working directory
- start and supervise one process tree per Execution Session
- apply execution-scoped environment/secret values passed by the trusted server
- stream stdin/stdout/stderr
- report exit status/result
- propagate cancellation and graceful termination
- force-kill the process tree when required
- isolate session state between executions
- expose runner capabilities/health needed by the server

The runner does not own:

- PostgreSQL or scheduler state
- Issue/Board workflow
- Agent concurrency or Model Profile capacity admission
- Project authorization
- Provider credential storage/decryption
- Review decisions
- durable Event/evidence persistence
- Docker/host orchestration

## Security boundary

`agent-runner` executes on the untrusted/contained side of the Runtime boundary.

The trusted server resolves and authorizes configuration and secrets before execution. The runner receives only execution-scoped data needed by the session.

The runner and Runtime Instance must never receive:

- PostgreSQL credentials
- backend encryption/signing keys
- Docker socket or daemon credentials
- broad control-plane credentials
- arbitrary host filesystem access outside authorized mounts

Secret values remain ephemeral. They must not be echoed in runner protocol responses and must be redacted before every durable server-side sink.

## Distribution

For v0.1, `agent-runner` ships in official Agent Board Runtime images. Runtime implementations start/connect to that runner rather than directly embedding Engine-specific process behavior.

Conceptual repository layout:

```text
apps/
├── server/
├── agent-runner/
└── web/
```

The initial Docker Runtime image contains the `agent-runner` binary plus the required baseline tooling/Engine installation.

## Questions and resume

Question/Decision/resume state remains durable on the server. The runner has no durable product-state responsibility.

A blocking Question may release or lose Runtime compute while the Workspace survives. When execution resumes, the server may reuse a healthy same-Workspace Runtime Instance or create a replacement Runtime Instance and start a new Execution Session with explicit resume context.

## Future Worker pools

A future Worker is compute capacity and remains separate from the runner:

```text
Run
 -> scheduler
 -> Worker/Pool
 -> Runtime Instance
 -> agent-runner
 -> Execution Session
 -> Engine
```

Worker pools, warm/permanent workers and spot recovery remain later execution-topology work. The v0.1 runner contract should make those possible without introducing a second scheduler or control plane.
