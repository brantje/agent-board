# Product roadmap

This roadmap defines ordering, not release dates. GitHub issues track concrete implementation work; canonical docs define durable product and architecture behavior.

Agent Board should keep extending one execution model instead of creating parallel systems:

Issue → Agent / Squad → Run → scheduler → Runner → Issue Git branch / Workspace → Engine → Review → delivery.

## Implemented foundation

The following are product baseline rather than future roadmap phases:

- durable asynchronous Runs and restart-safe scheduling;
- Agent + Model Profile capacity admission;
- external and server-managed Runners over protocol v2;
- Git-native Issue branches and SHA-pinned Review;
- local repository execution and remote Git Runner caches/worktrees;
- OpenCode execution, Questions/resume, evidence and Artifacts;
- built-in Users, Groups and Project authorization;
- Project-owned/shared Runner policy;
- MCP as a thin transport over the same application behavior;
- reusable Project-scoped Squads with mixed Agent/User membership and leader-only execution;
- canonical Agent delegation with explicit Allow delegation, ordinary delegated Runs, durable parent/child lineage, serialized same-Workspace/same-branch handoff, restart-safe result/parent continuation and trusted Squad-aware Agent target/member context.

Legacy Runtime/Runtime Instance support remains compatibility behavior where existing managed-compute code still uses it. It is not the canonical Agent/Runner configuration model.

## Current — collaboration follow-through

Canonical delegation, serialized Workspace handoff, delegated result/parent continuation and Squad-aware Agent target/member context are implemented on the normal Issue/Run/scheduler/Workspace path.

Remaining collaboration work should be added only where a concrete workflow needs it:

- human handoff/notification behavior;
- broader Agent messaging/wake behavior.

Do not add a Squad scheduler, delegation queue or alternate Workspace model.

## Next major product area — Tier-1 Source Providers

GitHub, GitLab and Forgejo are first-class Source Providers and should receive deep product integration.

They are not merely remote clone URLs.

See source-providers.md.

### Shared Source Provider foundation

Build the smallest shared application/domain model required by the three concrete providers:

- Source Connection identity, health and encrypted credentials;
- provider-backed repository identity and metadata;
- Project selection of Source Connection + repository;
- repository discovery/picker;
- provider-backed ephemeral Git credentials for Runner clone/fetch/push;
- provider-neutral Change Request identity;
- webhook verification/normalization;
- checks/pipeline and mergeability read models;
- provider API reconciliation.

Keep Git execution, Run scheduling, Issue branches, Workspaces and Review provider-neutral.

The existing generic git source remains available for unsupported Git servers and Runner-managed credentials.

### GitHub

Deep GitHub integration should include:

- GitHub App installation/authorization;
- installation-scoped repository discovery;
- repository picker;
- short-lived scoped credentials for Git/API operations;
- native signature/token verification;
- PR create/update/read;
- PR association with Agent Board Issues;
- checks/status rollup;
- mergeability/conflict state;
- external merge reconciliation.

### GitLab

GitLab.com and self-hosted GitLab should support the same product surface where the API permits it:

- authenticated instance connection;
- project/repository discovery;
- repository picker;
- ephemeral Git authentication;
- signed/token-authenticated webhooks;
- MR create/update/read;
- pipeline/status rollup;
- mergeability/conflict state;
- external merge reconciliation.

### Forgejo

Self-hosted Forgejo should receive the same Tier-1 treatment:

- authenticated instance connection;
- repository discovery;
- repository picker;
- ephemeral Git authentication;
- signed webhooks;
- PR create/update/read;
- status/check rollup where available;
- mergeability/conflict state where available;
- external merge reconciliation.

Bitbucket is not a Tier-1 requirement unless separately prioritized.

## Delivery policy

Source Providers unlock provider-aware delivery while preserving the existing Git/Review model.

Required progression:

- create/update a PR/MR from the existing agent-board/<issue-key> branch;
- expose PR/MR state, CI and mergeability on Issue/Review surfaces;
- reconcile external merge state through webhooks/API refresh;
- keep remote target branch as integration truth;
- add explicit Project delivery policy;
- allow optional autonomous PR/MR creation after a verified candidate;
- keep auto-merge/deploy/release as separate stronger permissions.

The default remains human-gated.

Provider webhook processing must not silently project external events into Issue Board state unless the Project explicitly enables that workflow policy.

## Work initiation and orchestration

Planning and automation continue to reuse normal Issues/Runs rather than introducing alternate task lifecycles:

- planning strategy: Auto / Always plan / Skip planning;
- Plan artifacts/read model;
- scheduled Project Automations creating normal Issues;
- Agent-created follow-up Issues under explicit Project policy;
- comments/mentions and collaboration-triggered wake behavior through canonical Issue/Agent commands;
- improved retry and operational UX.

Ordering inside this area follows concrete user value and existing issue dependencies.

## Execution topology and scale

After the core collaboration and source/delivery experience is solid:

- Worker registry;
- Worker Pools;
- warm/permanent capacity;
- spot/ephemeral capacity and recovery;
- scheduling preferences/classes where real placement needs require them;
- higher Runner session capacity where safe/useful.

Workers are compute, not Agents. Pools place capacity; they do not replace Run, Runner or Execution Session identity.

## Identity and administration expansion

The fixed local multi-user foundation is already implemented.

Future identity work should be added only for concrete requirements:

- external identity providers;
- MFA/WebAuthn;
- richer administration;
- organizations/tenants only if a real deployment model needs them;
- custom permissions only if the fixed deployment/Project roles prove insufficient.

Do not replace the existing authorization model speculatively.

## Product breadth

Later breadth may include:

- additional coding Engines;
- richer external triggers/integrations;
- broader automation policy;
- provider-specific delivery actions beyond the Tier-1 Source Provider baseline;
- explicit auto-merge/deploy/release workflows.

Exact ordering follows demonstrated user value.

## Plugins last

Plugin expansion remains the final major roadmap area unless explicitly reprioritized by a concrete product requirement.

Potential later Plugin work includes:

- installation/activation management;
- typed Actions and triggers;
- sandboxed UI extensions;
- MCP tools/resources/skills;
- SDK/packaging/ecosystem work.

Plugins must not become a shortcut for implementing core Source Provider, execution, authorization or collaboration behavior that belongs in Agent Board itself.

## Roadmap rule

A roadmap item should extend the existing authoritative model wherever possible.

When durable behavior changes, update the relevant canonical docs in the same work. Avoid duplicate schedulers, Run lifecycles, Workspace/source models, authorization rules and delivery state.
