# Part 6 — Live sections UI fixes (frontend only, no backend changes)
- web/index.html: Global search modal no longer throws TypeError on every page load (`enterprise.search.obj` is null until a search runs).
- web/assets/js/app.js: loadDashboardPro() only accepts arrays for performance/advisor rows (no `rows.map is not a function`). "Auto" label on the Online tab said 3s but the timer is 5s -> label now says 5s (FA + EN).
- web/index.html: speed values (Online tab, inbound Live modal, client Live modal) forced `dir="ltr"` so "↓ 2.38 MB/s ↑ 488 KB/s" is no longer scrambled in the Persian (RTL) UI.
- web/index.html: Xray log viewer forced `dir="ltr"` (timestamps/[Info] order no longer reversed, long lines no longer cut at the start); Live / Follow / lines labels translated in FA.
- web/index.html: top bar wraps on phones (was 73px wider than the screen in RTL).
- assets ?v=beta42. Live clock and live dashboard left untouched (verified OK).
