# Go Plugin Activation configuration ownership

The Go strangler backend owns the Project-scoped Plugin Activation configuration boundary:

- `GET/HEAD /api/projects/:projectId/plugin-activations`
- `POST /api/projects/:projectId/plugin-activations`
- `GET/HEAD /api/projects/:projectId/plugin-activations/:activationId`
- `POST /api/projects/:projectId/plugin-activations/:activationId/deactivate`
- `PUT /api/projects/:projectId/plugin-activations/:activationId/grants`
- `PUT /api/projects/:projectId/plugin-activations/:activationId/config`
- `GET/HEAD /api/projects/:projectId/plugin-activations/:activationId/audit`
- `GET/HEAD/PUT /api/projects/:projectId/plugin-activations/:activationId/storage/:scope/:key`
- native `OPTIONS` preflight for those exact routes

Plugin package installation/uninstallation, Plugin Actions and their durable delivery/scheduling workers, Plugin UI/resource serving, MCP discovery/approval/invocation, and other plugin execution behavior remain TypeScript-owned. Those routes continue through the compatibility fallback.

## Durable source of truth

This slice uses the existing canonical PostgreSQL tables without a schema change:

- `plugin_package_versions`
- `plugin_activations`
- `plugin_storage_entries`
- `plugin_audit_events`

Go does not introduce a second plugin registry, cache, scheduler, or background processor. TypeScript plugin workers continue consuming the same activation/config/grant rows after the HTTP ownership handoff.

## Activation behavior

Activation remains Project-scoped and package-version based. The command:

1. verifies the requested immutable package version exists and is not uninstalled;
2. preserves the one-active-version-per-Project/plugin constraint;
3. returns the existing activation idempotently when the same package version is already active;
4. rejects activating another version of the same plugin while an activation is active; and
5. records attributable `plugin.activated` audit history for newly created activations.

Creation and deactivation persist their lifecycle audit row in the same PostgreSQL transaction as the activation state change, so an audit failure cannot leave an unaudited lifecycle mutation. Deactivation locks the activation row before deciding whether to append `plugin.deactivated`, preventing concurrent requests from creating duplicate lifecycle audit events.

Deactivation remains soft: `deactivated_at` is set while historical package/activation/audit provenance is retained. Repeating deactivation is idempotent and does not append duplicate deactivation audit records.

## Grants and configuration

Approved grants are validated against the plugin manifest's declared permissions. Invalid permission syntax and forbidden network targets remain rejected, including loopback/private-style hostnames and IP literals. A permission declared by a package is still only a request; it grants no authority until present in the activation's approved grants.

Configuration is validated against the manifest's typed configuration declaration. Unknown fields, wrong value types, missing required fields, and plaintext values for `secret_ref` fields remain invalid. Secret configuration stores references only. Reads defensively redact malformed historical secret values rather than returning plaintext.

Grant and configuration mutations preserve attributable append-only plugin audit events. Audit payloads contain metadata such as changed grant names, configuration keys, and secret-reference field names, never secret values.

## Namespaced storage

Project and user storage remain isolated by Project, activation, plugin identity, scope, and optional user identity. Storage requires the corresponding `storage:project` or `storage:user` grant on every read/write.

The existing limits remain authoritative:

- key length: 256 characters
- value size: 8 KiB encoded JSON
- keys per namespace: 64
- total bytes per namespace: 64 KiB

Quota accounting and `plugin.storage_written` audit persistence occur in the same PostgreSQL transaction as the storage write. Permission-denied attempts remain auditable without writing storage data.

## HTTP parity and isolation

The Go routes preserve the existing validation/error ordering and public error codes. Request validation occurs before Project lookup where the TypeScript route did so. Project and activation UUIDs remain validated, cross-Project activation IDs return not-found behavior, and storage keys/scopes preserve the existing contract. Storage keys containing `/` remain Go-owned through the native wildcard route instead of falling through the strangler gateway.

Actor attribution continues to use the existing transitional `x-agent-board-actor-kind` and `x-agent-board-actor-id` headers. This slice does not introduce or weaken authentication; it preserves the same current attribution semantics while moving route ownership.

## Migration boundary

This handoff intentionally does **not** migrate Plugin Action execution, event dispatch, schedule processing, MCP transport, UI bridge behavior, package/resource asset mutation, or any other plugin background authority into Go. TypeScript remains authoritative for those capabilities until their own coherent migration slices have parity coverage and an explicit ownership switch.
