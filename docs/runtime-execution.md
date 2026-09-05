# Runtime Engine execution

The Runtime Instance is the actual execution environment for Engine adapters. Coding-agent processes never execute directly in the trusted Go backend process.

## Execution boundary

```text
Go Run worker
  -> execution service
      -> resolved Runtime
          -> Runtime implementation
              -> Runtime Instance
                  -> process/session
                      -> stdin
                      -> stdout
                      -> stderr
                      -> wait/exit
                      -> terminate/kill
```

Engine adapters receive only a provider-neutral Runtime execution capability for the Runtime Instance already selected for the Run. They do not receive Docker clients, the Docker socket or provider-specific handles.

## Workspace

The durable Issue Workspace is mounted at `/workspace`. Engine execution uses `/workspace` as its working directory unless an explicitly safe subdirectory is selected.

Destroying Runtime compute never deletes or resets the Workspace.

## Process/session contract

Conceptual Go boundary:

```go
type ExecRequest struct {
    Command []string
    Dir     string
    Env     map[string]string
    Secrets map[string]SecretHandle
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

Exact package/type names may differ; semantics are authoritative.

Requirements:

- command + argv
- Workspace-bounded working directory
- ephemeral environment/secrets
- bounded/streamed stdout and stderr with channel identity
- exit status/result
- context cancellation
- graceful termination then forced containment
- recoverable durable Runtime Instance external identity for cleanup after backend restart

## Raw output

Runtime output feeds the durable output sink described in `execution-evidence.md`.

Large stdout/stderr/protocol streams are not accumulated unboundedly in backend memory and are not duplicated into giant Event payloads.

Redaction occurs before persistence.

## Cancellation and containment

Cancellation:

1. cancel Run execution context
2. request graceful process termination
3. wait bounded grace period
4. force terminate/contain if needed
5. stop/destroy disposable Runtime Instance as policy requires
6. preserve durable Workspace

Cleanup is idempotent and restart-safe.

## Restart recovery

Durable Runtime Instance metadata includes enough external identity for a restarted Go backend to inspect/terminate/cleanup compute created by an earlier process.

Lease expiry alone must not cause blind duplicate Engine execution when external execution may still be alive.

## Docker implementation

Docker is implementation #1.

The trusted backend has controlled Docker access. Agent Runtime Instances never receive Docker daemon credentials/socket.

A real-Docker integration test should prove create/start/exec/stdout/stderr/non-zero exit/Workspace write/cancel/destroy and Workspace survival.

## Engine responsibilities

Engine adapter owns Engine-specific behavior:

- command/invocation
- protocol parsing
- model/provider configuration materialization
- mapping visible messages/commands/files/tests/questions/completion into canonical Agent Board evidence

Infrastructure owns scheduler admission, Runtime containment, Workspace durability, secret injection, raw-output persistence, cancellation and cleanup.
