# Go Plugin Package management ownership

The Go strangler backend now owns the installed Plugin Package registry and package lifecycle HTTP boundary.

## Native Go routes

Go owns:

- `GET/HEAD /api/plugin-packages`
- `POST /api/plugin-packages`
- `GET/HEAD /api/plugin-packages/:packageVersionId`
- `POST /api/plugin-packages/:packageVersionId/uninstall`
- native `OPTIONS` preflight for those exact routes

Unknown Plugin Package subroutes and all other Plugin surfaces continue through the existing TypeScript compatibility fallback.

## Durable source of truth

Go reads and writes the canonical PostgreSQL `plugin_package_versions` table. It does not introduce an in-memory Plugin registry.

Package installation preserves the existing invariants:

- Agent Board Plugin Manifest v1 validation remains required.
- Plugin ids remain reverse-DNS identifiers and versions remain semantic versions.
- declared permissions, configuration fields, Action contributions, MCP/Skill resource contributions, UI asset paths, and resource asset paths are validated before persistence.
- HTTP Action and MCP transports remain HTTPS-only and require the corresponding declared `network:<hostname>` permission.
- Action signing-secret references remain permission-gated through `secrets:use:<name>`.
- Issue-panel entry assets must resolve to installed HTML assets.
- Skill entry assets must resolve to installed Markdown or plain-text resource assets.
- package `(plugin_id, version)` identity is immutable once installed: re-installing the same version with a different archive digest returns `plugin_version_immutable`.
- immutable UI and resource assets preserve the existing conflict semantics.
- an idempotent re-install of the same installed version returns the existing package.
- an uninstalled version cannot be silently re-installed.

The package row, UI assets, and resource assets are persisted by the Go transaction path against the same canonical table used by the remaining TypeScript Plugin subsystems.

## Uninstall semantics

Uninstall is transactional. Go marks the package version as uninstalled and deactivates every still-active Project activation for that package version without deleting historical activation provenance. A subsequently attempted activation therefore continues to fail with `plugin_package_uninstalled`.

## Remaining TypeScript Plugin ownership

This migration does **not** move Plugin execution or background processing. TypeScript remains authoritative for:

- Plugin Actions and Action invocation/delivery
- durable Plugin Action event/schedule workers
- Plugin UI discovery, asset serving, and bridge execution
- Plugin resources and Skills consumption
- MCP discovery, approvals, and transport
- other Plugin workers/background behavior

Go stores the immutable package assets those subsystems consume, but it does not duplicate their production execution paths.

## Verification

Go coverage for this slice includes:

- exact route-ownership tests proving only the intended package routes bypass the compatibility fallback
- HTTP parity coverage for malformed manifests, install/list/detail, idempotent install, package-version immutability, UI/resource asset immutability, contribution-to-asset validation, uninstall, activation deactivation, and not-found/UUID validation behavior
- the existing Plugin Activation tests, which continue proving that Project activation/configuration/storage ownership remains native Go while Action/UI/resource routes remain fallback-owned

The TypeScript backend stays available only for the Plugin capabilities that have not yet migrated. This package-management slice does not make the overall TypeScript-to-Go backend rewrite complete.
