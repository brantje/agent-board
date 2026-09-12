# Canonical execution context and secrets

Every Engine execution is prepared by one trusted Go execution-context resolver. Engines do not rediscover Agent Board configuration from PostgreSQL and do not receive database, encryption-key or broad control-plane capabilities.

## Resolution chain

```text
Run
  -> Project + Project instructions
  -> Issue title/description/current relevant context
  -> Agent + Agent instructions
      -> Engine
      -> Model Profile
          -> Provider
  -> Workspace identity/repository metadata
  -> explicit Question/Review/resume context
```

Agents select Engine and Model Profile. The scheduler selects an eligible connected Runner for execution; Runner placement is not Agent configuration.

The resolved non-secret context is immutable for the execution attempt and suitable for safe provenance capture.

Model identifier/generation settings come from Model Profile. Provider connection metadata comes from Provider. Runner hosts own the executable environment for production execution.

## Secret separation

```text
encrypted secret/reference
 -> authorization check
 -> resolve/decrypt in trusted Go boundary
 -> execution-scoped secret material
 -> inject only into the assigned Runner session
 -> redact before every durable sink
```

Provider credentials and other authorized execution secrets are never persisted into:

- Run rows/resume metadata
- scheduler jobs/reservations
- Execution Session durable state
- Events
- raw logs
- Artifacts
- provenance
- HTTP responses

## Provider credentials

Provider credentials are decrypted only for authorized execution. The deployment encryption key remains backend-owned.

The Engine receives safe Provider/model configuration plus only the narrow ephemeral secret material needed to configure its process.

## Secret write capability

Secret-write HTTP operations are privileged deployment administration surfaces. v0.1 does not invent the later users/groups/roles model for these operations; instead, when secret storage is configured, writes require the deployment-scoped `AGENT_BOARD_SECRET_WRITE_TOKEN` capability presented through `X-Agent-Board-Secret-Write-Token`.

The capability grants secret-write authority across the deployment, including global and Project-scoped secrets. Project-scoped handlers still pass the target Project into the authorization boundary and validate that Project before persistence, so a future scoped authorizer can replace the deployment capability without changing handler semantics.

The secret-write capability remains backend/operator-owned. It is never persisted in Agent Board data, injected into execution sessions, or treated as caller-supplied actor identity.

## Source-control credentials

Remote Git Projects use the selected Runner host's own Git authentication. Agent Board does not persist provider credentials in clone URLs and does not forward source-provider secrets through the Runner protocol merely to clone or publish the Issue branch.

Future authenticated Source Connections may use the same trusted secret boundary for provider-specific actions without changing the Runner ownership model.

See `source-control.md`.

## Resume and Review context

Question answers continue the same Run where product policy says so. Native OpenCode Questions remain on the same live Runner Execution Session and native Engine session while waiting. Review changes may create a new attempt linked to the reviewed Run while continuing the same Issue branch.

Only explicit relevant human feedback is composed into execution context. Engines do not scan arbitrary historical Events and hidden reasoning is not product state.

## Secret redaction

Registered secret values are redacted before structured Events, raw output, Artifacts where applicable, provenance and application-log persistence.

Redaction applies before every durable boundary.

## Failure behavior

Missing/inaccessible configuration, failed decryption, unavailable Runner prerequisites or source-authentication failures produce stable actionable error codes before coding-agent process launch where possible.

Errors never include plaintext secrets, ciphertext, full environments or sensitive authorization material.

## Provenance

The safe resolved context is the source for immutable Run provenance. The selected Runner is attached as execution provenance after scheduler placement. Historical Run inspection never reconstructs execution truth from mutable current Agent/Model/Provider/Runner records.
