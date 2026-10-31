# Part 8 — Sidebar cleanup + full frontend/API contract audit (frontend only, no backend changes)
- web/index.html: removed duplicated sidebar entries (Telegram and Settings appeared twice; a "Sign out" item sat in the middle of the Account section — sign out remains in the sidebar footer, the mobile header and Settings).
- web/index.html: Online / Resellers sidebar clicks no longer trigger a second manual load (goTab -> loadTab already loads them).
- web/assets/js/app.js: loadTab now has an explicit `resellers` case (the load used to depend on the sidebar click, so opening Resellers from the URL, the command palette or the mobile bar left it empty).
- assets ?v=beta44.

## Audit results (nothing else needed changing)
- All ~167 API calls in app.js/index.html resolve to a real router.go route with a matching HTTP method (0 mismatches).
- app.js passes `node --check`; every identifier used in Alpine expressions is defined.
- Headless-Chromium run of all 14 tabs on desktop (1366px) and mobile (375px) with an empty database: 0 JS errors, 0 horizontal overflow.
