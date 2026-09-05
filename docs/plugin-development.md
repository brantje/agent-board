# Plugin development

This guide describes the v0.1 local workflow for authoring Agent Board Plugin Packages. The server remains authoritative for package validation, Project scoping, permission grants, Action approval, MCP approval, secret access, and network security.

## Package layout

A development Plugin Package is a directory containing `agent-board.plugin.json` plus any package-relative UI and resource assets referenced by the manifest.

For example:

```text
my-plugin/
  agent-board.plugin.json
  ui/
    issue-panel.html
  skills/
    release-checklist.md
```

Plugin versions are immutable after installation. Changing manifest or asset bytes means building and installing a new version.

## Validate and build

From the Agent Board repository:

```bash
pnpm plugin validate ./examples/plugins/issue-notes
pnpm plugin build ./examples/plugins/release-automation /tmp/release-automation.package.json
```

The root `pnpm plugin` command keeps `tsx` scoped to `@agent-board/server`, while plugin-directory and output paths are resolved from the directory where the root command was invoked. The repository-relative paths above therefore work as written.

`validate` parses the manifest and checks canonical Action, Plugin UI, MCP, skill, permission, and referenced-asset contracts before installation.

`build` performs the same validation and emits an installable JSON package document containing the manifest, immutable UI/resource assets, `archiveRef`, and a deterministic SHA-256 package digest. The build helper never creates permission grants or MCP approvals. Server-side validation and policy checks still run at install and invocation time.

The TypeScript helper surface is exported as:

```ts
import {
  buildPluginPackage,
  completed,
  createHostBridgeRequest,
  failed,
  refused,
  validatePluginPackage
} from '@agent-board/contracts/plugin-sdk'
```

## Plugin UI

The initial canonical UI contribution is `issue_panel`. Its `entryAsset` must reference package-owned HTML. The host loads that HTML in a sandboxed iframe and accepts only the validated message bridge.

A request envelope has this shape:

```js
parent.postMessage({
  source: 'agent-board-plugin-ui',
  kind: 'request',
  request: {
    id: 'request-1',
    method: 'storage.get',
    scope: 'project',
    key: 'example-key'
  }
}, '*')
```

The host owns Project, Issue, activation, and surface identity. Plugin code does not send arbitrary Project or Issue IDs and does not receive Agent Board authentication credentials.

See `examples/plugins/issue-notes` for Project-scoped and per-user storage through the bridge.

## HTTP Actions

Plugin Actions remain declarative HTTPS Actions. Their typed input/output schemas are runtime validated, required permissions must be declared by the manifest, and the current Project Activation must actually grant them before invocation.

Event-triggered Actions execute from persisted Events. Scheduled Actions use durable server-owned schedules. A retry of one delivery reuses its stable delivery identity; plugin services should use that identity for idempotency rather than treating each retry as new work.

## MCP servers and tools

MCP is declared separately under `manifest.contributions.resources`:

```json
{
  "id": "release_tools",
  "type": "mcp_server",
  "title": "Release tools",
  "description": "External release tooling",
  "url": "https://mcp.example.com/mcp",
  "requiredPermissions": ["network:mcp.example.com"]
}
```

Agent Board's v0.1 external transport targets MCP revision `2026-07-28`. It uses stateless HTTP requests for `tools/list` and `tools/call`, with the required protocol/method routing metadata. Agent Board does not execute third-party MCP server packages inside the Backend process.

Discovery and authorization are separate:

1. A human grants the exact MCP server network permission for the Project Activation.
2. Agent Board discovers tool name, description, and validated input schema.
3. The discovered tool is not exposed to Agents yet.
4. A human explicitly approves the tool for that activation.
5. Approval stores the exact SHA-256 digest of the discovered input schema.
6. Only a currently discovered tool whose digest still matches its approval is exposed to Agents.
7. If the schema changes, the prior approval is invalidated and is not carried forward to the changed schema; the new schema must be approved explicitly.

Every discovery/invocation rechecks the active package, Project Activation, current grants, public DNS resolution, and destination restrictions. Agent Board does not forward its cookies or authentication tokens to MCP servers.

## Skills

A skill is an immutable package resource:

```json
{
  "id": "release_checklist",
  "type": "skill",
  "title": "Release checklist",
  "description": "Release preparation guidance",
  "entryAsset": "skills/release-checklist.md"
}
```

Initial skill assets are UTF-8 Markdown or plain text. Active skill responses retain Project, activation, package version, plugin ID/version, and resource provenance.

Skills grant zero permissions. Text in a skill may explain when to use a capability, but it cannot authorize an Action, MCP tool, network host, secret, or other Project resource.

## Local development security

Local development does not introduce a production-security bypass. In particular:

- HTTP Actions and MCP endpoints remain HTTPS-only under the normal server contract.
- Network permissions remain exact public DNS host grants; localhost, IP literals, private/link-local targets, intranet-style names, wildcards, and metadata destinations remain forbidden.
- Runtime DNS resolution is checked before network activity and outbound connections are pinned to the validated address.
- Redirects are not followed by the v0.1 MCP transport.
- Plugin UI remains sandboxed and does not receive Agent Board auth state.
- Secrets remain references until an authorized server-side boundary resolves them; plaintext secret values do not belong in manifests, Plugin UI, Events, logs, or built package metadata.
- Package/SDK validation is developer feedback, not an authorization decision. The server always validates and enforces policy again.

For a local remote service, use an explicitly controlled HTTPS endpoint with a public DNS hostname rather than weakening the Agent Board network boundary.

## Clean-room examples

The examples in `examples/plugins` are original Agent Board examples:

- `issue-notes` — sandboxed Issue panel with Project and per-user storage.
- `release-automation` — typed HTTP Action, persisted Event trigger, durable schedule, MCP server requiring explicit tool-schema approval, and a packaged zero-permission skill.

Their manifests and package builds are covered by automated contract tests.
