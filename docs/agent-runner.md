# Agent Runner

`agent-runner` is the Engine-neutral execution-plane binary between the trusted Go control plane and coding Engine processes such as OpenCode.

Production v0.1 prefers **external persistent Runner hosts**: a user-managed Linux machine runs a versioned standalone `agent-runner` binary as a systemd service and connects outbound to Agent Board over protocol v2. The server-managed internal Runner uses the same protocol and Workspace-transfer path; it is not a second execution topology.

The Runner is part of the v0.1 execution architecture. It is not an Agent, Run, Runtime, Runtime Instance or future Worker.

Install and operate external hosts with `docs/external-runner-setup.md` and `apps/agent-runner/deploy/agent-runner.service`.

## Execution model

Preferred path:

```text
Issue
 -> Run
 -> existing scheduler
 -> selected connected Runner
 -> transferred Issue Workspace
 -> server-side Engine adapter
 -> Execution Session
 -> coding CLI on Runner host
 -> Workspace sync-back
 -> candidate / Review
```

External Runner execution does not create a placeholder Runtime Instance. Legacy managed Runtime support remains separate compatibility code; Agents do not select Runtimes directly.

These identities remain separate:

```text
Runner != agent-runner process != Execution Session != Run != Workspace
```

## Enrollment and credentials

External Runner enrollment has exactly one durable Runner identity from creation onward. When an administrator chooses **Create runner**, Agent Board immediately creates the pending external Runner with its immutable `runner_id`. A pending Runner has no display name and no permanent Runner credential. Its one-time registration-token hash is stored on that Runner row and the plaintext `registrationToken` is returned once.

`agent-runner register` submits that registration token together with `os.Hostname()`. In one PostgreSQL transaction the server verifies that the same Runner is still pending/external/non-revoked/non-deleted, consumes the registration-token hash, sets the hostname as the initial display name, creates the normal permanent Runner credential, and marks the Runner registered. The returned `runnerId` is the same immutable ID created by the administrator; `runnerToken` is the new permanent credential.

The one-time registration token is never a Runner credential and cannot authenticate `/api/runner/ws`. It is not persisted by `agent-runner` and cannot be reused after successful enrollment. The permanent Runner token is stored only as a hash server-side and is returned in plaintext only when first issued or rotated.

External enrollment persists the normal runtime configuration in `/etc/agent-board/agent-runner.env` using `AGENT_BOARD_URL`, `AGENT_RUNNER_ID`, `AGENT_RUNNER_TOKEN` and `AGENT_RUNNER_WORKSPACE_ROOT`. `AGENT_RUNNER_TOKEN` always means the permanent post-registration Runner credential. Normal startup remains the existing `configFromEnv()` -> `Server.Connect()` path; there is no additional Runner state/configuration file.

Hostname is only the initial display-name source. An administrator may rename a registered external Runner later. Reconnects and capability/health updates do not resend or overwrite that name, and the Runner process cannot rename itself.

The server-managed internal Runner continues to use the same ordinary permanent Runner credential generation/hash/authentication path without going through external enrollment. It is immediately registered and server-managed.

Required relationships:

- one Runner may execute many Execution Sessions over time
- one active Runner Execution Session owns one process tree and one transferred Workspace copy
- one Run may use Execution Sessions over its lifetime, but a blocking native Question continues the same live Runner Execution Session
- the durable Issue Workspace remains server-authoritative
- one logical writer owns an Issue Workspace during a Runner execution lifecycle
- a Runtime Instance, where legacy managed compute is used, remains bound to exactly one Workspace for its lifetime

## Workspace ownership and transfer

The durable Issue Workspace on the server is authoritative and outlives Runner processes and connections.

Before Runner execution, Agent Board snapshots the complete non-ignored Git state and transfers it to the selected Runner. Snapshotting must not mutate the authoritative `HEAD`, Git index, staging state or visible refs/history. The transfer preserves staged, unstaged and untracked non-ignored files, deletions, executable bits and symlinks so the Runner starts from the same filesystem state.

From transfer through execution, blocking Questions, recovery sync-back, candidate capture and final transition, the Run's Runner Execution Session is the logical Workspace writer. Generic server-side Workspace mutation is fenced during that lifecycle. The durable ownership record does not require a PostgreSQL connection to remain checked out while an Engine runs or waits for human input; database/advisory locks are used only for bounded filesystem critical sections.

On completion, failure or cancellation, the Runner returns a Workspace bundle. The server verifies it and applies the Runner's filesystem delta to the authoritative working tree **without replacing the authoritative Git index and without manufacturing a commit**. Runner staging is transport input, not permission to alter server staging. Existing authoritative staging therefore remains intact while Runner-created file contents/deletions/untracked files become visible to candidate collection.

Ignored files are not transferred. Transport-only commits and refs used to encode Git state are implementation details and must never become user-visible history.

The Runner keeps its session Workspace until the server has successfully verified and applied the returned bundle and explicitly acknowledges that apply. Apply failure, missing acknowledgement or transport loss preserves the Runner Workspace for recovery.

## Transport

The server and Runner communicate over one outbound WebSocket initiated by the Runner. Protocol v2 is the only supported Runner protocol after the #68 migration.

Every execution message is scoped to a server-issued Execution Session identity. The wire protocol supports multiple sessions over the lifetime of one Runner connection even when the configured concurrency limit is one.

The protocol supports at least:

- Runner handshake and protocol-version negotiation
- Runner capability advertisement
- session start
- Workspace transfer in both directions with bounded chunks/checksums
- explicit server acknowledgement after returned Workspace apply
- stdin streaming and stdin close
- stdout/stderr streaming with channel identity
- exit/result reporting
- graceful termination
- forced kill
- Runner/session errors
- liveness/connection reconciliation

A WebSocket disconnect is an infrastructure signal, not by itself durable proof that a Run or Engine process failed. The reconnect grace period starts when the live Runner connection is actually lost. Its server deployment setting is `AGENT_BOARD_RUNNER_RECONNECT_TIMEOUT`, which defaults to `5m` and accepts a positive Go duration. If the Runner reconnects within the grace period, Agent Board may reattach to the same active Execution Session; unresolved sessions fail only after that grace expires.

A newly authenticated connection claiming the same immutable `runner_id` does not blindly evict a live transport. The server compares the old/new Runner health claims with durable active Execution Session ownership. Matching claims for the same durable session permit safe replacement/reattachment. Missing, mismatched or unexpected active-session claims are rejected/fail-closed so a takeover cannot discard ownership or enable duplicate Engine execution.

## Concurrency and future fleets

Default advertised capacity is:

```text
max active Execution Sessions per Runner = 5
```

A Runner advertises `max_active_sessions` so capacity can change without changing the identity model or transport contract. Missing or zero advertised capacity is treated as 5.

Workspace write safety remains separate from Runner transport concurrency. Supporting several protocol sessions does not imply that several authoritative writers may mutate one Workspace concurrently.

## Engine ownership

Engine adapters stay in the trusted server.

Engine adapters own:

- Engine-specific command/invocation construction
- protocol parsing
- Model Profile/Provider configuration materialization
- mapping visible Engine activity into canonical Agent Board evidence

`agent-runner` stays Engine-neutral. It receives an authorized execution request and starts/supervises that process inside the Execution Session's transferred Workspace on the selected host.

## Runner responsibilities

The Runner owns only execution-plane behavior:

- authenticate and connect outbound to the control plane
- advertise versioned capabilities
- accept authorized session requests
- enforce the provided Workspace-bounded working directory
- start and supervise one process tree per Execution Session
- apply execution-scoped environment/secret values passed by the trusted server
- stream stdin/stdout/stderr
- report exit status/result
- propagate cancellation and graceful termination
- force-kill the process tree when required
- retain session Workspace state until successful apply acknowledgement
- isolate session state between executions

The Runner does not own:

- PostgreSQL or scheduler state
- Issue/Board workflow
- Agent concurrency or Model Profile capacity admission
- Project authorization
- Provider credential storage/decryption
- Review decisions
- durable Event/evidence persistence
- control-plane Git history

## Security boundary

External Runner hosts are user-managed, trusted execution environments. Agent Board does not claim Docker/container isolation for them. Operators decide what coding CLIs and host-level tooling are installed there.

The trusted server resolves and authorizes configuration and secrets before execution. The Runner receives only execution-scoped data needed by the session and a Workspace-bounded working directory. Internal managed Runner deployment may add container isolation, but that does not change the protocol or product model.

The Runner must never receive:

- PostgreSQL credentials
- backend encryption/signing keys
- Docker socket or daemon credentials from the control plane
- broad control-plane credentials
- arbitrary server filesystem access

Secret values remain ephemeral. They must not be echoed in Runner protocol responses and must be redacted before every durable server-side sink.

## Distribution and versioning

For v0.1, `agent-runner` is a standalone Linux binary suitable for installation as a persistent systemd service on external hosts. Release/production builds embed a concrete version into `internal/server.Version`; that same value is advertised as protocol capability `runner_version`. Local development may use the `dev` fallback.

The Docker build accepts `AGENT_RUNNER_VERSION`. Standalone release builds can inject the same value with Go `-ldflags -X`; the exact command and systemd installation are documented in `docs/external-runner-setup.md`.

This repository does not yet publish standalone binaries through a release workflow. Publishing/versioned artifact distribution belongs to that release pipeline; #68 does not add a package manager, installer repository or shell installer.

Conceptual repository layout:

```text
apps/
├── server/
├── agent-runner/
└── web/
```

## Questions and resume

Question/Decision/resume state remains durable on the server. The Runner has no durable product-state responsibility.

When OpenCode raises a blocking native Question, Agent Board persists that Question before the Run enters `WAITING_FOR_INPUT`. The same live Runner Execution Session and the same Runner Workspace remain attached while waiting; Agent Board does not sync the Workspace back, start a replacement Engine session or pay for a synthetic continuation prompt merely because human input is pending.

After the Decision is persisted, the Run resumes against that same native Engine session and Workspace. Cancellation while waiting terminates the process tree and performs the same bounded recovery sync-back used for other cancellation/failure paths.

## Future Worker pools

A future Worker/Pool is compute capacity and remains separate from Runner identity. Fleet placement can be added above the existing Runner protocol without introducing a second Run lifecycle or scheduler.

```text
Run
 -> existing scheduler
 -> Worker/Pool placement
 -> Runner
 -> Execution Session
 -> Engine
```

Worker pools, warm/permanent workers and spot recovery remain later execution-topology work.
