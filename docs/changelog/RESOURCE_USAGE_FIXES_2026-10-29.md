# Resource usage fixes — 2026-10-29

## Browser/API polling

- Removed the automatic browser-triggered `/dashboard/sync-traffic` request every 15 seconds. Traffic/quota synchronization already runs server-side every 45 seconds, so every open panel previously duplicated Xray stats polling and database writes.
- Reduced periodic dashboard refresh from 10 seconds to 30 seconds.
- Refresh the six-endpoint dashboard bundle only while the Dashboard tab is visible; refresh Enterprise data only in the Pro/Operations tabs.
- Removed the extra online-user fetch from the dashboard bundle; Online users and client-live views retain their dedicated polling when actually displayed.
- The overview endpoint cache window is now 10 seconds, and concurrent cache misses are coalesced so multiple browsers do not sample system metrics and aggregate DB queries simultaneously.

## Validation and limitations

- `node --check web/assets/js/app.js`: passed.
- `bash scripts/validate.sh`: passed static validation, platform matrix, and route coverage.
- Shell syntax checks: passed.
- Full Go compile/tests and runtime profiling require Go 1.27.1 and a runnable test environment; they were not available in this environment. This change reduces avoidable polling but is not a substitute for measuring CPU/RAM on the affected VPS with `top`, `pidstat`, and `systemd-cgtop`.

## Follow-up optimization pass

- Dashboard overview refresh now uses a frontend single-flight guard, so startup, visibility changes, and timer ticks cannot issue overlapping overview requests.
- The six-request dashboard bundle is no longer started unconditionally when the user opens a different section directly; it loads when the Dashboard tab is selected.
- Inbound Live now selects only the inbound summary fields and each client's `id`, `inbound_id`, and `email`, instead of preloading every client column on each live poll.
- Kept the existing live refresh cadence and server-side traffic accounting semantics unchanged to avoid sacrificing live responsiveness or quota/expiry correctness.

Validation note: static, platform, and route tests pass. Full Go compile/tests are not available in this environment because the project requires Go 1.27.1 while only Go 1.23.2 is installed and the toolchain download endpoint is unreachable.

## Additional conservative audit pass

- Added a 4-second deadline to the `xray api statsquery` subprocess. A stuck CLI/API call can no longer retain the global stats lock indefinitely and block all live/accounting callers.
- Avoided an unnecessary second deep-copy of freshly parsed stats; the cache keeps its own independent copy, while the caller receives the newly allocated parsed map.
- Removed duplicate dashboard overview loading on startup (routing already loads the selected tab).
- Deferred inbound preloading to the Dashboard only (for its protocol summary); direct visits to unrelated tabs no longer trigger inbound list loading.
- Deferred Pro/Enterprise data loading to their own tabs instead of loading those datasets on every page visit.

Validation: JavaScript syntax, static validation, platform matrix, web-routing matrix, and OpenAPI route coverage pass. Full Go compilation/unit tests remain unverified because the required Go 1.27.1 toolchain cannot be downloaded in this environment. Runtime CPU/RAM profiling must be performed on the VPS; these changes reduce avoidable work but do not prove that Xray itself is the source of any specific CPU spike.
