# PART 20 — Skeleton Loading for major tabs

Files: `web/assets/css/style.css`, `web/assets/js/app.js`, `web/index.html`

## What
When switching between major tabs, a professional shimmer skeleton is shown for a short time (minimum ~280ms) while data loads. This prevents empty flashes and makes the panel feel much more polished.

## Covered tabs
- Dashboard
- Inbounds
- Subscription
- Online users
- Finance
- Settings

## Technical
- New `tabLoading` state map + `withTabSkeleton()` / `setTabLoading()` helpers.
- `loadTab()` now wraps the main data loaders of the above tabs.
- Skeleton respects `prefers-reduced-motion`.
- No features removed. Existing loading flags and API calls unchanged.
- Asset cache bust: `?v=beta47`.

## Next (requested)
- Part 21: Virtual scroll for long inbound / client lists
- Part 22: Polish of a specific page (subscription or settings)
