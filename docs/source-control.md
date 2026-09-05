# Source control and repository-backed Workspaces

Project repository context is part of the v0.1 critical path. The first complete coding-agent flow uses a local Git repository source so repository execution can be proven without remote source authentication becoming a prerequisite.

## v0.1 Project repository

Project owns:

- local repository source/path
- default/base branch

The configured repository is a server-accessible Git repository. Agent Board materializes it into the durable Issue Workspace before Engine execution.

The local source path is backend-owned configuration and must be validated against the deployment's permitted repository roots. A browser/API caller cannot use Project repository configuration as arbitrary filesystem access.

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

Later attempts reuse the same Workspace and Git state.

Runtime Instance destruction never destroys or resets the Workspace.

## Failure behavior

If a repository-backed Project is missing required configuration or the local source cannot be materialized, execution fails or waits with an actionable machine-readable reason.

Do not silently initialize an unrelated empty Git repository.

## Concurrency/restart safety

Workspace bootstrap for one Issue is race-safe under scheduler retries/restarts. Two workers cannot concurrently initialize or corrupt the same Issue Workspace.

The Workspace checkout and working branch are ready before Engine process execution begins.

## Provenance

Durable Workspace/Run metadata records the safe repository identity, base revision and working branch actually used.

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
