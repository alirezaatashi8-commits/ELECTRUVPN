# Part 3 — Online users / Live modals
- internal/xray/live.go: LiveSampleForSubjects (merges primary email + "email#profile-N" aliases), HasLiveSamples. Tests added.
- internal/api/handlers/client_profiles.go: clientsTrafficSubjects (bulk alias lookup, one query).
- client_handler.go (OnlineUsers, Live), inbound_handler.go (Live), dashboard_handler.go (onlineCount): clients that connect through extra config profiles are no longer shown offline / 0 speed. OnlineUsers warms up a cold tracker so the first open is not empty.
- web/assets/js/app.js: Online tab auto-refresh on by default and also loads when opened by URL; request guard against overlapping polls; inbound Live modal keeps retrying after a failed first request.
- web/index.html: inbound Live modal shows why live stats are unavailable (sing-box protocols); assets ?v=beta39.
