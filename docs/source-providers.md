# Source Providers

Agent Board treats GitHub, GitLab and Forgejo as first-class **Source Providers** for repository identity, authentication, webhooks and pull/merge-request delivery.

This is deliberately separate from the existing model **Provider** concept used for model inference.

Model Providers configure model access for Agents. Source Providers connect Projects to hosted Git repositories.

The execution path remains provider-neutral: Project → Issue → Run → scheduler → Runner → Issue Git branch / Workspace → Engine → Review → delivery.

Source Provider integrations enrich and authenticate that existing Git lifecycle. They must not create a second Workspace, branch, Run, scheduler or Review model.

## Tier-1 providers

The first supported Source Providers are:

- GitHub
- GitLab
- Forgejo

These three are one product tier. Provider-specific APIs may differ, but Agent Board should expose the same product capabilities wherever the host supports them.

Bitbucket is not a Tier-1 requirement unless separately prioritized.

## Product model

### Source Connection

A Source Connection represents one authenticated provider account, installation or instance available to Agent Board.

It stores only the durable provider identity and encrypted authentication material required to obtain scoped API and Git access.

Core fields are expected to include a durable Agent Board ID, provider kind, display name, instance/base URL where applicable, external account or installation identity, encrypted credential material, and connection health/status.

Provider-specific authentication details remain behind the provider adapter. The rest of Agent Board must not depend on GitHub installation IDs, GitLab token shapes or Forgejo-specific API structures.

### Source Repository

A Source Repository is the provider-backed identity of a repository visible through a Source Connection.

Agent Board needs the provider repository ID, owner/namespace, name/path, web URL, clone URLs, default branch and archived/disabled state where available.

Projects select a Source Connection plus Source Repository instead of requiring users to manually copy a clone URL when a provider connection is available.

The provider repository ID is the durable external identity. Repository name/path and URLs are metadata that may change.

### Project source modes

A Project may use one of three practical source modes:

- **local** — server-visible local Git repository;
- **git** — provider-neutral clone URL using Runner-host Git authentication;
- **connected** — Source Connection + Source Repository using provider-backed ephemeral Git authentication.

The existing provider-neutral git source remains a useful escape hatch for unsupported Git servers and manually managed Runner credentials.

A connected source is the preferred path for GitHub, GitLab and Forgejo.

## Shared Source Provider boundary

GitHub, GitLab and Forgejo are three concrete current requirements, so a small shared provider boundary is justified.

It should cover only behavior needed by those implementations:

- connection validation and current-account identity;
- repository discovery and repository metadata;
- scoped Git credential resolution;
- pull/merge-request create, update and read;
- checks/pipeline/status summary;
- mergeability/conflict state where the provider exposes it;
- webhook verification and normalization.

Do not build a generic SCM plugin framework, arbitrary capability registry or extension system.

Provider transports remain thin. Durable product behavior belongs in shared application/domain code.

## Authentication

### GitHub

GitHub should use a GitHub App as the primary integration.

The connection flow should support installing/authorizing the Agent Board GitHub App, selecting repositories the installation may access, discovering those repositories in Agent Board, resolving short-lived installation credentials for API and Git operations, and receiving signed GitHub webhooks.

Agent Board should not require a long-lived personal access token for the normal GitHub path.

### GitLab

GitLab must support GitLab.com and self-hosted GitLab instances.

The connection stores the instance identity plus the minimum durable OAuth/token material required to obtain API and Git access. OAuth should be preferred where the deployment and instance support the configured flow; token-based administration may remain available where needed for self-hosted operation.

Repository/project discovery, merge requests, pipelines/status and webhooks use the GitLab API behind the shared Source Provider boundary.

### Forgejo

Forgejo must support self-hosted Forgejo instances.

The connection stores the instance identity plus the minimum durable OAuth/token material required for repository/API access. OAuth should be preferred where configured, with token-based setup available when that is the practical self-hosted administration path.

Repository discovery, pull requests, commit/check status and webhooks use Forgejo APIs behind the same shared product boundary.

## Git credentials and Runner execution

A connected Source Provider becomes the trusted credential source for remote Git execution.

Trusted server code resolves a scoped credential, injects it ephemerally through the existing execution-context/secret boundary, and the selected Runner uses it for clone/fetch/push of the selected repository.

Requirements:

- credentials are resolved by trusted server code;
- credentials are scoped to the selected repository and operation as narrowly as the provider allows;
- credentials are injected ephemerally through the existing execution-context/secret boundary;
- credentials are never persisted in clone URLs, Git config, Events, logs, provenance or Runner registration;
- Runners do not need machine-specific permanent Git credentials for connected Projects;
- a credential must not grant broad Agent Board control-plane access.

The scheduler remains responsible for selecting a Runner. Source Provider authentication must not become Agent configuration or a second placement system.

## Change Requests

Agent Board uses one provider-neutral **Change Request** concept for the external review object:

- GitHub → Pull Request;
- GitLab → Merge Request;
- Forgejo → Pull Request.

A durable Change Request record/reference contains only provider state Agent Board needs to correlate and inspect the external object: Project/Issue identity, Source Connection/Repository, external ID or number, web URL, state, draft state, head and target branches/SHAs where available, CI/check summary, mergeability/conflict summary and synchronization time.

Provider APIs remain authoritative for provider-owned state. Agent Board persists enough identity/snapshot state for reliable UI, webhook reconciliation and recovery; it does not recreate the provider's entire PR/MR database.

## Issue association

Agent Board-created Change Requests are linked directly to the originating Issue when created.

Externally created PRs/MRs may be associated when their provider event contains an unambiguous Agent Board Issue key through the deterministic agent-board/<issue-key> branch, the Issue key in the title, or a configured closing keyword such as Closes AB-123, Fixes AB-123 or Resolves AB-123.

A bare Issue-key mention in arbitrary body text must not be enough to trigger delivery/status behavior.

String matching is a recovery/discovery mechanism. It must not replace the exact durable association for Change Requests created by Agent Board.

## Webhooks and reconciliation

Each provider integration verifies its native webhook signature/token before resolving a Source Connection or Project.

Provider-specific payloads are normalized into shared application events such as repository metadata changes, Change Request opened/updated/closed/reopened/merged, head-SHA changes, and checks/pipeline status changes.

Webhook delivery is not assumed to be exactly once. Processing must be idempotent and safe under duplicate, reordered and delayed delivery.

Periodic or on-demand provider refresh may repair missed webhook state. The provider API remains the source of truth for current external PR/MR/check/mergeability state.

## CI, checks and mergeability

Issue and Review UX should expose the external Change Request together with provider/repository identity, PR/MR number and URL, open/closed/merged state, draft state, head and target branches/SHAs, additions/deletions/changed-file counts where available, checks/pipeline summary, and mergeability/conflict state where available.

Provider-specific status vocabularies are normalized to a deliberately small read model. Do not create a second CI system.

Detailed provider runs/jobs remain provider-owned and should link to the provider unless Agent Board has a separate concrete reason to ingest them.

## Delivery

Publishing agent-board/<issue-key> is not delivery to the target branch.

For connected remote Projects, delivery is: Issue branch → Review pinned to exact SHA → delivery policy permits provider action → create/update Change Request → provider CI/review/merge lifecycle → webhook/API reconciliation → external target integration recorded.

The external repository target branch remains integration truth.

Agent Board must never mark a remote change integrated merely because the Issue branch was pushed or an internal Review was approved.

## Project delivery policy

Source Provider support enables explicit Project delivery policy.

The default remains human-gated.

### Human-gated

A successful Agent Board Review may create/update the PR/MR, but provider merge and Issue Board status remain explicit external/human workflow.

### Complete on external merge

Where a Project explicitly enables it, a verified provider merge may complete delivery and set the Issue to DONE.

This is an opt-in workflow rule, not implicit webhook behavior.

### Autonomous PR/MR creation

A Project may explicitly allow a successful verified candidate to create/update its PR/MR without first requiring Agent Board's internal human approval.

This permission does not imply provider merge.

### Auto-merge/deploy

Automatic merge, deployment or release is a stronger capability and must be designed and authorized separately. It is never implied by Source Provider connectivity or autonomous PR/MR creation.

## Review relationship

Agent Board Review and provider PR/MR review are related but distinct.

Agent Board Review proves a specific base_revision + review_revision candidate. The provider Change Request exposes that candidate to the repository host.

If the Change Request head moves away from the pinned review_revision, Agent Board must show that the external object no longer represents the exact internally reviewed candidate. Delivery policy must fail closed rather than silently treating a different SHA as approved.

## Security

- Source Connection secrets are encrypted at rest and never returned plaintext after save.
- Webhooks are authenticated before any Project/Issue lookup that could disclose data.
- Provider callbacks use signed/validated state and cannot choose an arbitrary Project.
- Repository discovery respects the provider account/installation's actual scope.
- Provider-backed Git credentials are ephemeral and repository-scoped where possible.
- Git credential material is redacted before every durable sink.
- Provider external IDs and repository IDs are identity, never authorization.
- Human configuration/mutation uses the normal Agent Board deployment/Project authorization model.
- Runners receive only the credentials required for the selected execution.

## Architecture rules

Source Providers extend the existing source-control and delivery boundaries.

They must not introduce a Source Provider scheduler, provider-specific Run types, provider-specific Workspaces, provider-specific Issue branches, provider-specific Review objects replacing Agent Board Review, a second authorization model, transport-owned delivery policy, or provider-specific business logic duplicated across HTTP, MCP and workers.

Shared behavior belongs in the authoritative Go application/domain layer. HTTP/webhook/UI adapters translate provider-specific transport details into that shared behavior.

See also source-control.md, architecture.md, execution-context.md, execution-evidence.md and roadmap.md.
