# Part 2 — Live dashboard (speed, online users, chart)
- internal/api/handlers/dashboard_handler.go: Overview now returns netUpSpeed / netDownSpeed (bytes/s, from host counter deltas) and onlineCount; cache TTL 30s -> 4s.
- internal/xray/live.go: online state no longer flickers/resets when two handlers sample the same cached counters (min 1s sample gap, 15s online grace). Test added.
- web/assets/js/app.js: dashboard overview polled every 5s while visible; "Live Traffic" chart now plots speed (B/s) instead of cumulative bytes, is rebound to the canvas each time the dashboard opens (x-if), and lives outside Alpine's reactive state; online gauge reads overview.onlineCount (was always 0 outside the Online tab).
- web/index.html: online gauges use onlineNow(); assets ?v=beta38.
