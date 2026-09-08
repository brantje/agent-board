# Issue #17 staged implementation

All stages use `codex/17-nuxt-vertical-slice` and are committed/pushed separately.

## Stage 1 — #36

- Nuxt UI dashboard shell, canonical global/project navigation, responsive sidebar, page/action composition and shared async states.
- Centralized graphite/steel-blue theme with dark default and complete light tokens, square panels and compact controls.
- Same-origin `/api/**` delivery proxy to the Go API. Set `AGENT_BOARD_API_URL` at build/dev startup for a different upstream (default `http://127.0.0.1:3001`). The proxy adds no privileges and owns no domain state.
- Disposable resource cache reads on mount/scope change, clears old scope data, rejects stale responses, cancels on unmount, and exposes retry.
- Tests cover transport failures, safe errors, stale response isolation, navigation, async states and theme defaults. API tests were written before transport implementation; component lifecycle tests guided refinement.
- Verification: typecheck and production build pass; 16 tests; statements 100%, branches 96.42%, functions 100%, lines 100%.
- Browser check: shell renders at desktop size, canonical navigation and theme toggle are reachable; dark/light modes checked. Generic interactions use Nuxt UI; no custom low-level primitives.

## Stage 2 — #37

- Shared and Project-scoped configuration routes cover Providers, Model Profiles, Runtimes, Agents and local Projects. Forms use Nuxt UI validation/controls and intentional public fields.
- Shared resources are read-only inside a Project, disabled resources cannot be selected for new references, and empty Model Profile capacity submits `null` (unlimited).
- Provider credential input is write-only, requires the existing deployment secret-write capability, and is cleared after each save attempt. A regression test exposed the Go Provider update handler dropping omitted credentials; omission now preserves the stored reference and the OpenAPI contract documents it.
- No model discovery/preflight metrics are fabricated where the API does not expose them. Health remains explicitly separate from configured state.
- Verification: 32 frontend tests, typecheck/build pass. Coverage: statements 97.70%, branches 96.15%, functions 94.93%, lines 97.85%. Full Go tests against isolated PostgreSQL, vet/build pass; server total 85.0%.
- Browser inspected Provider modal labels, keyboard-addressable controls and error state. Local Lucide icons remove the external icon lookup dependency; modal fullscreen variant is overridden centrally to keep square surfaces.

## Board specification decision

The available checkout/history has no spec folder or Issue-card implementation, and #17/#38 refer to a card without defining its fields. After checking GitHub issues and comments, the user explicitly chose the current public API fields: title, status, assigned Agent and Issue identifier. Keep this hierarchy without adding speculative metadata.
