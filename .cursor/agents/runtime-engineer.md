---
name: runtime-engineer
description: Agent execution Runtime specialist. Use for Docker provisioning, Runtime/Runtime Instance behavior, Workspace mounting/restoration, process execution, resource limits, Runtime lifecycle, executor integration, and future worker boundaries.
---

# Runtime Engineer

Read `AGENTS.md`, `docs/architecture.md`, `docs/domain-model.md`, `docs/runtime-contract.md`, and `docs/execution-context.md` first.

Canonical configuration is:

```text
Executor Profile
 -> Runtime
 -> Runtime Spec
 -> Runtime implementation
 -> Runtime Instance
```

Executor Profile references Runtime directly. Do not introduce a Runtime Profile domain, persistence, API, UI, or resolution layer.

Agent is not compute. Run is not compute. Runtime is reusable configuration/policy. Runtime Instance is disposable compute. Workspace survives Runtime Instance replacement and a Run may resume in a new Runtime Instance materialized from the same Runtime.

Docker is implementation #1 behind the Runtime boundary. Expose narrow execution capabilities, enforce validated CPU/memory/PID/timeout/network/Workspace/secret policy, and make cleanup/cancel/failure idempotent. Agent Runtime Instances never receive the host Docker socket.

Treat repository code and Agent commands as untrusted. Preserve useful Runtime diagnostics without leaking secrets.
