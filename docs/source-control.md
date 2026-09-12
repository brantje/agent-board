# Source control and repository-backed Workspaces

Git is the durable code-state model for Issue execution and Review. Agent Board does not maintain a second filesystem-snapshot or candidate-snapshot history beside Git.

Each Project uses the source contract introduced by #73:

```text
local
  -> server-accessible repository_path
  -> default/base branch

git
  -> clone_url
  -> optional ref
```

Every Issue has one deterministic durable working branch:

```text
agent-board/<issue-key>
```

For example, Issue `AB-12` uses `agent-board/AB-12`. Runs and Review attempts for the same Issue continue that branch instead of creating replacement code-state objects.

## Core invariant

At every real execution hand-back boundary, Agent Board requires:

```text
what Review sees
==
tree(review_revision)
==
what local delivery later integrates
```

A Review therefore pins Git identity, not a filesystem archive:

- `base_revision` — the exact Issue base commit
- `review_revision` — the exact Issue branch HEAD being reviewed

The Review diff is reproducible as:

```bash
git diff <base_revision>..<review_revision>
```

There is no separate immutable Review filesystem snapshot, candidate snapshot, Base/Index/Worktree transport state or competing acceptance history.

## Clean Git execution boundaries

An Engine may use Git normally during a Run and agent-created commits are preserved. At a real completion, failure or cancellation hand-back boundary, shared finalization logic:

1. verifies the checkout is still on the expected `agent-board/<issue-key>` branch
2. rejects unresolved conflicts
3. proves the recorded execution-start revision is still an ancestor of `HEAD`
4. stages tracked changes, deletions and non-ignored untracked files
5. creates one controlled Agent Board fallback commit only when uncommitted non-ignored work remains
6. verifies the resulting checkout is clean
7. records/publishes that exact branch HEAD

Ignored untracked files remain excluded. Finalization never resets or rebases an Issue branch onto a newer target branch.

`WAITING_FOR_INPUT` is not a hand-back boundary. A blocking Question keeps the same live Runner, Execution Session, native Engine session and potentially dirty Workspace. Agent Board does not finalize, transfer, push or recreate the Workspace merely because human input is pending.

## Local Project sources

A local Project points at a server-accessible Git repository. The source path is backend-owned configuration and is validated against the deployment's permitted repository roots; API/browser callers cannot use Project configuration as arbitrary filesystem access.

The server reads:

- `AGENT_BOARD_REPOSITORY_ROOTS` — absolute backend-visible repository roots separated by the operating-system path-list separator. Without an authorized root, local repository materialization is denied.
- `AGENT_BOARD_WORKSPACE_ROOT` — durable Workspace storage root. The default is `/var/lib/agent-board/workspaces`.

Repository roots and configured paths are canonicalized through symlinks before access. Relative paths, sibling-prefix tricks and symlink escapes are rejected. Authorization is checked immediately before Git access, not only when configuration is saved.

With the default Compose deployment, `AGENT_BOARD_REPOSITORY_ROOT` is the host directory bind-mounted at `AGENT_BOARD_REPOSITORY_MOUNT_PATH` (default `/repositories`). `AGENT_BOARD_REPOSITORY_ROOTS` must contain the container-visible path.

### Project Workspace

A local Project has one durable backend-owned **Project Workspace** representing the current accepted target-branch state. It is distinct from the configured repository source and from per-Issue Workspaces. It is never mounted into an agent Runtime.

On first use Agent Board materializes the configured target branch, records the exact revision and atomically publishes the Project Workspace. Later Project configuration changes do not silently reset an already-ready Project Workspace.

Local Review approval integrates the exact pinned `review_revision` into the current Project target under the existing Project-scoped lock. Git merge semantics are used; filesystem patches are not. If the target has advanced, clean integration may create a merge commit. A real conflict fails explicitly and leaves the target branch at its prior clean HEAD. The Issue is not marked `DONE` until delivery and durable approval state complete.

### Local Issue execution and Runner transfer

Local Issues use persistent server-owned Issue Workspaces and `agent-board/<issue-key>` branches. The branch is created from the exact target revision chosen when the Issue Workspace is first materialized. Later target advancement does not silently reset or rebase that Issue branch.

When execution occurs on an external Runner, #71's transport remains the synchronization edge, but the payload is Git-native and branch-only:

```text
server Issue branch
 -> bounded/checksummed branch transfer
 -> Runner checkout of the exact branch HEAD
 -> Engine execution
 -> shared finalization
 -> returned branch history
 -> server imports exact Issue branch revision
 -> server sends apply acknowledgement
```

The old synthetic Base -> Index -> Worktree commits, server staging-preservation transport and filesystem-delta overlay are not part of the current architecture. Execution boundaries are clean branch HEADs.

The Runner retains the transferred Workspace until the server has durably imported the returned branch and acknowledged the apply. Missing acknowledgement or unsafe import retains recovery state rather than deleting it.

## Remote Git Project sources

A `source_type=git` Project uses the Project's `clone_url` and optional `source_ref`. The Runner reaches the repository with the Git credentials already configured on that Runner host. Agent Board does not add provider-specific source credentials or source-provider APIs as part of this architecture.

Remote execution does not transfer the full Project Workspace from the server. The selected Runner maintains a bare repository cache and a per-Run worktree:

```text
remote repository
 -> Runner bare cache
 -> per-Run Issue worktree
 -> Engine execution
 -> shared finalization
 -> normal Issue-branch push
```

### Ref namespaces

Runner caches keep fetched source branches separate from Agent Board Issue branches:

```text
remote source branches:
  refs/remotes/origin/*

Agent Board Issue branches:
  refs/heads/agent-board/*
```

The fetch mapping is equivalent to:

```text
+refs/heads/*:refs/remotes/origin/*
```

Remote heads are never fetched directly into the cache's `refs/heads/*`. After fetch, `refs/remotes/origin/HEAD` is refreshed so an unconfigured Project ref follows the remote's current default branch. A configured `source_ref` overrides that default.

### Bare cache and worktrees

The Runner keeps one bare cache per repository identity and serializes mutations of that cache. Different repository caches may mutate concurrently. Each active Issue execution uses its own worktree and its own `agent-board/<issue-key>` branch, so one Issue lifecycle does not reset or rebase another.

On first execution, Agent Board resolves the configured target revision, creates the Issue branch at that exact commit and checks it out in the Run worktree. On later execution, the Runner fetches and continues the published Issue branch.

The durable Workspace revision records the last Agent Board revision that the control plane has accepted for that Issue. Continuation must prove that the remote Issue branch matches or validly contains the recorded revision. Unexpected external advancement, history rewrites or missing recorded revisions fail closed; Agent Board does not treat arbitrary remote branch movement as its own work.

### Publication and acknowledgement

Remote Issue branches are published with an ordinary non-force push. Force-push is not part of the Issue lifecycle.

The Runner retains the finalized worktree until the server persists the published revision and sends the existing transfer/apply acknowledgement. This retention also provides bounded recovery for the failure window where the Runner successfully pushes revision `B` but the response, database update or acknowledgement is lost. The control plane can retry publication against the same retained Execution Session; republishing the same clean branch HEAD is idempotent and uses another normal non-force push. Only after the server durably records `B` does it acknowledge cleanup.

This recovery does not weaken continuation checks. If another actor advances the remote Issue branch to an unrelated revision, the normal push or later preparation still fails rather than adopting that advancement as Agent Board-owned state.

After the published revision is durably recorded, another Runner can fetch the same remote Issue branch and continue from that exact revision.

## Review and Request Changes

Review is Git identity plus Run evidence. A pending Review pins the Issue's `base_revision` and the current `review_revision` (Issue branch HEAD). Commands, tests, messages, artifacts, usage and provenance remain evidence about that Run, but they are not a second representation of the reviewed code.

Request Changes continues the same durable Issue branch:

```text
base A
 |
 B <- Review 1
 |
 C
 |
 D <- Review 2
```

Older Reviews remain immutable because their pinned SHAs do not change when the branch advances.

Approving one Issue must not mutate another Issue branch. Local delivery changes only the Project target plus the approved Issue's lifecycle state.

## Local target delivery versus remote publication

For a local Project, the Project Workspace is the target-branch integration truth inside Agent Board. Approval integrates the pinned reviewed commit into that target under the Project lock.

For a remote Git Project, publishing `agent-board/<issue-key>` is **not** target-branch integration. PR/MR provider APIs and remote target integration are outside #72. Internal Review approval must not pretend that publishing an Issue branch merged the repository's target branch.

## Failure, cancellation and recovery

Completion, failure and cancellation use the same bounded Git finalization rules when the checkout can be proven safe. Trustworthy partial work may therefore be committed and returned/published. If branch ownership, ancestry, conflict state, push/import or acknowledgement cannot be proven safe, Agent Board retains the Workspace/worktree and reports the failure instead of resetting or deleting it.

Runtime Instance destruction does not define Issue code state. Git commits and branches do.

## Concurrency and restart safety

Project Workspace bootstrap and local approval delivery are serialized per Project. Issue Workspace bootstrap is serialized per Issue. Runner bare-cache mutation is serialized per repository cache while unrelated caches remain independent.

PostgreSQL locking protects durable server-side Workspace identity. Runner ownership and Execution Session fencing remain authoritative while an Engine is active or waiting for input.

## Provenance

Durable Workspace/Run metadata records the repository identity, deterministic Issue branch, exact base revision and current accepted/published Issue revision. Review stores the exact base and review SHAs. These identities are sufficient to reproduce reviewed code through Git.

## Superseded snapshot/staging assumptions

Earlier #15-era documentation described candidate filesystem snapshots, preservation of arbitrary staged/unstaged state across Runner transport, synthetic Base/Index/Worktree commits and a separate Review delivery snapshot. Those assumptions are superseded by the Git-native model above and are no longer authoritative.

The authoritative code state is now normal Git branch/commit history. Evidence remains evidence; it does not duplicate source state.

## Future source-provider integrations

Future GitHub/GitLab/Bitbucket/Forgejo integrations may add Source Connections and PR/MR operations. They must build on the same Project source contract, durable Issue branch, Review SHA identity and execution lifecycle rather than introducing a second synchronization or code-state model.
