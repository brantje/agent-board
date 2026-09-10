# Runtime and Runner Engine execution

Preferred production execution uses an external persistent `agent-runner` host selected by the existing scheduler. The server-managed internal Runner uses the same Runner/Execution Session/protocol-v2 path. Runtime/Runtime Instance remain only for **legacy internal managed compute** where existing code still uses them. Coding-agent processes never execute directly in the trusted Go backend process.

## Execution boundary

Canonical v0.1 path:

```text
Run
  -> existing scheduler
      -> selected connected Runner
          -> authenticated outbound protocol-v2 WebSocket
              -> agent-runner
                  -> Execution Session
                      -> Engine process tree
```

Agents configure Engine + Model Profile, not Runtime, Runtime Instance, Runner, Executor Profile or Runner Profile. Legacy managed compute may still provision a Runtime Instance before reaching the same `agent-runner` / Execution Session boundary. Do not create a placeholder Runtime Instance merely to run an external Runner.

Engine adapters remain in the trusted server. They receive a provider-neutral execution capability for the Runner already selected for the Run. They do not receive Docker clients, Docker sockets, provider-specific Runtime handles or raw WebSocket framing.

## Workspace

The backend-owned durable Issue Workspace is authoritative. Before Runner execution, its exact required non-ignored Git state is transferred into a fresh Runner-local session Workspace and exposed to the Engine as `/workspace`.

Each external/internal Runner Execution Session gets its own temporary Runner Workspace materialization. The Runner itself is not bound to one Issue Workspace and may execute sequential sessions for different Runs/Workspaces over time. Sync-back returns resulting filesystem state to the authoritative Issue Workspace while preserving authoritative staging and avoiding visible transport commits/refs.

Where legacy internal managed compute uses Runtime Instances, a Runtime Instance remains bound to exactly one Workspace for its lifetime. Runtime teardown never deletes the durable Workspace.

## Runner transport

The Runner initiates one authenticated outbound WebSocket to the Agent Board server. Protocol v2 is authoritative for both external and server-managed internal Runners.

Every execution message is scoped to a server-issued Execution Session ID. Transport semantics support:

- protocol-v2 handshake/version negotiation
- Runner version/capability advertisement
- session start
- Git-native Workspace transfer in both directions
- stdin/stdout/stderr streaming
- exit/result reporting
- graceful termination and forced kill
- session/Runner errors
- health with active session IDs
- liveness and reconnect/reconciliation handling

A disconnected WebSocket is not by itself proof that the Engine process or Run failed. Disconnect time starts the configured reconnect grace. The durable Execution Session remains uncertain/reconciling and cannot be duplicated while ownership is unresolved. If the same Runner reconnects and reports the same active session, the server reattaches. Only after the grace expires without reconciliation may the session become infrastructure-failed and existing Run retry behavior decide what happens next.

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

Exact package/type names may differ; semantics are authoritative. The server-side execution client maps this interface onto protocol v2.

Callers that intentionally stop consuming stdout or stderr must explicitly abandon that stream through the provider-neutral execution API (`AbandonStdout` / `AbandonStderr` in the server implementation). Abandoning output releases only the local consumer; it does not terminate the Execution Session. The transport continues draining already-received output so the Runner connection cannot deadlock on backpressure.

Requirements:

- explicit Execution Session identity
- command + argv
- Workspace-bounded working directory
- ephemeral environment/secrets
- bounded/streamed stdout and stderr with channel identity
- explicit output-stream abandonment without cancelling the process
- exit status/result
- context cancellation
- graceful termination then forced containment
- one process tree per Execution Session
- restart/reconnect reconciliation that does not require process-local control-plane state

v0.1 permits one active Execution Session per Runner. A Runner may execute many sessions sequentially over its lifetime. The protocol retains `max_active_sessions` so this v0.1 limit is not a permanent architectural restriction.

For the legacy Runtime path only, durable Runtime Instance external identity remains sufficient to inspect/clean up managed compute after backend restart.

## Raw output

Runner output feeds the durable output sink described in `execution-evidence.md`.

Large stdout/stderr/protocol streams are not accumulated unboundedly in backend memory and are not duplicated into giant Event payloads.

Redaction occurs before persistence. Secret values must not be reflected by Runner protocol responses.

## Cancellation and containment

Runner session cancellation:

1. cancel Run/session execution context
2. request graceful process-tree termination through the Runner
3. wait a bounded grace period
4. force-kill the process tree if needed
5. sync Runner Workspace state back whenever technically possible, including cancellation/failure
6. retain Runner Workspace until successful server apply acknowledgement
7. finalize durable execution outcome and release Workspace ownership only after recovery/apply handling completes

For legacy internal managed compute, Runtime-specific cleanup may additionally stop/destroy unhealthy compute after the same Execution Session recovery rules. That is not part of normal external-Runner cancellation.

Cleanup is idempotent and restart-safe.

## Questions and waiting for input

A native blocking Question does not end the Runner Execution Session. The same Runner, native Engine session and Runner Workspace remain attached through `WAITING_FOR_INPUT`. Answering routes back into that same live session. Agent/Model scheduler capacity policy is separate from this Execution Session ownership; the Runner must not become eligible for duplicate work while its active session is waiting for input.

## Restart and reconnect recovery

Runner connection state is ephemeral, but Run/Execution Session ownership is durable. On backend restart or WebSocket loss, the server reconciles the selected Runner and potentially active Execution Session before starting replacement work.

A newly authenticated connection for the same immutable `runner_id` may replace a stale transport only after active-session reconciliation. Matching claims for the durable active session permit reattachment. Missing/mismatched/unexpected active-session claims remain fail-closed rather than discarding ownership or starting duplicate Engine work.

Lease expiry alone must not cause blind duplicate Engine execution when external execution may still be alive.

Legacy Runtime Instance metadata is reconciled separately only where internal managed compute was actually used.

## Legacy Docker implementation

Docker remains implementation #1 for legacy/internal managed compute; it is not the preferred production placement architecture.

Official Agent Board managed Runtime images may include `agent-runner`. The trusted backend creates the container with its authorized Workspace binding and controlled Docker access, starts/reconciles the Runner connection, and executes Engine work through the same protocol-v2 Execution Session boundary.

Agent Runtime Instances never receive Docker daemon credentials/socket. External Runner hosts are user-trusted machines and may independently have Docker/Podman installed as host tooling; Agent Board does not provide those daemon credentials through the Runner protocol.

Real-Docker integration tests continue to cover the supported legacy managed-compute path without making Docker Runtime Instances a release prerequisite for normal external Runner execution.

## Engine responsibilities

Engine adapter owns Engine-specific behavior:

- command/invocation
- protocol parsing
- Model Profile/Provider configuration materialization
- mapping visible messages/commands/files/tests/questions/completion into canonical Agent Board evidence

`agent-runner` stays Engine-neutral and owns process-tree/session supervision only.

Infrastructure owns scheduler admission/Runner placement, Workspace durability/transfer, secret resolution/injection, raw-output persistence, cancellation, reconciliation and cleanup. Legacy Runtime containment remains a conditional internal-managed-compute responsibility, not a second execution lifecycle.
