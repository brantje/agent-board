# Authentication and authorization

This document defines the implemented local human authentication and Project authorization model introduced by #74–#79. The Go backend is authoritative; HTTP and Nuxt are adapters/presentation around these rules.

## Human identity and authentication

A deployment-global `User` has a unique normalized username and email, durable User ID, deployment role, status, password hash, `force_password_change` state and authentication version.

Local login accepts username or email plus password. Access tokens are JWTs; refresh tokens are opaque random secrets stored only as hashes. Access authentication re-loads authoritative User state and validates the token authentication version, so disabling a User or changing/resetting/admin-assigning their password invalidates already-issued access and refresh authentication immediately.

The first successful registration in a zero-User deployment race-safely creates the deployment administrator and closes public registration. Additional Users are created by deployment administrators as `pending` and activate through one-time hashed setup tokens. Setup/reset tokens expire after 24 hours.

Normal protected application access requires an active User who is not currently forced to change their password. The account password-change endpoint remains available to an authenticated User in the forced-change state.

## Deployment roles

Exactly two deployment roles exist:

```text
admin
member
```

A deployment `admin` manages Users, Groups and deployment-global configuration and has implicit Project-admin authority for every Project. A deployment `member` may create Projects and otherwise sees only Projects granted to them.

Groups contain Users only and never grant deployment roles.

## Project roles and grants

Exactly three Project roles exist:

```text
admin > member > viewer
```

A User's effective Project role is the highest role from direct `ProjectUserAccess` and every `ProjectGroupAccess` reachable through their Group memberships. There are no deny or override rules.

- `viewer`: read-only Project data, Issues, Runs, activity/evidence and Review data.
- `member`: viewer access plus ordinary workflow mutations such as Issue work, Agent assignment, supported Run actions, Question answers and Review decisions.
- `admin`: member access plus Project settings and Project access administration.

Deployment administrators evaluate as Project admins without synthetic access rows.

New Projects are private. Creation and the creator's direct User `admin` grant are persisted together. Every Project must retain at least one active direct individual User admin; Group-admin grants and implicit deployment-admin authority do not satisfy that invariant.

## Authoritative boundaries

Human requests follow this shape:

```text
authenticated User
 -> deployment role / effective Project role
 -> shared Go authorization boundary
 -> existing application command/query
 -> persistence/domain behavior
```

`ProjectAccessService` owns fixed Project-role decisions. Transport middleware provides exhaustive defense-in-depth coverage for the `/api/projects` collection and `/api/projects/{projectID}/...` resource tree, including SSE/event routes and nested resources. Project settings/access/configuration require Project admin; ordinary Project mutations require member; reads require viewer.

Deployment-global User/Group/authentication administration is enforced by the authentication application service. Deployment-global configuration routes—including Providers, Model Profiles, legacy Runtimes, Agents, Runners, repository settings and global secret writes—require deployment admin.

Runner machine enrollment and Runner WebSocket transport are machine-authenticated execution-plane surfaces and are intentionally separate from human deployment administration.

Caller-controlled user/admin/role headers never grant human identity or authorization.

## Authorization inventory

Every existing human-facing control-plane surface is intentionally classified under the fixed model rather than relying on route shape or frontend visibility alone.

| Surface | Required authority |
| --- | --- |
| `/healthz` | unauthenticated health-only |
| bootstrap status/register, login, refresh, setup/reset completion, logout-by-refresh-token | unauthenticated/auth-only token flows with their own lifecycle checks |
| current account/profile/password/session operations | authenticated User; forced-password-change access is limited to the account flow needed to replace the password |
| User administration, Group administration and authentication settings | deployment admin |
| deployment-global Providers, Model Profiles, legacy Runtimes, Agents, Runners, repository settings and secret writes | deployment admin |
| `GET /api/projects` | authenticated User; inaccessible Projects omitted |
| `POST /api/projects` | authenticated active deployment member/admin; creator receives direct Project admin |
| Project and nested Project reads, including Issues, Runs, Questions, Reviews, execution evidence, raw logs, Artifacts and Project SSE | effective Project viewer or higher |
| ordinary Project workflow mutations, including Issue work/relationships, assignment, supported Run operations, Question answers and Review decisions | effective Project member or higher |
| Project settings/configuration mutations and Project access/directories/grants | effective Project admin |
| Project effective-role read | effective Project viewer or higher |
| Runner enrollment/WebSocket execution transport | machine-authenticated execution plane, not human deployment authority |

Project-scoped configuration reads remain available at the viewer boundary where the current product exposes them; mutation remains Project-admin-only. Shared deployment-global configuration remains deployment-admin-only outside Project scope.

The inventory is enforced in shared application authorization plus transport middleware. Individual handlers must not invent alternate role vocabularies, trust caller-provided actor/admin headers or bypass the shared effective-role decision.

## Isolation semantics

Normal Users only receive Projects they can access. For inaccessible Project-scoped resources:

```text
Project collection -> inaccessible Project omitted
direct/nested Project resource -> 404 / not-found semantics
Project SSE -> no subscription; same not-found isolation
```

Authorization is resolved from the authenticated User and Project scope before nested Issue/Run/Question/Review/resource lookup, preventing resource IDs from becoming existence oracles across Projects.

Role/grant changes are authoritative on the next request; frontend state is not an authorization cache.

## Human actor attribution

Existing durable domain records that support human attribution continue using their existing `actor_type = HUMAN` shape. New authenticated human actions populate the existing `actor_id` with the durable authenticated User ID. Question answers and Review approve/request-changes paths use this identity.

Historical records are preserved when a User is renamed, disabled or re-enabled. This is not a generalized deployment-wide audit-log product.

## Browser behavior

Nuxt exposes login/bootstrap/setup/reset/forced-password-change flows, current-account/session management, deployment administration and Project access controls. UI role checks only determine navigation/control presentation; bypassing the browser must still be denied by the Go backend.

Without `Stay logged in`, credentials use browser-session/in-memory persistence. With it, access and refresh credentials use persistent browser storage. Authentication failures clear/recover credentials through the shared auth composable; no cookie-only authentication model is substituted.

Protected SSE, raw-output and Artifact reads use authenticated browser transports. Artifact downloads fetch the protected content with the bearer credential and then create a local browser download; access tokens are never placed in download URLs.

## Explicitly out of scope

This model does not add OIDC/OAuth/SAML/LDAP/SCIM, MFA/WebAuthn, organizations/tenants, custom roles, permission matrices, deny ACLs, per-resource ACLs, nested Groups, API keys/PATs/service accounts, Agent/Squad identity changes or a generalized audit-log/policy engine.
