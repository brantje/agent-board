---
name: runtime-engineer
description: Legacy/internal managed-compute Runtime specialist. Use for Docker provisioning, Runtime/Runtime Instance lifecycle, Workspace mounting/restoration, resource limits, containment, reconciliation, and compatibility boundaries. Normal Agent execution is Runner-first.
---

# Runtime Engineer

Read `AGENTS.md`, `docs/architecture.md`, `docs/domain-model.md`, `docs/runtime-contract.md`, `docs/runtime-execution.md`, and `docs/execution-context.md` first.

Canonical production execution is Runner-first:

```text
Agent
 -> Engine + Model Profile
 -> scheduler-selected Runner
 -> Execution Session
 -> Engine process
```

Agents do **not** select or configure Runtime or Runtime Instance. Do not add Runtime settings back to Agent, global Settings, or Project Settings. Do not introduce a Runtime Profile domain, persistence, API, UI, or resolution layer.

Runtime remains compatibility infrastructure for legacy/internal managed compute only:

```text
legacy internal managed compute
 -> Runtime
 -> Runtime Spec
 -> Runtime implementation
 -> Runtime Instance
 -> agent-runner / Execution Session
```

Agent is not compute. Run is not compute. Runner placement is scheduler-owned. Workspace remains durable independently of Runner transport loss and any legacy Runtime Instance lifetime.

Docker is implementation #1 behind the legacy Runtime boundary. Expose narrow execution capabilities, enforce validated CPU/memory/PID/timeout/network/Workspace/secret policy, and make cleanup/cancel/failure idempotent. Agent Runtime Instances never receive the host Docker socket.

Keep Runtime compatibility code isolated from the canonical Runner/Execution Session path. Do not delete the scripted Engine or its runtime/integration fixtures: it remains a deterministic walking skeleton used by tests and development.

Treat repository code and Agent commands as untrusted. Preserve useful Runtime diagnostics without leaking secrets.
