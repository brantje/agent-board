# Source control and repository-backed Workspaces

Project repository context is part of the v0.1 critical path. The first complete coding-agent flow uses a local Git repository source so repository execution can be proven without remote source authentication becoming a prerequisite.

## v0.1 Project repository

Project owns:

- local repository source/path
- default/base branch

The configured repository is a server-accessible Git repository. Agent Board materializes it into the durable Issue Workspace before Engine execution.

The local source path is backend-owned configuration and must be validated against the deployment's permitted repository roots. A browser/API caller cannot use Project repository configuration as arbitrary filesystem access.

### Deployment configuration

The server reads these filesystem settings:

- `AGENT_BOARD_REPOSITORY_ROOTS` — one or more absolute, backend-visible repository roots separated by the operating system path-list separator. With no authorized roots configured, local repository materialization is denied.
- `AGENT_BOARD_WORKSPACE_ROOT` — absolute durable Workspace storage root. The default is `/var/lib/agent-board/workspaces`.

Repository roots and configured Project repository paths are canonicalized through symlinks before access. A Project path must resolve to a directory at or below one authorized root; relative paths, sibling-prefix tricks and symlink escapes are rejected. Authorization is rechecked immediately before Git access rather than trusted only from configuration time.

When using the default Compose setup, `AGENT_BOARD_REPOSITORY_ROOT` is the host directory bind-mounted read-only at `AGENT_BOARD_REPOSITORY_MOUNT_PATH` (default `/repositories`). `AGENT_BOARD_REPOSITORY_ROOTS` must contain the container-visible path, not the host path. For example, mounting `/srv/repos` at `/repositories` means a Project repository `/srv/repos/widget` is configured in Agent Board as `/repositories/widget`.

The Workspace root remains writable and durable. Repository source mounts may be read-only because Agent Board clones them into the Issue Workspace before execution.

## Workspace lifecycle

v0.1 uses exactly one persistent Workspace per Issue.

On first execution:

1. resolve the Project local repository configuration
2. validate the configured source is accessible and permitted
3. create or restore the Issue Workspace
4. clone/materialize the configured repository
5. start from the configured base branch
6. create/use a deterministic Issue working branch
7. attach Workspace to the Run
8. mount it at `/workspace` in the Runtime

Later attempts reuse the same Workspace and Git state, including modified, staged and untracked files. Project repository/base-branch changes do not silently replace or reset an already-reserved Issue Workspace source snapshot.

Runtime Instance destruction never destroys or resets the Workspace.

## Failure behavior

If a repository-backed Project is missing required configuration or the local source cannot be materialized, execution fails or waits with an actionable machine-readable reason.

Do not silently initialize an unrelated empty Git repository. Bootstrap uses a temporary checkout and atomically publishes it only after the requested base branch and deterministic Issue working branch are ready. Failed or interrupted temporary checkouts are cleaned before retry.

## Concurrency/restart safety

Workspace bootstrap for one Issue is race-safe under scheduler retries/restarts. Two workers cannot concurrently initialize or corrupt the same Issue Workspace.

PostgreSQL session advisory locking serializes materialization for the durable Workspace identity. A worker reloads durable state after acquiring the lock, so a retry reuses a Workspace already completed by another worker. If the checkout was atomically published but the final metadata write failed, a later attempt validates and adopts that checkout instead of cloning over it.

The Workspace checkout and working branch are ready before Engine process execution begins.

## Provenance

Durable Workspace/Run metadata records the canonical repository identity, base branch, exact base revision and working branch actually used.

## Remote Source Connections

Authenticated remote repositories are implemented after the first complete local-repository v0.1 flow is proven.

Planned Source Connection types:

- GitHub
- GitLab
- Bitbucket
- Forgejo

A Source Connection owns provider/server identity, encrypted credential references and health/validation state. Credentials never belong in repository URLs, Workspace metadata, Events, raw logs or provenance.

Remote clone/fetch and later source-provider actions such as PR/MR creation reuse the same Workspace and trusted secret boundaries; they do not change Workspace identity or introduce another execution lifecycle.

## Delegation compatibility

Delegated work for an existing Issue inherits the current effective Workspace state. Delegation does not implicitly create a clean repository checkout.
