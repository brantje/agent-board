# Source control and repository-backed Workspaces

Project repository context is part of the v0.1 critical path. The first complete coding-agent flow uses a local Git repository source so repository execution can be proven without remote source authentication becoming a prerequisite.

## v0.1 Project repository

Project owns:

- local repository source/path
- default/base branch

The configured repository is a server-accessible Git repository. It is the bootstrap source for the Project Workspace; Agent Board does not treat it as the writable accepted checkout.

The local source path is backend-owned configuration and must be validated against the deployment's permitted repository roots. A browser/API caller cannot use Project repository configuration as arbitrary filesystem access.

### Deployment configuration

The server reads these filesystem settings:

- `AGENT_BOARD_REPOSITORY_ROOTS` — one or more absolute, backend-visible repository roots separated by the operating system path-list separator. With no authorized roots configured, local repository materialization is denied.
- `AGENT_BOARD_WORKSPACE_ROOT` — absolute durable Workspace storage root. The default is `/var/lib/agent-board/workspaces`.

Repository roots and configured Project repository paths are canonicalized through symlinks before access. A Project path must resolve to a directory at or below one authorized root; relative paths, sibling-prefix tricks and symlink escapes are rejected. Authorization is rechecked immediately before Git access rather than trusted only from configuration time.

When using the default Compose setup, `AGENT_BOARD_REPOSITORY_ROOT` is the host directory bind-mounted at `AGENT_BOARD_REPOSITORY_MOUNT_PATH` (default `/repositories`). `AGENT_BOARD_REPOSITORY_ROOTS` must contain the container-visible path, not the host path. For example, mounting `/srv/repos` at `/repositories` means a Project repository `/srv/repos/widget` is configured in Agent Board as `/repositories/widget`.

The repository bind mount is writable so Agent Board can create missing Project repository directories and initialize them as Git repositories on Project create/update. The configured repository remains a bootstrap source only; durable accepted and execution state still lives in backend-owned Workspaces.

The Workspace root remains writable and durable. Agent Board materializes configured repositories into backend-owned Workspaces before execution or approval delivery.

## Project Workspace

v0.1 uses exactly one durable **Project Workspace** per Project.

The Project Workspace is Agent Board's backend-owned writable Git checkout that represents the latest **accepted** state of the Project. It is distinct from both:

- the configured Project repository, which is an external/bootstrap source and may be read-only
- Issue Workspaces, which are mutable per-Issue working copies used by agents

The Project Workspace lives under `AGENT_BOARD_WORKSPACE_ROOT`, is never mounted into an agent Runtime, and is only mutated through trusted server-side lifecycle operations.

### Project Workspace initialization

On first use Agent Board:

1. resolves and re-validates the configured Project repository
2. materializes the configured default branch into a temporary Project Workspace checkout
3. records the exact source revision used
4. atomically publishes the checkout as the Project Workspace

Once initialized, later edits to Project repository configuration do not silently replace or reset the Project Workspace. Reinitialization/synchronization policy is explicit rather than an implicit side effect of configuration edits.

### Accepted state

The Project Workspace HEAD is the canonical accepted revision for local-repository v0.1 workflows.

Approval applies the exact candidate snapshot attached to the Review to the current Project Workspace under a Project-scoped serialization lock. The trusted approval input is the immutable backend-only Review delivery snapshot captured for that Run, not the current Issue Workspace and not the redacted candidate Artifacts exposed through Review/Run inspection. This separation allows public evidence to redact secret values without changing the source bytes that are accepted. The delivery snapshot is stored with durable backend Workspace state and is never exposed through public evidence APIs.

If application succeeds, Agent Board creates an internal acceptance commit and advances Project Workspace HEAD. The Issue is not marked `DONE` until the accepted commit and durable approval state are finalized.

If the Project Workspace has advanced since the reviewed Issue Workspace was created, approval attempts to apply the reviewed candidate on top of the newer accepted state. A real conflict fails the apply with an actionable error; Agent Board does not silently discard either accepted changes or reviewed candidate changes.

Approval is restart-safe and idempotent. A crash after filesystem/Git mutation but before database finalization must be detectable so reconciliation can complete without applying the same Review twice. Candidate-evidence retries for the same Run reuse the already-pinned Review delivery snapshot so the Review evidence and later approval cannot silently drift if the Issue Workspace changes.

The Project Workspace is internal accepted state only. v0.1 does not push its commits back to the configured source repository and does not create an external PR/MR as part of approval.

## Issue Workspace lifecycle

v0.1 uses exactly one persistent Issue Workspace per Issue.

On first execution:

1. ensure the Project Workspace exists and is ready
2. create or restore the Issue Workspace
3. materialize the current Project Workspace accepted revision into the Issue Workspace
4. create/use a deterministic Issue working branch
5. attach the Issue Workspace to the Run
6. mount it at `/workspace` in the Runtime

The Issue Workspace records the Project Workspace accepted revision from which it was materialized. Later attempts for the same Issue reuse the same Workspace and Git state, including modified, staged and untracked files.

A later approval on another Issue may advance the Project Workspace, but it does not silently rebase, reset or replace an already-reserved Issue Workspace. Any necessary conflict handling occurs explicitly when its reviewed candidate is approved.

Runtime Instance destruction never destroys or resets an Issue Workspace.

## Failure behavior

If a repository-backed Project is missing required configuration or the Project Workspace cannot be materialized, execution fails or waits with an actionable machine-readable reason.

Do not silently initialize an unrelated empty Git repository. Project and Issue Workspace bootstrap use temporary checkouts and atomically publish them only after the requested Git state is ready. Failed or interrupted temporary checkouts are cleaned before retry.

## Concurrency/restart safety

Project Workspace bootstrap and approval delivery are serialized per Project. Issue Workspace bootstrap is serialized per Issue.

PostgreSQL session advisory locking protects durable Workspace identities. A worker reloads durable state after acquiring the relevant lock, so retries reuse Workspaces already completed by another worker. If a checkout was atomically published but the final metadata write failed, a later attempt validates and adopts that checkout instead of cloning over it.

The Issue Workspace checkout and working branch are ready before Engine process execution begins.

## Provenance

Durable Project Workspace metadata records the configured repository identity and the accepted revision currently at HEAD.

Durable Issue Workspace/Run metadata records the Project Workspace accepted revision used as its base, working branch and repository identity. Review evidence remains pinned to the exact Run attempt and candidate snapshot even after either Workspace advances later.

## Remote Source Connections

Authenticated remote repositories are implemented after the first complete local-repository v0.1 flow is proven.

Planned Source Connection types:

- GitHub
- GitLab
- Bitbucket
- Forgejo

A Source Connection owns provider/server identity, encrypted credential references and health/validation state. Credentials never belong in repository URLs, Workspace metadata, Events, raw logs or provenance.

Remote clone/fetch and later source-provider actions such as PR/MR creation reuse the same Project Workspace / Issue Workspace model and trusted secret boundaries; they do not introduce another execution lifecycle.

## Delegation compatibility

Delegated work for an existing Issue inherits the current effective Issue Workspace state. Delegation does not implicitly create a clean repository checkout or advance the Issue to the latest Project Workspace revision.
