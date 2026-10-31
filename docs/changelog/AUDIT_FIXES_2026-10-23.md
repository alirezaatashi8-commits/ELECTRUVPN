# Audit fixes (2026-10-23)

## Critical
- **Finance tab routing**: `finance` was missing from `routeMap()`, so `goTab('finance')` always fell back to dashboard.
- **Subscription client list**: clients without a loaded inbound were dropped; now always included with a fallback remark.
- **Bot deep links**: `/start shop|renew|wallet|status|configs|trial` now open the matching bot flow (used by subscription landing renew button).

## Hardening
- Subscription dashboard loader already uses per-request safe API wrappers.
- `renderSubscriptionChart` clears safely when empty / Chart missing.
- `loadFinance` tolerates partial API failures without breaking the tab.

## Note
A full compile-time audit of every Go package requires the project Go toolchain on the host; static review focused on panel↔API↔bot connections.

## Static review pass (follow-up)
- `go.mod` directive was `go 1.22.2` while deps and installer need Go 1.27.1 → set to `go 1.27.1`.
- Removed unused `github.com/google/uuid` import in `tma_handler.go` (compile error).
- `app.js`: 15 `this.api()` calls were missing the `/pro` prefix (dashboard performance/advisor/platform summary+security, BBR, public-host, settings center, backups) → all 404'd; fixed.
- Regenerated `RELEASE_SOURCE_MANIFEST.txt` (was stale: install.sh would abort on hash mismatch / missing .pyc entries).
