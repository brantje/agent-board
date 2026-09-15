# Agent Board MCP server

Agent Board exposes a first-party Model Context Protocol endpoint from the existing Go server process at:

```text
http://<agent-board-server>/mcp
```

The endpoint uses Streamable HTTP in stateless mode and exposes tools only. It is another transport over the same Agent Board application/domain services used by the HTTP API; it does not own separate Issue, Run, Question, Review, authorization, scheduling, or evidence behavior.

## Authentication

Use an existing Agent Board human access token on every MCP request:

```http
Authorization: Bearer <access-token>
```

Authentication is performed by the existing Agent Board authentication service. MCP does not add API keys, service accounts, MCP-specific users, or a separate permission model.

Access tokens expire, so long-lived client configuration may require replacing the token when it expires.

## Authorization

Project access follows the same roles as the rest of Agent Board:

- `viewer` can use Project-scoped read tools;
- `member` can perform workflow mutations;
- `admin` keeps the existing Project administration authority;
- deployment administrators keep their existing implicit Project-admin access.

Projects the authenticated User cannot access are omitted from discovery. Direct access to inaccessible Project data preserves Agent Board's existing not-found isolation behavior.

## Identifiers

MCP uses the public Agent Board identifiers:

```text
projectId = Project UUID
issueId   = public Issue key, for example AB-123
```

Runs, Questions, Reviews, and relationships returned by MCP use public Issue keys rather than exposing the internal Issue database UUID as the normal Issue identifier.

## Tool surface

The initial MCP surface is intentionally limited to normal control-plane work:

- Project context, Project members, Agents, and valid assignees;
- Issue reads, metadata updates, Board status, ownership, and relationships;
- Issue execution state and explicit Run start/cancellation;
- Run inspection and redacted durable raw-output chunks;
- blocking Questions and answers;
- Reviews, approval, and change requests.

Administrative configuration, user/group administration, secrets, Runner administration, Provider/model configuration, Project access-grant mutation, MCP resources/prompts, comments, delegation, and arbitrary binary Artifact streaming are not part of this MCP version.

List tools return an object containing an `items` array. This keeps their structured output compatible with the official Go MCP SDK's object-schema requirement while preserving the underlying application list behavior.

## Workflow invariants

MCP preserves Agent Board's separation between Board state, ownership, and execution:

```text
Issue Board state  -> set_issue_status
Issue ownership    -> set_issue_assignee
Agent execution    -> start_issue_run / cancel_run
```

Changing status or assignment does not implicitly cancel Runs. Starting a Run does not change Board status or ownership. Scheduler admission/capacity, Question continuation, Review delivery gates, durable Events, and evidence redaction remain owned by the existing backend services.
