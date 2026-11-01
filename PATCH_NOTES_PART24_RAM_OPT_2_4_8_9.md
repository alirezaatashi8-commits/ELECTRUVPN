# PART 24 — RAM / smoothness: chart destroy, lazy data, virt clients, scroll throttle

Files: `web/assets/js/app.js`, `web/index.html`

## #2 Full Chart destroy
- New `destroyAllCharts()` frees net / finance / subscription Chart.js instances.
- Called when leaving chart tabs (dashboard, finance, subscription).
- Called on `visibilitychange` when the browser tab is hidden (frees GPU/RAM while admin is away).
- Chart is recreated when returning to dashboard via `ensureNetChart()`.

## #4 Lazy data for heavy tabs
- `tabLoaded` map for: pro, fabric, ops, resellers, platform, control, telegram.
- First open runs the normal skeleton + fetch; later opens reuse in-memory data (no duplicate API storm).
- Explicit refresh buttons that call loaders directly still work (bypass lazy gate).
- Live tabs (dashboard, inbounds, online, logs, subscription, settings, finance) still load every time as before.

## #8 Deeper virtual scroll
- Client list **inside an expanded inbound** (desktop) now uses the same windowed renderer as subscription clients.
- max-height ~52vh, top/bottom spacers, only visible rows in the DOM when list > 35.

## #9 Scroll throttle
- `onVirtClientScroll` / `onVirtInboundScroll` use `requestAnimationFrame` — at most one window recompute per frame.

## Safety
- No API or feature removed.
- Pagination, tags, groups, edit, bulk actions unchanged.
- Cache: `?v=beta51`.
