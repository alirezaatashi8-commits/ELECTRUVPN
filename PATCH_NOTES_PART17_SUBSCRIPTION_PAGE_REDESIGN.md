# PART 17 — Subscription page redesign (desktop / Windows / large screens)

Files: `internal/api/handlers/subscription_handler.go`, `internal/api/handlers/sub_speedometer.go`

- New responsive two-column layout: sticky profile sidebar + main column (max width 1480px) — no more narrow 560px strip on Windows/desktop.
- Light / dark theme (follows system, manual toggle, remembered in localStorage).
- Progress ring for traffic, remaining volume, live days/hours countdown to expiry, status levels (ok / warn / bad).
- Stat tiles (download, upload, usage/limit, expiry), theme-aware live speedometer (same `/live` endpoint).
- Config list with protocol badges (VLESS/VMESS/TROJAN/SS/HY2/TUIC), parsed name + host:port, tabs (all/domain/IP), copy per config / copy section, QR modal, collapsible full link.
- Sidebar QR for the subscription link (desktop → scan with phone), 3-step setup guide, app cards for Android / iOS / Windows (v2rayN) / Windows-Mac-Linux (Hiddify) with auto-highlight for the visitor's OS.
- Lighter page: per-row QR thumbnails removed (QR only in modal), inline SVG icons instead of emoji, no external requests (CSP-safe).
- Security: link data embedded via `encoding/json` (escapes `<`, `>`, `&`), all text HTML-escaped.
- Accessibility: focus rings, aria labels, Esc closes modal, `prefers-reduced-motion`, print styles.
- File names and ZIP name unchanged. `RELEASE_SOURCE_MANIFEST.txt` regenerated.

## Revision 2
- Dark theme only (light theme and theme toggle removed).
- Configs are at the top again, each with its QR thumbnail (click to enlarge) + copy button; subscription link card has its QR beside it.
- Traffic ring + live speedometer moved to a side column on wide screens (below configs on mobile).
- App download buttons are now neutral dark buttons (Android / iOS only); no colored stickers or laptop cards.

## Revision 3 — «طرح‌ها» (designs) switcher
- New "طرح‌ها" button in the top bar: **مدرن** (new dashboard, default) and **کلاسیک (اولیه)** (the original look: traffic → speed → configs → sub link → apps).
- Classic is now responsive: on Windows / wide screens it uses a 2-column grid up to 1320px instead of a 560px strip.
- The choice is remembered per subscription in the visitor's browser (localStorage); no server change needed.

## Revision 4 — Daily usage chart (both designs)
- New "مصرف روزانه" card on the subscription page (modern and classic): stacked download/upload bars, ranges 7 / 14 / 30 days, summary tiles (period total, daily average, peak day, today), hover/tap a bar to see that day's numbers. Dates are shown in the Jalali calendar.
- Data comes from the existing `usage_snapshots` table (written by the scheduler / traffic sync) via the new `subDailyUsage()` helper in `subscription_handler.go`; counter resets are handled. No schema or API change.
- The chart is drawn client-side as inline SVG (no external libraries, CSP-safe). If no usage has been recorded yet it shows an empty-state message.
