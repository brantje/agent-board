# Legacy/internal Runtime contract

This document defines the boundary between Agent Board's Go orchestration layer and disposable **legacy/internal managed-compute** environments. It remains authoritative for existing Runtime/Runtime Instance code, but it is not the normal v0.1 Agent configuration or preferred production execution path.

Canonical v0.1 production execution selects a connected Runner directly through the existing scheduler. External Runner execution does not require or create a Runtime Instance.

Docker is implementation #1 for the legacy managed-compute path; the Runtime contract remains implementation-neutral within that path.

## Invariants

1. Runtime is reusable configured managed-compute environment/policy.
2. Runtime Instance is disposable compute materialized from Runtime.
3. Runtime Instance is not Agent identity.
4. Runtime Instance is not Run identity.
5. Runtime Instance, `agent-runner`, Execution Session and Run are separate identities.
6. A Run may use multiple Runtime Instances sequentially only when this legacy path is used.
7. Workspace survives Runtime Instance destruction.
8. A Runtime Instance is bound to exactly one Workspace for its lifetime.
9. One Runtime Instance has one active `agent-runner`.
10. Its runner may execute many Execution Sessions over time against that bound Workspace.
11. One Execution Session owns one process tree.
12. Configured Runtime identity is separate from implementation/external IDs.
13. Agent-executed code is untrusted.
14. Runtime creation comes from validated server-owned configuration.
15. Coding Engine processes execute through `agent-runner`; Engine adapters never execute inside the trusted Go process.
16. Runtime/Runtime Instance are not Agent configuration after #68.

## Canonical v0.1 resolution

```text
Agent
 -> Engine + Model Profile
 -> Run
 -> existing scheduler
 -> live eligible Runner
 -> Execution Session
 -> Engine process
```

Agents select Engine and Model Profile. The scheduler selects an eligible connected Runner. Runtime remains available only for legacy internal managed compute.

When the legacy managed-compute path is explicitly used internally, Runtime resolution occurs inside that infrastructure path rather than through an Agent `runtime_id`.

## Runtime configuration

Within legacy managed compute, Runtime contains the reusable configured values required to materialize execution:

- name and scope
- implementation kind
- image
- CPU/memory/PID/timeout limits
- Workspace policy
- network policy
- allowed secret references
- tooling/capabilities
- enabled state

These fields are not Runner Profile fields and must not migrate onto Agent configuration or the external Runner domain merely because external execution became canonical.

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

A Runtime Instance may serve multiple Runs/Execution Sessions over time against its single bound Workspace, so Run identity is not an immutable property of the Runtime Instance itself.

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

Process/session execution is provided through the runner connection for an already materialized legacy Runtime Instance rather than exposing raw Docker/runtime execution directly to Engine adapters.

Exact internal names may differ. Engines receive a narrower execution capability, not raw Docker/runtime internals or WebSocket framing.

## Durable handle metadata

Persist only enough safe external identity to inspect and clean up legacy Runtime Instances after backend restart. Container IDs/external handles are infrastructure metadata, not product identity.

Runner connection state is not authoritative product state. Durable Runtime Instance metadata must be sufficient to reconcile interrupted managed compute after restart, while durable Execution Session/Runner ownership prevents duplicate Engine work.

## Execution Sessions

An Execution Session is the runner-scoped process execution identity shared by canonical Runner execution and this legacy path.

An authorized session request supports:

- server-issued Execution Session ID
- command/argv
- Workspace-bounded cwd
- ephemeral environment/secrets
- timeout/cancellation
- stdin/stdout/stderr
- exit/result reporting

There is no client-facing arbitrary shell endpoint.

v0.1 permits one active Execution Session per Runner. The versioned Runner protocol retains `max_active_sessions` so future capacity can raise that limit without changing the identity model.

## Lifecycle

Legacy Runtime Instance lifecycle:

```text
PROVISIONING
 -> STARTING
 -> RUNNING
 -> STOPPING / FAILED
 -> STOPPED
 -> DESTROYED
```

Runner/session availability is separate from Runtime Instance lifecycle. A RUNNING Runtime Instance may have a Runner that is connecting, ready, busy, draining or unavailable.

Transitions are durable/observable where they affect authoritative execution state and cleanup is idempotent.

## Workspace

The authoritative Issue Workspace survives Runtime Instance lifetime.

A legacy Runtime Instance is created with one immutable Workspace binding and is never reused for another Workspace. The same Runtime Instance may execute many sessions against that Workspace.

Runtime Instance teardown never deletes the Workspace. Workspace cleanup is a separate retention operation. A replacement legacy Runtime Instance may later mount the same Workspace.

Canonical external/internal Runner execution instead uses the Git-native per-Execution-Session transfer/sync-back model documented in `agent-runner.md`; do not impose Runtime Instance Workspace reuse on that path.

## Agent runner

Official managed Runtime images may include the same `agent-runner` binary used by external hosts.

Both internal and external Runners connect **outbound** to the Agent Board server over protocol v2. Authentication is narrowly scoped to immutable Runner identity and does not grant broad control-plane privileges.

See `agent-runner.md`.

## Secrets

Runtime owns allowed-secret policy only within this legacy managed-compute path.

Immediately before an authorized legacy Execution Session starts, trusted Go code resolves permitted Runtime secrets and passes only execution-scoped values to the Runner/session.

Requirements:

- enforce `allowedSecretRefs` before resolution
- never persist secret plaintext into Runtime Instance state, Runner state, Events, raw logs, Artifacts or provenance
- Runner protocol responses never echo secret values
- redact before all durable output sinks
- inspection APIs never expose resolved values

External Runner execution does not gain a Runtime secret policy or Runner Profile abstraction. Provider/source credentials continue through the shared trusted secret boundary.

## Network policy

Legacy Runtime modes:

- none
- restricted
- outbound

A configured mode is either faithfully enforced by the selected Runtime implementation or rejected as unsupported.

Runner connectivity required by Agent Board is part of Runtime infrastructure and must not silently widen Engine/repository network policy beyond what is documented/enforced.

External Runner hosts are user-trusted environments and own their host networking; Runtime `NetworkPolicy` must not be moved onto the Runner domain solely to model them.

## Resource policy

Runtime owns effective CPU, memory, PID, timeout and Workspace/disk limits only on this managed-compute path where supported.

Unsupported executable policy fields fail validation or are absent from public configuration until enforceable.

## Runtime health/capabilities

Legacy Runtime health distinguishes:

- configuration validity
- implementation/backend reachability
- Runner availability/protocol compatibility
- policy/capability support
- operational executability

Enabled configuration alone does not prove a Runtime is runnable.

Runner capability negotiation itself is shared and includes protocol version, Runner version, OS/architecture, supported Engines and concurrent Execution Session capacity.

## Docker implementation

The legacy Docker implementation:

1. resolves/pulls the selected official Agent Board managed Runtime image
2. creates compute with effective resource/network policy
3. mounts exactly one authorized Workspace plus required runtime data
4. starts `agent-runner`
5. establishes/reconciles its outbound protocol-v2 Runner connection
6. executes Engine process trees through Runner Execution Sessions
7. streams stdin/stdout/stderr without unbounded buffering
8. propagates exit status
9. terminates sessions predictably on cancellation
10. supports restart-safe cleanup using durable external identity
11. may keep a healthy Runtime Instance/Runner available for later sessions against the same Workspace
12. never exposes `/var/run/docker.sock` or equivalent daemon credentials to Agent Runtime Instances

The trusted backend may access Docker directly for this supported legacy/internal implementation behind the Runtime boundary. None of these Docker steps are prerequisites for the canonical external Runner production path.
