# Frontend implementation policy

Agent Board's frontend is a **clean-room implementation** defined by the canonical Agent Board documentation and implemented with Nuxt 4, Vue 3, TypeScript, Tailwind CSS and Nuxt UI v4.

This document is authoritative for frontend implementation.

## Implementation inputs

Frontend implementation uses:

- Agent Board's canonical product, domain and architecture documentation
- Agent Board's documented public API/contracts
- Agent Board's independently written tests and product requirements
- current official Nuxt, Vue, TypeScript, Tailwind CSS and Nuxt UI documentation
- public web/platform standards and APIs

## Stack

```text
Nuxt 4
Vue 3
TypeScript
Tailwind CSS
Nuxt UI v4
```

The Go backend is Agent Board's control plane. Nuxt owns web rendering, routing and browser interaction. Durable product state, authorization, scheduling, Runs, Workspaces, Runtime execution, Questions/Review continuation, Events and audit state remain Go/PostgreSQL-owned.

Nuxt server routes, Nitro state, server middleware, process memory and framework caches do not become a second Agent Board control plane.

## Official sources

Agents must use current official documentation instead of guessing APIs from memory.

Use at minimum:

- `https://nuxt.com/docs/4.x/`
- `https://nuxt.com/llms.txt`
- `https://ui.nuxt.com/docs/`
- `https://ui.nuxt.com/docs/components/`
- the documentation page for the specific Nuxt UI component/composable being used

Nuxt provides LLM-oriented documentation, and Nuxt UI provides an MCP endpoint for component documentation and metadata.

## Mandatory Nuxt UI-first rule

**Before creating any generic UI primitive or interaction pattern, verify that Nuxt UI does not already provide the component or a suitable documented composition.**

Nuxt UI v4 provides 125+ accessible Vue components, including layout, forms, navigation, overlays, data display, dashboards, color-mode and AI/chat components.

Examples relevant to Agent Board include:

```text
UApp
UButton
UCard
UBadge
UAlert
UInput
UTextarea
USelect / USelectMenu
UCheckbox
USwitch
UForm / UFormField
UModal
UDrawer
UPopover
UDropdownMenu
UTabs
UTable
UAccordion
UTooltip
USkeleton
UProgress
USeparator
UEmpty
UTimeline
UDashboardSidebar
UDashboardNavbar
UDashboardToolbar
UColorModeButton
```

The catalog evolves. Agents must consult the current official component catalog before introducing a reusable primitive.

## Mandatory agent workflow

Before implementing frontend UI, an agent must:

1. Read this document and `docs/frontend-theme.md`.
2. Inspect the current `apps/web` Nuxt configuration and existing shared components/composables.
3. Consult `https://nuxt.com/llms.txt` for unfamiliar Nuxt behavior.
4. Search the current Nuxt UI component catalog for the required interaction.
5. Use the configured Nuxt UI MCP server for props, slots, events, examples and component metadata when available.
6. Compose Agent Board product UI from Nuxt UI components.
7. Create a custom low-level primitive only when no suitable Nuxt UI component/composition exists.

A custom primitive requires an explicit implementation/PR note containing:

- the Nuxt UI components/compositions checked
- why they do not satisfy the requirement
- the accessibility and interaction behavior the custom primitive provides

Convenience or personal styling preference is not sufficient justification.

## AI-agent support

The project configures the Nuxt UI MCP server in `.cursor/mcp.json`:

```text
https://ui.nuxt.com/mcp
```

Use it before guessing Nuxt UI component APIs. In particular, agents should search components and inspect component metadata/documentation/examples for unfamiliar controls.

Nuxt's `llms.txt` is the default framework reference for AI-assisted Nuxt work.

## Component ownership

Use this separation:

```text
apps/web/app/pages/*
  routing/page composition

apps/web/app/components/*
  Agent Board product components and deliberate reusable compositions

apps/web/app/composables/*
  frontend data/interaction composition

apps/web/app/assets/css/*
  shared theme/application CSS

apps/web/app/app.config.ts
  Nuxt UI theme/component defaults where appropriate
```

Generic product components do not own backend domain state. Shared presentation should be centralized instead of duplicating large utility-class strings across pages.

## Nuxt boundary

Nuxt may SSR/render pages and proxy or consume intentional Go APIs, but it is not an alternate product backend.

- authoritative mutations go through the Go API
- durable reads come from the Go API/PostgreSQL read models
- live Run/Issue state reconciles against persisted API/SSE state
- Nitro/server routes do not own Run scheduling, Workspace lifecycle, Runtime execution or authorization policy
- client-only state is never authoritative for execution progress

A browser reload or Nuxt process restart must not lose durable Agent Board state or execution progress.

## Scope discipline

Frontend work remains focused on the complete v0.1 flow:

```text
Local Project repository
 -> Issue
 -> Agent
 -> Run
 -> Runtime
 -> real coding Engine
 -> durable evidence
 -> Review
```
