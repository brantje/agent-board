# Agent Runner

`agent-runner` is the Engine-neutral execution-plane binary between the trusted Go control plane and coding Engine processes such as OpenCode.

Production v0.1 prefers **external persistent Runner hosts**: a user-managed Linux machine runs a versioned standalone `agent-runner` binary as a systemd service and connects outbound to Agent Board over protocol v2. The server-managed internal Runner uses the same protocol; it is not a second execution topology.

The Runner is part of the v0.1 execution architecture. It is not an Agent, Run or future Worker.

Install and operate external hosts with `docs/external-runner-setup.md` and `apps/agent-runner/deploy/agent-runner.service`.

## Execution model

Preferred path:

```text
Issue
 -> Run
 -> existing scheduler
 -> selected connected Runner
 -> source-aware Issue checkout
 -> server-side Engine adapter
 -> Execution Session
 -> coding CLI on Runner host
 -> Git finalization/publication or branch return
 -> Review pinned to Git SHAs
```

The source synchronization edge depends on the Project source:

```text
local Project
  server-owned agent-board/<issue-key> branch
   -> #71 branch-only transfer
   -> Runner checkout
   -> branch-only return + server apply acknowledgement

remote Git Project
  Runner bare repository cache
   -> refs/remotes/origin/* source refs
   -> refs/heads/agent-board/* Issue branches
   -> per-Run worktree
   -> normal non-force Issue-branch push
```

Runner placement is scheduler-owned; Agents select Engine and Model Profile rather than execution hosts.

These identities remain separate:

```text
Runner != agent-runner process != Execution Session != Run != Workspace
```

## Enrollment and credentials

External Runner enrollment has exactly one durable Runner identity from creation onward. When an administrator chooses **Create runner**, Agent Board immediately creates the pending external Runner with its immutable `runner_id`. A pending Runner has no display name and no permanent Runner credential. Its one-time registration-token hash is stored on that Runner row and the plaintext `registrationToken` is returned once.

`agent-runner register` submits that registration token together with `os.Hostname()`. In one PostgreSQL transaction the server verifies that the same Runner is still pending/external/non-revoked/non-deleted, consumes the registration-token hash, sets the hostname as the initial display name, creates the normal permanent Runner credential, and marks the Runner registered. The returned `runnerId` is the same immutable ID created by the administrator; `runnerToken` is the new permanent credential.

The one-time registration token is never a Runner credential and cannot authenticate `/api/runner/ws`. It is not persisted by `agent-runner` and cannot be reused after successful enrollment. The permanent Runner token is stored only as a hash server-side and is returned in plaintext only when first issued or rotated.

External enrollment persists the normal execution configuration in `/etc/agent-board/agent-runner.env` using `AGENT_BOARD_URL`, `AGENT_RUNNER_ID`, `AGENT_RUNNER_TOKEN` and `AGENT_RUNNER_WORKSPACE_ROOT`. `AGENT_RUNNER_TOKEN` always means the permanent post-registration Runner credential. Normal startup remains the existing `configFromEnv()` -> `Server.Connect()` path; there is no additional Runner state/configuration file.

Hostname is only the initial display-name source. An administrator may rename a registered external Runner later. Reconnects and capability/health updates do not resend or overwrite that name, and the Runner process cannot rename itself.

The server-managed internal Runner continues to use the same ordinary permanent Runner credential generation/hash/authentication path without going through external enrollment. It is immediately registered and server-managed.

Required relationships:

- one Runner may execute many Execution Sessions over time
- one active Runner Execution Session owns one process tree and one Issue checkout/worktree
- one Run may use Execution Sessions over its lifetime, but a blocking native Question continues the same live Runner Execution Session
- one logical writer owns an Issue checkout during an active Runner execution lifecycle
- local Issue branch authority remains server-side; remote Issue branch authority is the published Git branch plus the durably recorded Workspace revision

## Git-native Workspace ownership

Every Issue uses one deterministic durable branch:

```text
agent-board/<issue-key>
```

The branch and Git commits are the durable code state. The Runner does not create candidate snapshots, Review filesystem snapshots or synthetic Base/Index/Worktree state.

### Shared finalization invariant

Both local and remote Runner execution use the same shared Git finalization rules. At a real hand-back boundary the checkout must:

1. still be on the expected `agent-board/<issue-key>` branch
2. have no unresolved conflicts
3. still contain the recorded execution-start revision in its history
4. preserve any commits created by the Engine
5. commit remaining tracked changes, deletions and non-ignored untracked files with the controlled Agent Board fallback identity when needed
6. finish as a clean checkout

A branch switch, history rewrite past the recorded start revision or unresolved conflict fails finalization. Agent Board retains recoverable state instead of persisting the wrong revision.

Ignored untracked files remain excluded.

### Local Project transfer

For `source_type=local`, the server owns the persistent Issue Workspace and exact Issue branch HEAD. Before Runner execution, Agent Board transfers that branch history through the existing #71 bounded/checksummed transfer protocol. The Runner checks out the exact transferred branch and revision.

This is a branch transfer, not a filesystem-state snapshot. The old staging-preservation contract and synthetic Base -> Index -> Worktree transport commits are superseded. A clean Git commit is the execution boundary.

After finalization, the Runner returns the finalized Issue branch. The server verifies/imports it and durably records the returned revision before sending `transfer_applied`. Missing acknowledgement or unsafe import leaves the Runner Workspace available for recovery.

### Remote Git Project checkout

For `source_type=git`, the Runner reaches `clone_url` with the Git authentication already configured on that host. Agent Board does not forward provider credentials and does not use provider-specific APIs in this path.

The Runner owns an execution-side bare cache per repository identity and creates a per-Run worktree. Source refs and Issue refs are deliberately separate:

```text
refs/remotes/origin/*     fetched remote source branches
refs/heads/agent-board/*  Agent Board Issue branches
```

The cache fetch mapping is equivalent to:

```text
+refs/heads/*:refs/remotes/origin/*
```

Remote heads are not fetched into local `refs/heads/*`. The configured Project ref is used when present; otherwise the Runner refreshes and follows `refs/remotes/origin/HEAD`.

Mutations of one bare cache are serialized. Different repository caches can mutate concurrently. Distinct Issue branches use distinct worktrees, so one Issue execution does not reset or rebase another Issue branch.

On later Runs the Runner fetches and continues the durable remote Issue branch. The recorded Workspace revision must match or be provably contained in the expected Agent Board branch history. Unexpected remote advancement or rewrites fail closed.

### Remote publication and acknowledgement

Remote finalization publishes only the Issue branch and always uses a normal non-force push. Publishing `agent-board/<issue-key>` is not the same thing as merging the remote target branch.

The Runner keeps the finalized worktree until the server durably records the published revision and sends the existing apply acknowledgement. This retained session is also the recovery authority if a push succeeds but its response, database write or acknowledgement is lost. The server may retry publication against the same retained Execution Session; pushing the same clean branch HEAD again is idempotent and remains non-force.

Only after durable persistence and acknowledgement does the Runner clean up the worktree. A later Runner can then fetch and continue the Issue from the recorded/published revision.

Arbitrary remote advancement is not accepted as recovery. If another actor moves the Issue branch, normal push/continuation ownership checks still reject it.

## Transport

The server and Runner communicate over one outbound WebSocket initiated by the Runner. Protocol v2 is the supported Runner protocol after the #68 migration.

Every execution message is scoped to a server-issued Execution Session identity. The wire protocol supports multiple sessions over the lifetime of one Runner connection even when the configured concurrency limit is one.

The protocol supports at least:

- Runner handshake and protocol-version negotiation
- Runner capability advertisement
- session start
- local branch transfer in both directions with bounded chunks/checksums
- remote Git prepare/publish control messages
- explicit server acknowledgement after durable returned/published revision persistence
- stdin streaming and stdin close
- stdout/stderr streaming with channel identity
- exit/result reporting
- graceful termination
- forced kill
- Runner/session errors
- liveness/connection reconciliation

A WebSocket disconnect is an infrastructure signal, not by itself durable proof that a Run or Engine process failed. The reconnect grace period starts when the live Runner connection is actually lost. Its server deployment setting is `AGENT_BOARD_RUNNER_RECONNECT_TIMEOUT`, which defaults to `5m` and accepts a positive Go duration. If the Runner reconnects within the grace period, Agent Board may reattach to the same active Execution Session; unresolved sessions fail only after that grace expires.

A newly authenticated connection claiming the same immutable `runner_id` does not blindly evict a live transport. The server compares old/new Runner health claims with durable active Execution Session ownership. Matching claims for the same durable session permit safe replacement/reattachment. Missing, mismatched or unexpected active-session claims are rejected/fail-closed so a takeover cannot discard ownership or enable duplicate Engine execution.

## Concurrency and future fleets

Default advertised capacity is:

```text
max active Execution Sessions per Runner = 5
```

A Runner advertises `max_active_sessions` so capacity can change without changing the identity model or transport contract. Missing or zero advertised capacity is treated as 5.

Workspace write safety remains separate from Runner transport concurrency. Supporting several protocol sessions does not imply that several writers may mutate one Issue branch concurrently. Repository-cache mutation is serialized per repository, while unrelated caches are independent.

## Engine ownership

Engine adapters stay in the trusted server.

Engine adapters own:

- Engine-specific command/invocation construction
- protocol parsing
- Model Profile/Provider configuration materialization
- mapping visible Engine activity into canonical Agent Board evidence

`agent-runner` stays Engine-neutral. It receives an authorized execution request and starts/supervises that process inside the Execution Session's prepared Issue checkout on the selected host.

## Runner responsibilities

The Runner owns only execution-plane behavior:

- authenticate and connect outbound to the control plane
- advertise versioned capabilities
- accept authorized session requests
- enforce the provided Workspace-bounded working directory
- prepare local transferred branches or remote Git worktrees
- start and supervise one process tree per Execution Session
- apply execution-scoped environment/secret values passed by the trusted server
- stream stdin/stdout/stderr
- report exit status/result
- propagate cancellation and graceful termination
- force-kill the process tree when required
- finalize the owned Issue branch at real hand-back boundaries
- retain session Workspace/worktree state until successful durable acknowledgement
- isolate session state and Issue branches between executions

The Runner does not own:

- PostgreSQL or scheduler state
- Issue/Board workflow
- Agent concurrency or Model Profile capacity admission
- Project authorization
- Provider credential storage/decryption
- Review decisions
- durable Event/evidence persistence
- remote target-branch integration

## Security boundary

External Runner hosts are user-managed, trusted execution environments. Agent Board does not claim Docker/container isolation for them. Operators decide what coding CLIs and host-level tooling are installed there.

The trusted server resolves and authorizes configuration and secrets before execution. The Runner receives only execution-scoped data needed by the session and a Workspace-bounded working directory. Internal Runner deployment may add container isolation, but that does not change the protocol or product model.

The Runner must never receive:

- PostgreSQL credentials
- backend encryption/signing keys
- Docker socket or daemon credentials from the control plane
- broad control-plane credentials
- arbitrary server filesystem access

Remote Git authentication is the Runner host's own Git configuration; Agent Board does not inject provider tokens into clone URLs for #72.

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

Question/Decision/resume state remains durable on the server. The Runner has no durable product-state responsibility beyond retaining its owned execution checkout until acknowledgement.

When OpenCode raises a blocking native Question, Agent Board persists that Question before the Run enters `WAITING_FOR_INPUT`. The same live Runner Execution Session, native Engine session and checkout remain attached while waiting. Agent Board does not finalize, return/push the branch, create a replacement Engine session or pay for a synthetic continuation prompt merely because human input is pending.

After the Decision is persisted, the Run resumes against that same native Engine session and checkout. Cancellation while waiting terminates the process tree and then uses the same bounded Git finalization/recovery path as other cancellation/failure boundaries.

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
