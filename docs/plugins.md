# Plugin architecture

> **Roadmap priority:** Plugins are deliberately late product work. Do not expand the Plugin system until the complete real coding-agent v0.1 flow is proven and higher-priority foundational product work has landed. Unless explicitly reprioritized later, users/groups/roles/permissions and other core administration work come before new Plugin features. See `roadmap.md`.

This document preserves the intended clean-room Plugin architecture so later implementation does not need to be rediscovered. Existing Plugin foundations may be maintained, but Plugin expansion must not compete with the v0.1 critical path.

## Goals

Plugins extend Agent Board without weakening core invariants:

- Project isolation remains mandatory
- the Go backend owns durable work and authorization
- plugin code is untrusted by default
- secrets never enter normal logs/UI/Event payloads
- important actions are durable/auditable
- plugin failure cannot corrupt core Board behavior
- human Review remains the delivery gate where applicable

Potential contributions include:

- UI surfaces
- typed Actions/tools
- event hooks
- scheduled hooks
- external HTTP integrations
- MCP tools
- Agent skills/resources
- namespaced storage

## Package model

### Plugin Package

Immutable versioned artifact containing a declarative manifest and static resources.

### Plugin Installation

Durable installation of one immutable Plugin Package version.

### Plugin Activation

Project-scoped enabling/configuration of an installed Plugin version. Activation owns Project configuration, grants, secret references, storage namespace and enabled contributions.

Package installation and Project activation are separate concepts.

## Manifest

Working filename:

```text
agent-board.plugin.json
```

The manifest is runtime validated and declarative; manifest fields are not executable code.

Conceptual shape:

```json
{
  "manifestVersion": 1,
  "id": "com.example.plugin",
  "name": "Example",
  "version": "1.0.0",
  "permissions": [],
  "configuration": {},
  "contributions": {
    "surfaces": [],
    "actions": [],
    "resources": []
  }
}
```

Versions are immutable. Installing an update creates a new version and activation changes explicitly select it. Historical Events retain plugin ID/version even after uninstall/deactivation.

## Project isolation

```text
Plugin Package Version
        |
        v
Plugin Installation
        |
        +--> Project Activation A
        |
        +--> Project Activation B
```

Each activation has independent config, grants, secrets and storage. Cross-Project access is rejected.

## Permissions

Declared permissions are requests, not authorization. Approved activation grants are authoritative.

Capability families may include:

```text
projects:read
issues:read
issues:write
comments:read
comments:write
runs:read
runs:write
events:read
artifacts:read
artifacts:write
storage:project
storage:user
secrets:use:<name>
network:<hostname>
```

Network is deny-by-default. Private/loopback/link-local/metadata/wildcard/IP-literal targets are rejected unless a future explicit safe policy says otherwise.

## Configuration and secrets

Typed configuration may include string/number/boolean/enum/multiline/secret-reference fields.

Secret values are resolved only by the trusted backend for an authorized invocation. Frontend Plugin UI never receives server-side secret plaintext.

## Plugin Actions

Actions are typed capabilities with validated input/output. Allowed invocation contexts may include human/manual, Agent, Plugin UI, persisted Event and schedule.

Agent-callable Actions are available only when:

1. the Plugin is active for the Project
2. required grants are approved
3. the Action explicitly permits Agent invocation
4. any extra tool approval policy is satisfied

Action refusal/failure is structured and auditable.

## HTTP transport

Remote Actions use outbound HTTPS with:

- approved-host enforcement
- strict timeout/body bounds
- SSRF protections
- request signing/auth support
- stable idempotent delivery IDs
- retry policy
- secret redaction

Do not execute arbitrary third-party packages inside the trusted Go backend process.

## MCP transport

External MCP tools are approval-driven:

- discover tool metadata/schema
- display before approval
- approve per Project Activation
- pin approval to schema digest
- schema change invalidates approval
- expose only approved tools

MCP infrastructure is untrusted external infrastructure and follows normal network/secret rules.

## Event hooks

Plugin event dispatch starts only from persisted Agent Board Events. Delivery is durable, has a stable ID and is safe to retry.

Causation/recursion safeguards prevent infinite Plugin event loops.

## Scheduled hooks

Plugin schedules are durable server-side state with stable occurrence identity, minimum-frequency/resource policy and activation enable/disable state.

First-class Agent Board Project Automations are a separate core product feature and should be implemented before relying on Plugins for normal recurring Agent work. See `automations.md`.

## UI surfaces

Third-party UI uses a strong sandbox such as an iframe with restrictive CSP and a narrow message bridge.

Potential surfaces include:

```text
issue_panel
project_settings_panel
issue_action
project_action
```

Plugin UI never receives Agent Board cookies/raw API credentials/database access.

The host bridge exposes explicit capability methods such as context/Issue reads, permitted comments/actions, Plugin storage, resize and theme context. Every mutation is permission checked and attributable.

## Storage

Provide namespaced key/value storage for Project Activation and optional per-user scopes. Plugins cannot select another Plugin's namespace.

Large files/results use the normal Artifact/blob subsystem rather than Plugin KV storage.

## Skills/resources

Plugin-provided Agent resources retain Plugin/version/activation provenance and are available only while relevant activation is enabled.

Skills/resources never grant permissions by themselves.

## Audit

Plugin install/update/activation/grant/config/storage/action/delivery/UI activity should be represented by durable audit/Event records where appropriate.

Never persist secret values in Plugin Events.

## Failure isolation

- UI failures do not break core Issue pages
- remote Actions have strict timeout/cancellation
- Event/schedule dispatch is asynchronous from core writes
- disabled/uninstalled Plugins are not invoked
- repeatedly failing hooks may be paused with an auditable reason

## TDD/security

When Plugin work eventually resumes, test at least:

- manifest validation
- immutable version behavior
- Project isolation
- permission denial
- secret non-disclosure
- SSRF/network restrictions
- Action validation/retry/idempotency
- event recursion prevention
- schedule occurrence identity
- MCP approval/schema invalidation
- UI sandbox/bridge enforcement
- uninstall/deactivation behavior

## Implementation ordering

Plugin development is not part of the v0.1 critical path.

When the roadmap eventually reaches Plugins, recommended internal order is:

1. package/install/activation model
2. permissions/config/secrets/storage
3. management UI
4. typed Actions + HTTP transport
5. event/schedule triggers
6. Agent-callable Actions
7. MCP approvals/transport
8. sandboxed UI + host bridge
9. skills/resources
10. SDK/examples/ecosystem tooling

This sequence begins only after higher-priority core and administration work. GitHub Plugin issues should remain deferred unless explicitly reprioritized.
