# Canonical execution context and secrets

Every Engine execution is prepared by one trusted Go execution-context resolver. Engines do not rediscover Agent Board configuration from PostgreSQL and do not receive database, encryption-key or broad control-plane capabilities.

## Resolution chain

```text
Run
  -> Project + Project instructions
  -> Issue title/description/current relevant context
  -> Agent + Agent instructions
      -> Executor Profile
          -> Engine
          -> Model Profile
              -> Provider
          -> Runtime
  -> Workspace identity/repository metadata
  -> explicit Question/Review/resume context
```

Executor Profile references Runtime directly.

The resolved non-secret context is immutable for the execution attempt and suitable for safe provenance capture.

Model identifier/generation settings come from Model Profile. Provider connection metadata comes from Provider. Runtime kind/image/resources/network/Workspace/secret/tooling/capability policy comes directly from Runtime.

## Secret separation

```text
encrypted secret/reference
 -> authorization check
 -> resolve/decrypt in trusted Go boundary
 -> execution-scoped secret material
 -> inject only into Runtime process/session
 -> redact before every durable sink
```

Provider/source-control/Runtime secrets are never persisted into:

- Run rows/resume metadata
- scheduler jobs/reservations
- Runtime Instance durable state
- Events
- raw logs
- Artifacts
- provenance
- HTTP responses

## Provider credentials

Provider credentials are decrypted only for authorized execution. The deployment encryption key remains backend-owned.

The Engine receives safe Provider/model configuration plus only the narrow ephemeral secret material needed to configure its process.

## Runtime secret references

Runtime declares allowed secret references. Before resolving one:

1. verify the ref is allowed by the selected Runtime
2. reject undeclared refs before secret resolution
3. authorize within current Project/Run context
4. resolve in trusted backend code
5. inject only at process launch

## Source-control credentials

Repository bootstrap uses the same trusted secret boundary. Source credentials are ephemeral and excluded from durable repository URLs, Git config, provenance and logs.

See `source-control.md`.

## Resume and Review context

Question answers continue the same Run where product policy says so. Review changes may create a new attempt linked to the reviewed Run while reusing the same Issue Workspace.

Only explicit relevant human feedback is composed into execution context. Engines do not scan arbitrary historical Events and hidden reasoning is not product state.

## Secret redaction

Registered secret values are redacted before structured Events, raw output, Artifacts where applicable, provenance and application-log persistence.

Redaction applies before every durable boundary.

## Failure behavior

Missing/inaccessible configuration, failed decryption, unauthorized secret refs, unavailable Runtime prerequisites or source-authentication failures produce stable actionable error codes before coding-agent process launch where possible.

Errors never include plaintext secrets, ciphertext, full environments or sensitive authorization material.

## Provenance

The safe resolved context is the source for immutable Run provenance. It captures the direct Runtime selected by Executor Profile. Historical Run inspection never reconstructs execution truth from mutable current Agent/Executor/Model/Provider/Runtime records.
