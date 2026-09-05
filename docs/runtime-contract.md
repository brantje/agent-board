# Runtime contract

This document defines the boundary between Agent Board's Go orchestration layer and disposable execution environments.

Docker is implementation #1; the contract remains implementation-neutral.

## Invariants

1. Runtime is reusable configured execution environment/policy.
2. Runtime Instance is disposable compute materialized from Runtime.
3. Runtime Instance is not Agent identity.
4. Runtime Instance is not Run identity.
5. Runtime Instance, `agent-runner`, Execution Session and Run are separate identities.
6. A Run may use multiple Runtime Instances sequentially.
7. Workspace survives Runtime Instance destruction.
8. A Runtime Instance is bound to exactly one Workspace for its lifetime.
9. One Runtime Instance has one active `agent-runner`.
10. One runner may execute many Execution Sessions over time against its bound Workspace.
11. One Execution Session owns one process tree.
12. Configured Runtime identity is separate from implementation/external IDs.
13. Agent-executed code is untrusted.
14. Runtime creation comes from validated server-owned configuration.
15. Coding Engine processes execute inside the selected Runtime Instance through `agent-runner`.

## Resolution

```text
Executor Profile
 -> Runtime
 -> verify accessible/enabled/runnable
 -> validated Runtime Spec
 -> Runtime implementation
 -> Runtime Instance
 -> agent-runner
 -> Execution Session
 -> Engine process
```

Runtime owns the complete executable environment configuration and policy. Executor Profile references Runtime directly.

## Runtime configuration

Runtime contains the reusable configured values required to materialize execution:

- name and scope
- implementation kind
- image
- CPU/memory/PID/timeout limits
- Workspace policy
- network policy
- allowed secret references
- tooling/capabilities
- enabled state

## Runtime Spec

Conceptual Go shape:

```go
type RuntimeSpec struct {
    RuntimeInstanceID string
    ProjectID         string
    IssueID           string
    WorkspaceID       string
    RuntimeID         string

    Image             string
    WorkingDirectory  string
    Resources         ResourcePolicy
    Workspace         WorkspaceMount
    Network           NetworkPolicy
    AllowedSecretRefs []string
    Labels            map[string]string
}
```

A Runtime Instance may serve multiple Runs/Execution Sessions over time, so Run identity is not an immutable property of the Runtime Instance itself.

Runtime Spec is internal authorized state, not a public arbitrary-client request shape. Secret plaintext is not durable Runtime Spec state.

## Runtime implementation interface

Conceptual boundary:

```go
type RuntimeImplementation interface {
    Create(context.Context, RuntimeSpec) (Handle, error)
    Start(context.Context, Handle) error
    Inspect(context.Context, Handle) (Inspection, error)
    Stop(context.Context, Handle, StopReason) error
    Destroy(context.Context, Handle) error
}
```

Process/session execution is provided through the runner connection for the already materialized Runtime Instance rather than exposing raw Docker/runtime execution directly to Engine adapters.

Exact internal names may differ. Engines receive a narrower execution capability, not raw Docker/runtime internals or WebSocket framing.

## Durable handle metadata

Persist only enough safe external identity to inspect and clean up Runtime Instances after backend restart. Container IDs/external handles are infrastructure metadata, not product identity.

Runner connection state is not authoritative product state. Durable Runtime Instance metadata must be sufficient to reconcile an interrupted runner connection with the underlying compute after restart.

## Execution Sessions

An Execution Session is the runner-scoped process execution identity.

An authorized session request supports:

- server-issued Execution Session ID
- command/argv
- Workspace-bounded cwd
- ephemeral environment/secrets
- timeout/cancellation
- stdin/stdout/stderr
- exit/result reporting

There is no client-facing arbitrary shell endpoint.

v0.1 permits one active Execution Session per runner. The versioned runner protocol must remain capable of representing multiple sessions over the lifetime of one runner so future capacity can raise that limit without changing the identity model.

## Lifecycle

Runtime Instance lifecycle:

```text
PROVISIONING
 -> STARTING
 -> RUNNING
 -> STOPPING / FAILED
 -> STOPPED
 -> DESTROYED
```

Runner/session availability is separate from Runtime Instance lifecycle. A RUNNING Runtime Instance may have a runner that is connecting, ready, busy, draining or unavailable.

Transitions are durable/observable where they affect authoritative execution state and cleanup is idempotent.

## Workspace

The authoritative Issue Workspace is mounted at `/workspace`.

A Runtime Instance is created with one immutable Workspace binding and is never reused for another Workspace in v0.1. The same Runtime Instance may execute many sessions against that Workspace.

Runtime Instance teardown never deletes the Workspace. Workspace cleanup is a separate retention operation. A replacement Runtime Instance may later mount the same Workspace.

## Agent runner

Official v0.1 Runtime images include `agent-runner`.

The server and runner communicate over a versioned WebSocket protocol. The exact connection-initiation/registration mechanism is implementation-defined, but authentication must be narrowly scoped and must not grant broad control-plane privileges.

See `agent-runner.md`.

## Secrets

Runtime owns allowed-secret policy.

Immediately before an authorized Execution Session starts, trusted Go code resolves permitted secrets and passes only execution-scoped values to the runner/session.

Requirements:

- enforce `allowedSecretRefs` before resolution
- never persist secret plaintext into Runtime Instance state, runner state, Events, raw logs, Artifacts or provenance
- runner protocol responses never echo secret values
- redact before all durable output sinks
- inspection APIs never expose resolved values

## Network policy

Initial modes:

- none
- restricted
- outbound

A configured mode is either faithfully enforced by the selected Runtime implementation or rejected as unsupported.

Runner connectivity required by Agent Board is part of Runtime infrastructure and must not silently widen the Engine/repository network policy beyond what is documented/enforced.

## Resource policy

Runtime owns effective CPU, memory, PID, timeout and Workspace/disk limits where supported.

Unsupported executable policy fields fail validation or are absent from public configuration until enforceable.

## Runtime health/capabilities

Health distinguishes:

- configuration validity
- implementation/backend reachability
- runner availability/protocol compatibility
- policy/capability support
- operational executability

Enabled configuration alone does not prove a Runtime is runnable.

Runner capability negotiation includes protocol version and supported concurrent Execution Sessions. v0.1 uses a maximum of one active session per runner.

## Docker implementation

Docker implementation:

1. resolves/pulls the selected official Agent Board Runtime image
2. creates compute with effective resource/network policy
3. mounts exactly one authorized Workspace plus required runtime data
4. starts `agent-runner`
5. establishes/reconciles the versioned WebSocket runner connection
6. executes Engine process trees through runner Execution Sessions
7. streams stdin/stdout/stderr without unbounded buffering
8. propagates exit status
9. terminates sessions predictably on cancellation
10. supports restart-safe cleanup using durable external identity
11. may keep a healthy Runtime Instance/runner available for later sessions against the same Workspace
12. never exposes `/var/run/docker.sock` or equivalent daemon credentials to Agent Runtime Instances

The trusted backend may access Docker directly for v0.1 behind the Runtime implementation boundary.
