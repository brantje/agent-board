# Go Issue ownership

The Go strangler gateway owns the Project Issue **collection** boundary:

- `GET/HEAD /api/projects/:projectId/issues`
- `POST /api/projects/:projectId/issues`
- native `OPTIONS` preflight for that exact collection path

It also owns the Issue **update** boundary:

- `PATCH /api/projects/:projectId/issues/:issueId`
- native `OPTIONS` preflight for that exact Issue path

And it owns the Issue **relationship** read, create, and delete boundary:

- `GET/HEAD /api/projects/:projectId/issues/:issueId/relationships`
- `POST /api/projects/:projectId/issues/:issueId/relationships`
- `DELETE /api/projects/:projectId/issues/:issueId/relationships/:relationshipId`
- native `OPTIONS` preflight for those exact relationship paths

Issue assignment/orchestration is now separately Go-owned as documented in `docs/go-issue-assignment.md`. Other nearby individual-Issue behavior remains TypeScript-owned unless another migration slice already owns that exact subresource. Questions, Reviews, Run start/resume/transition behavior, scheduler/worker execution, Workspace/Git work, and Runtime execution are not transferred by this Issue collection slice. `GET /api/projects/:projectId/issues/:issueId` also remains on the TypeScript fallback.

## Durable invariants

PostgreSQL remains authoritative. Go lists from the canonical `issues` and `projects` rows and creates an Issue in one transaction that:

1. atomically increments `projects.next_issue_number`, obtaining the old value as the new Issue number;
2. inserts the Issue using that Project-scoped number; and
3. commits both changes together.

This preserves the existing TypeScript allocation semantics under concurrent Issue creation. There is no in-memory counter or queue.

Issue updates preserve the TypeScript optimistic status invariant. The handler first reads the current Issue, applies the same protected Review/DONE transition checks, then updates with `where status = <observed status>`. If another request or orchestration path changes the status between the read and write, the update does not overwrite that transition and the API returns `issue_status_changed`. A concurrent delete still returns `issue_not_found` after the failed compare-and-swap. Non-status edits remain allowed while an Issue is in `REVIEW` or `DONE`, exactly as in the TypeScript route.

Issue relationships use the canonical `issue_relationships` table and preserve its Project-scoped foreign keys, source/target non-self constraint, relationship type constraint, and uniqueness key `(project_id, source_issue_id, target_issue_id, type)`. Relationship listing remains source-scoped and deterministic by `created_at asc, id asc`. Deletion is scoped by Project, source Issue, and relationship ID.

The public DTO remains compatible with `packages/contracts`: Issue keys are `<issuePrefix>-<number>`, `assignedAgentId` is populated only for agent assignees, nullable execution/assignment fields remain nullable, relationship DTOs preserve their Project/source/target/type fields, and timestamps use the existing TypeScript-compatible UTC millisecond format.

## HTTP compatibility

Issue creation preserves the existing strict request contract: trimmed title (1-500 UTF-16 code units), description up to 100,000 UTF-16 code units, the canonical Issue status enum, integer priority `0..4`, defaults of empty description / `BACKLOG` / priority `0`, and rejection of unknown fields. Missing Projects continue to return `project_not_found`; invalid Project IDs and bodies retain the `validation_error` envelope.

Issue updates preserve the strict non-empty `updateIssueSchema` contract. `title`, `description`, `status`, and `priority` remain optional individually; unknown fields are rejected; title uses ECMAScript trim before the same 1-500 UTF-16-code-unit validation; description retains its 100,000-unit maximum; status uses the canonical enum; and priority remains an integer from `0` through `4`. The protected transition errors remain `issue_done_requires_review_approval`, `issue_review_requires_decision`, and `issue_done_requires_reopen`. A stale optimistic write returns `issue_status_changed` rather than overwriting the newer status.

Relationship creation preserves the strict `{ targetIssueId, type }` contract and the existing relationship types: `blocks`, `depends_on`, `related_to`, and `duplicates`. Source lookup is scoped to the requested Project and returns `issue_not_found`; missing or cross-Project targets return `issue_relationship_target_not_found`; self-reference returns `issue_relationship_self_reference`; duplicate relationships return `issue_relationship_exists`; missing source-scoped deletes return `issue_relationship_not_found`.

The TypeScript implementation is intentionally retained because direct TypeScript development and unmigrated orchestration still use the same canonical tables and routes. The Go gateway owns only the exact paths documented for each migrated slice; nearby commands and subresources continue through the TypeScript fallback unless explicitly documented as Go-owned.
