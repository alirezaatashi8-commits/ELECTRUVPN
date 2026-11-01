# PART 19 — UI smoothness & light memory optimizations

Files: `web/assets/css/style.css`, `web/assets/js/app.js`, `web/index.html`

## Goal
Make the panel feel smoother and a bit lighter on RAM / GPU without removing any features or changing behaviour of existing APIs, pages, or buttons.

## CSS
- Reduced `backdrop-filter` blur from 18px → 12px on `.glass` (still glassy, less GPU cost on many cards).
- Smoother easing: cubic-bezier(0.22, 1, 0.36, 1) on common transitions and `.fade-up`.
- Softened infinite float / drift animations (smaller movement) so they cost less continuous paint.
- Added thorough `prefers-reduced-motion` support (disables float, drift, pulse-glow, live border animation).
- `contain: layout style` on main shell, sidebar and glass cards to limit reflow/paint scope.
- `content-visibility: auto` + intrinsic size hint on inbound cards / client rows / finance cards (browsers that support it skip off-screen work).
- Hover on primary buttons uses transform + box-shadow instead of `filter: brightness` where practical.
- Slightly reduced grid-texture opacity/size for cheaper background.

## JS
- On tab leave: explicitly `destroy()` Chart.js instances for dashboard network chart, finance charts and subscription chart. They are re-created when the tab is opened again. Long admin sessions no longer keep multiple Chart objects alive.
- Live overview poll interval 5s → 6s (still responsive, fewer concurrent requests on small VPS).
- Network speed history capped at 24 points instead of 30 (less array churn + smaller chart data).

## Cache bust
- `style.css` and `app.js` query string bumped to `?v=beta46`.

## Not changed
- No feature, route, API, permission, or data model removed or altered.
- File / ZIP name unchanged.
- Backend, agents, install scripts, OpenAPI, TMA, portal untouched.
