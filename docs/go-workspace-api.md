# Go Workspace API ownership

The Go strangler backend owns the existing Project/Issue Workspace HTTP boundary:

- `POST /api/projects/:projectId/issues/:issueId/workspace`
- `GET/HEAD /api/projects/:projectId/issues/:issueId/workspace`
- `GET/HEAD /api/projects/:projectId/issues/:issueId/workspace/status`
- `GET/HEAD /api/projects/:projectId/issues/:issueId/workspace/diff`
- native `OPTIONS` preflight for those exact routes

All other Workspace, Run, Issue-assignment, Review, Question-command, and execution routes continue through the TypeScript fallback unless separately documented as Go-owned.

## Durable ownership and isolation

PostgreSQL remains the durable source of Workspace metadata. Go reads and writes the canonical `workspaces` table and preserves the one-Workspace-per-Project/Issue constraint, `bootstrapping`/`ready`/`error` state transitions, retention metadata, and TypeScript-compatible timestamps.

Every HTTP operation validates the Project first and then the Issue in that Project. A Workspace cannot be read through another Project or Issue identity. Missing resources preserve the existing `project_not_found`, `issue_not_found`, and `workspace_not_found` contracts.

`POST` preserves create-or-restore behavior. A first creation returns `201`; an existing Workspace returns `200`. An errored Workspace is returned to `bootstrapping`, and a durable row is marked `ready` only after its checkout has been initialized successfully. Bootstrap failure resets the checkout directory, marks the durable row `error`, and returns `workspace_bootstrap_failed`.

## Filesystem and Git behavior

`WORKSPACE_ROOT` is mounted at the same absolute path into both the Go and TypeScript backends. This is intentional during the strangler migration: Go owns the manual Workspace HTTP API while TypeScript-owned Run execution may continue using the same checkout tree.

The Go Workspace Git runner preserves the existing hardening relevant to these routes:

- Workspace IDs resolve to one path segment below `WORKSPACE_ROOT`.
- Git hooks, global/system configuration, filesystem monitoring, pagers, interactive prompts, and SSH execution are disabled for Workspace commands.
- `.git` must be a real directory; linked-worktree `commondir` metadata is rejected.
- `file:` and `ext:` clone protocols are rejected.
- repository-less Workspaces initialize a local repository; configured repositories clone the requested base ref and create the requested working branch.
- status returns the existing `branch`, `clean`, `staged`, `unstaged`, and `untracked` DTO.
- diff uses `--no-ext-diff` and `--no-textconv` and returns `{ "patch": "..." }`.

This slice does **not** move automatic Run Workspace orchestration, Run scheduling, Runtime execution, retention cleanup, or execution lifecycle authority to Go. There is still only one authoritative Run scheduler/worker: the existing TypeScript implementation.
