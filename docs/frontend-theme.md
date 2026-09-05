# Frontend theme architecture

Agent Board uses **Nuxt 4 + Vue 3 + TypeScript + Tailwind CSS + Nuxt UI v4** for the web application.

Read `frontend-implementation.md` first. It defines the clean-room implementation boundary and mandatory Nuxt UI-first component workflow.

The visual goal is a dense technical control-panel aesthetic: dark by default, restrained steel-blue accent, square surfaces, clear borders, compact controls, and highly legible status/log information.

## Visual language

Agent Board should feel like a technical administration/control interface rather than a soft consumer SaaS product.

Core rules:

- cards and large panels are square by default (`0px` radius)
- inputs, buttons, dropdowns, menus, modals and similar controls use `0-2px` radius at most unless interaction semantics require otherwise
- pill shapes are reserved for controls where the shape communicates meaning, such as compact tags/chips/statuses; they are not the default treatment
- avoid large-radius cards, floating rounded containers and decorative soft-surface styling
- use tonal separation and thin borders to establish hierarchy instead of prominent shadows
- keep shadows absent or extremely restrained unless an overlay genuinely needs elevation
- keep layouts compact and information-dense while preserving comfortable pointer/keyboard targets
- small uppercase section/category labels are appropriate for navigation and settings group hierarchy

Square presentation is the default. A component should not become rounded merely because the underlying Nuxt UI default uses a larger radius; configure the shared theme/component default instead of adding one-off overrides everywhere.

## Color direction

The dark theme uses a neutral graphite hierarchy with a muted steel-blue primary/accent. Exact values may be tuned for contrast and accessibility; the visual relationship matters more than copying literal hex values.

Directional dark palette:

```text
Deep/sidebar surface      ~ #181818
Application canvas        ~ #202020 - #212121
Panel/card surface        ~ #292929 - #2B2B2B
Secondary/selected layer  ~ #242424 - #2E2E2E
Border/divider            ~ #343434 - #3D3D3D

Primary text              ~ #E5E5E5
Secondary text            ~ #A0A0A0
Muted/meta text           ~ #7D8287

Primary/accent            muted steel blue
Primary hover/active      slightly brighter steel blue
Focus                     accessible brighter blue derived from primary
```

The accent is intentionally sparse. Use it for active navigation, links, primary actions, selected controls, focus indication and other genuine emphasis. Do not wash large portions of the interface in blue.

Status colors for success/warning/error remain semantically distinct but restrained and readable in both dark and light modes. Status must never rely on color alone.

Prefer Nuxt UI semantic colors/theme configuration over raw palette values inside feature templates. Centralize the actual tuned palette so contrast can be improved without rewriting product components.

## Component system

Nuxt UI is the required shared component foundation.

Use Nuxt UI components directly for generic controls, overlays, forms, navigation, tables, dashboards, feedback and color-mode behavior. Product components compose them into Agent Board-specific behavior.

Before adding a low-level component:

1. inspect existing Agent Board components
2. inspect the current Nuxt UI component catalog
3. use `https://ui.nuxt.com/mcp` for unfamiliar component props/slots/events/examples when available
4. consult the official component page
5. compose the product UI from Nuxt UI components

Do not hand-roll generic buttons, dialogs/modals, dropdowns, tabs, selects, tooltips, inputs, textareas, badges, cards, tables, form fields, sidebars, empty states, skeletons, alerts, progress indicators or similar controls when Nuxt UI provides them.

A custom low-level primitive requires the justification defined in `frontend-implementation.md`.

## Nuxt boundary

Nuxt owns web rendering, routing and browser-facing application delivery.

- durable product state remains in PostgreSQL through the Go backend
- product mutations go through intentional Go APIs
- scheduling, execution, Workspace orchestration and authorization remain Go-owned
- Nitro/server routes do not create a second domain/control plane
- frontend state reconciles against durable API/SSE state

## Theme ownership

Use centralized Nuxt UI theming rather than repeated local overrides.

### `app/app.config.ts`

Use application configuration for intentional Nuxt UI defaults and shared component/theme configuration where supported.

Shared configuration is where Agent Board's square default, semantic colors and repeated component variants should live whenever Nuxt UI supports the relevant theme/default hook.

### shared CSS

Shared application CSS owns system-wide tokens/rules that are not more appropriately represented through Nuxt UI configuration, including typography roles, application-specific layout constants, surface tokens and any radius tokens not expressible cleanly through component configuration.

### product components

Product components own layout/composition and genuine local differences. They do not redefine the design system independently on every page.

Prefer semantic Nuxt UI colors and shared tokens over raw palette colors scattered through templates.

## Dark and light mode

Dark mode is the default. Light mode remains complete and usable.

Use Nuxt UI/Nuxt Color Mode facilities rather than implementing a parallel theme system. Components should consume semantic colors so the same composition works in both modes.

The light theme should preserve the same square, dense, technical visual language rather than becoming a separate rounded design.

All meaningful frontend changes should be checked in both modes.

## Typography

Keep product typography systematic. Prefer semantic markup and reusable roles over arbitrary font-size/weight/line-height combinations copied across templates.

Use compact uppercase labels sparingly for section/group metadata where they improve hierarchy. Main headings, body copy and form labels prioritize readability.

Monospace treatment is appropriate for IDs, code, commands, logs, hashes, paths and raw execution data.

## Spacing and density

Agent Board is intentionally compact, but not cramped.

Use Nuxt UI sizing/variants and Tailwind layout utilities deliberately. Repeated visual treatment belongs in shared component defaults, variants or product components rather than duplicated page-local class strings.

Controls remain comfortably usable by pointer and keyboard.

## Component customization order

Use this order:

1. Existing Agent Board composition.
2. Existing Nuxt UI component.
3. Documented Nuxt UI composition.
4. Nuxt UI component props/variants/slots.
5. Shared Nuxt UI theme/component configuration, including square/radius and semantic color defaults.
6. Agent Board product component wrapping/composing Nuxt UI.
7. Local layout classes for genuine one-off layout needs.
8. Custom low-level primitive only with explicit justification.

## Accessibility

Preserve and build on Nuxt UI/Reka UI accessibility behavior.

- visible focus states
- complete keyboard operation
- correct labels/descriptions
- status must not rely on color alone
- overlay focus behavior remains correct
- loading and disabled states are explicit
- dense tables/logs remain readable and navigable
- muted text and subtle borders still meet appropriate contrast/readability requirements

Custom interactive primitives require explicit accessibility tests.

## Agent Board-specific UI expectations

Execution state must be understandable without color alone.

Structured Questions should be quick to answer, show options/recommendations clearly and permit an alternate answer where the product contract allows it.

Run/Event/log views handle:

- initial loading
- long output
- reconnecting SSE
- replayed history
- errors
- empty states
- partial evidence
- cancellation/failure states

The browser is a view onto durable server-owned state, not the source of truth for a Run.

## Testing

Theme/frontend work follows `docs/testing.md`.

For frontend changes:

- test user-visible Vue behavior rather than brittle class snapshots
- test keyboard/focus behavior for meaningful interactions
- cover loading/empty/error/reconnect states
- cover dark/light behavior when theme logic changes
- verify Nuxt UI components are reused rather than duplicated with custom primitives
- verify shared theme configuration keeps cards/panels square by default instead of relying on page-local overrides

The desired outcome is centralized styling and reusable compositions: Vue components describe product structure and behavior; Nuxt UI plus shared theme configuration provide the common visual/accessibility foundation.
