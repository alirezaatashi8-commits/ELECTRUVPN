# Part 4 — Live Xray log viewer
- web/assets/js/app.js: Logs tab refreshes every 3s while visible (Live toggle), follows the tail (Follow toggle, auto-off when the user scrolls up), no overlapping requests, no re-render when nothing changed.
- web/index.html: Live / Follow toggles and line counter; assets ?v=beta40.
- No backend changes in this part.
