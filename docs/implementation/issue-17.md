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
