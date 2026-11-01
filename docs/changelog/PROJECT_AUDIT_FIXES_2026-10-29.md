# Project audit fixes — 2026-10-29

- Fixed expiry comparison for clients and inbounds: an expiry timestamp is considered expired at the timestamp boundary (`now >= expiry`), rather than remaining active for the exact matching millisecond. A zero/non-positive inbound expiry remains unlimited; client expiry zero remains unlimited.
- Added a dedicated `Client days` field to the create-inbound wizard for clients created in the same operation. It is independent of `Inbound days`; `0` means no client expiry, and values from 1 through 3650 are validated server-side.
- Added model tests covering unlimited expiry and expired timestamps.

## Verification

- ZIP archive integrity and release-manifest hashes verified before editing.
- JavaScript syntax check and shell syntax checks pass after editing.
- Full Go tests could not be run in this environment because the project requests Go 1.27.1 and outbound access to `proxy.golang.org` is unavailable.

## Second audit pass

- Added the 14 registered API operations that were missing from `web/docs/openapi.yaml`; the OpenAPI route-coverage self-test now passes and is included in `scripts/validate.sh`.
- Fixed database maintenance to use SQL statements valid for SQLite and PostgreSQL (`VACUUM` / `ANALYZE`, not invalid `PRAGMA` variants). SQLite integrity-check failures are now reported as failures, and PostgreSQL receives an explicit unsupported response for SQLite-only `PRAGMA integrity_check` rather than a misleading generic error.
- Centralized quota comparisons so usage counter addition and GiB-to-byte conversion cannot overflow `int64` and accidentally bypass or prematurely trip limits.
- Aligned expiry handling at the exact expiry millisecond, including subscription grace-period checks and expired-client reporting/disable queries.
- Added regression tests for quota arithmetic overflow and precise byte limits.

## Follow-up audit: upload endpoints (2026-10-29)

- Fixed the parent authenticated API body limit overriding the import/restore handlers: 3x-ui imports now receive the intended 128 MiB request budget, and backup ZIP uploads receive the intended 1.5 GiB budget. All other API endpoints retain the default 4 MiB cap.
- Updated the generated Nginx reverse-proxy configuration with exact-match locations for those two upload routes. The general 2 MiB proxy cap remains in place for all other routes; the two upload locations use explicit caps and longer timeouts.
- Added explicit `413 Request Entity Too Large` responses when multipart parsing hits an upload limit, rather than returning the misleading generic “file required” message.
- Added middleware regression coverage for normal API requests and upload paths, including deployments under a configured panel base path.
- Validation run: JavaScript syntax, shell syntax, platform matrix, OpenAPI route coverage (289 operations), web base-path routing, upload proxy assertions, and Go formatting passed. Full Go tests remain blocked in this environment because the required Go 1.27.1 toolchain cannot be downloaded.


## Resource-usage follow-up

- Reduced the admin dashboard's periodic refresh frequency from 30 seconds to 45 seconds and extended the overview cache to 15 seconds.
- Prevented dashboard refresh requests from bypassing the single-flight guard when a manual/route refresh overlaps a scheduled refresh.
- Increased online-user and live traffic modal polling from 3 seconds to 5 seconds; online-user and inbound-live polling now pauses network requests while the browser tab is hidden.
- Separated domain health checks (5-minute interval) from certificate renewal checks (6-hour interval). Certificate renewal still runs at startup, while avoiding repeated certbot subprocess attempts every two minutes.
- Added exponential backoff to automatic engine recovery when a restart/configuration validation fails (30 seconds up to 5 minutes), preventing a persistent retry storm on missing binaries or invalid configuration. Xray and configured sing-box recovery conditions are retained.
- Validation: JavaScript syntax and repository static validation pass. Full Go tests/build remain unavailable in this environment because Go 1.27.1 cannot be downloaded without network access.

## CPU follow-up: recovery and dashboard single-flight

- Automatic recovery now verifies that Xray and any configured Sing-box engine are still running after a successful apply before resetting retry backoff. If an engine exits during startup despite a nil apply error, retries back off rather than repeatedly regenerating configuration on the 30-second schedule.
- Explicit/manual dashboard refreshes can no longer bypass the single-flight guard and start overlapping six-request batches.
- Validation after these changes: repository static validation, platform matrix, OpenAPI route coverage, web routing self-test, JavaScript syntax, shell syntax, and `gofmt` pass. Full Go build/tests are still blocked because Go 1.27.1 cannot be downloaded in this network-restricted environment.
