# Runtime contract

This document defines the boundary between Agent Board's Go orchestration layer and disposable execution environments.

Docker is implementation #1; the contract remains implementation-neutral.

## Invariants

1. Runtime is reusable configured execution environment/policy.
2. Runtime Instance is disposable compute materialized from Runtime.
3. Runtime Instance is not Agent identity.
4. Runtime Instance is not Run identity.
5. A Run may use multiple Runtime Instances sequentially.
6. Workspace survives Runtime Instance destruction.
7. Configured Runtime identity is separate from implementation/external IDs.
8. Agent-executed code is untrusted.
9. Runtime creation comes from validated server-owned configuration.
10. Coding Engine processes execute inside the selected Runtime Instance.

## Resolution

```text
Executor Profile
 -> Runtime
 -> verify accessible/enabled/runnable
 -> validated Runtime Spec
 -> Runtime implementation
 -> Runtime Instance
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
    RunID             string
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

Runtime Spec is internal authorized state, not a public arbitrary-client request shape. Secret plaintext is not durable Runtime Spec state.

## Runtime implementation interface

Conceptual boundary:

```go
type RuntimeImplementation interface {
    Create(context.Context, RuntimeSpec) (Handle, error)
    Start(context.Context, Handle) error
    Exec(context.Context, Handle, ExecRequest) (ProcessSession, error)
    Inspect(context.Context, Handle) (Inspection, error)
    Stop(context.Context, Handle, StopReason) error
    Destroy(context.Context, Handle) error
}
```

Exact internal names may differ. Engines receive a narrower execution capability, not raw Docker/runtime internals.

## Durable handle metadata

Persist only enough safe external identity to inspect and clean up Runtime Instances after backend restart. Container IDs/external handles are infrastructure metadata, not product identity.

## Exec request

Internal authorized exec supports command/argv, Workspace-bounded cwd, ephemeral environment/secrets, timeout and cancellation.

There is no client-facing arbitrary shell endpoint.

## Lifecycle

```text
PROVISIONING
 -> STARTING
 -> RUNNING
 -> STOPPING / FAILED
 -> STOPPED
 -> DESTROYED
```

Transitions are durable/observable and cleanup is idempotent.

## Workspace

The authoritative Issue Workspace is mounted at `/workspace`. Runtime Instance teardown never deletes it. Workspace cleanup is a separate retention operation.

## Secrets

Runtime owns allowed-secret policy.

Immediately before authorized process launch, trusted Go code resolves permitted secrets and injects them ephemerally.

Requirements:

- enforce `allowedSecretRefs` before resolution
- never persist secret plaintext into Runtime Instance state, Events, raw logs, Artifacts or provenance
- redact before all durable output sinks
- inspection APIs never expose resolved values

## Network policy

Initial modes:

- none
- restricted
- outbound

A configured mode is either faithfully enforced by the selected Runtime implementation or rejected as unsupported.

## Resource policy

Runtime owns effective CPU, memory, PID, timeout and Workspace/disk limits where supported.

Unsupported executable policy fields fail validation or are absent from public configuration until enforceable.

## Runtime health/capabilities

Health distinguishes:

- configuration validity
- implementation/backend reachability
- policy/capability support
- operational executability

Enabled configuration alone does not prove a Runtime is runnable.

## Docker implementation

Docker implementation:

1. resolves/pulls the selected image
2. creates compute with effective resource/network policy
3. mounts only authorized Workspace/runtime data
4. starts a neutral container/session host where needed
5. executes Engine processes through Docker exec/session semantics
6. streams stdout/stderr without unbounded buffering
7. propagates exit status
8. terminates predictably on cancellation
9. supports restart-safe cleanup using durable external identity
10. never exposes `/var/run/docker.sock` or equivalent daemon credentials to Agent Runtime Instances

The trusted backend may access Docker directly for v0.1 behind the Runtime implementation boundary.
