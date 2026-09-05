# Runtime Engine execution

The Runtime Instance is the actual execution environment for Engine processes. Coding-agent processes never execute directly in the trusted Go backend process.

## Execution boundary

```text
Go Run worker
  -> execution service
      -> resolved Runtime
          -> Runtime implementation
              -> Runtime Instance
                  -> agent-runner
                      -> Execution Session
                          -> Engine process tree
                              -> stdin
                              -> stdout
                              -> stderr
                              -> wait/exit
                              -> terminate/kill
```

Engine adapters remain in the trusted server. They receive a provider-neutral execution capability for the Runtime Instance already selected for the Run. They do not receive Docker clients, Docker sockets, provider-specific runtime handles or raw WebSocket framing.

## Workspace

The durable Issue Workspace is mounted at `/workspace`. Engine execution uses `/workspace` as its working directory unless an explicitly safe subdirectory is selected.

A Runtime Instance is bound to exactly one Workspace for its lifetime. A healthy instance/runner may execute many sequential Execution Sessions against that same Workspace.

Destroying Runtime compute never deletes or resets the Workspace. A replacement Runtime Instance may later mount the same Workspace.

## Runner transport

The server and `agent-runner` communicate over WebSocket.

The protocol is explicitly versioned and every execution message is scoped to a server-issued Execution Session ID. The exact connection-initiation/registration mechanism and final wire schema are implementation details, but transport semantics must support:

- protocol handshake/version negotiation
- runner capability advertisement
- session start
- stdin/stdout/stderr streaming
- exit/result reporting
- graceful termination and forced kill
- session/runner errors
- liveness and reconnect/reconciliation handling

A disconnected WebSocket is not by itself proof that the Engine process or Run failed. The server reconciles durable Run/Runtime state with external execution state before deciding whether to retry, terminate or replace compute.

See `agent-runner.md`.

## Execution Session contract

Conceptual Go boundary used by Engine adapters:

```go
type ExecRequest struct {
    SessionID string
    Command   []string
    Dir       string
    Env       map[string]string
    Secrets   map[string]SecretHandle
}

type ProcessSession interface {
    Stdout() io.Reader
    Stderr() io.Reader
    Stdin() io.WriteCloser
    Wait(context.Context) (ExitResult, error)
    Terminate(context.Context) error
    Kill(context.Context) error
}
```

Exact package/type names may differ; semantics are authoritative. The server-side execution client maps this interface onto the versioned runner WebSocket protocol.

Requirements:

- explicit Execution Session identity
- command + argv
- Workspace-bounded working directory
- ephemeral environment/secrets
- bounded/streamed stdout and stderr with channel identity
- exit status/result
- context cancellation
- graceful termination then forced containment
- one process tree per Execution Session
- recoverable durable Runtime Instance external identity for cleanup/reconciliation after backend restart

v0.1 permits one active Execution Session per runner. A runner may execute many sessions sequentially over its lifetime. The protocol must not encode the v0.1 concurrency limit as a permanent architectural restriction.

## Raw output

Runner output feeds the durable output sink described in `execution-evidence.md`.

Large stdout/stderr/protocol streams are not accumulated unboundedly in backend memory and are not duplicated into giant Event payloads.

Redaction occurs before persistence. Secret values must not be reflected by runner protocol responses.

## Cancellation and containment

Session cancellation:

1. cancel Run/session execution context
2. request graceful process-tree termination through the runner
3. wait bounded grace period
4. force kill/contain the session if needed
5. decide whether the same Runtime Instance remains healthy/reusable for the same Workspace
6. stop/destroy Runtime Instance when policy or health requires it
7. preserve durable Workspace

Cleanup is idempotent and restart-safe.

## Restart recovery

Durable Runtime Instance metadata includes enough external identity for a restarted Go backend to inspect/terminate/cleanup compute created by an earlier process.

Runner connection state itself is not authoritative durable state. After restart or WebSocket loss, the server reconciles the Runtime Instance and any potentially active execution before starting replacement work.

Lease expiry alone must not cause blind duplicate Engine execution when external execution may still be alive.

## Docker implementation

Docker is implementation #1.

Official Agent Board Runtime images include `agent-runner`. The trusted backend creates the container with one Workspace bind mount and controlled Docker access, starts/reconciles the runner connection, and executes Engine work through runner Execution Sessions.

Agent Runtime Instances never receive Docker daemon credentials/socket.

A real-Docker integration test should prove create/start/runner-connect/session-start/stdout/stderr/non-zero exit/Workspace write/cancel/reuse-same-Workspace/destroy and Workspace survival.

## Engine responsibilities

Engine adapter owns Engine-specific behavior:

- command/invocation
- protocol parsing
- model/provider configuration materialization
- mapping visible messages/commands/files/tests/questions/completion into canonical Agent Board evidence

`agent-runner` stays Engine-neutral and owns process-tree/session supervision only.

Infrastructure owns scheduler admission, Runtime containment, Workspace durability, secret resolution/injection, raw-output persistence, cancellation, reconciliation and cleanup.
