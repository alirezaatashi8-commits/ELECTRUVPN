# ElectruVPN Performance & Code Quality Hardening

This pass intentionally keeps the BETA1 feature set and API surface intact.

## Changes
- Background workers now share a cancellation context and stop cleanly during server shutdown.
- Node health alerting changed from N+1 database lookups to two bounded queries per pass.
- Node health checks select only required columns.
- Scheduler `node_health` checks for the first stale row instead of loading the entire agent table.
- Usage snapshot automation selects only required client columns and writes snapshots in batches of 500.
- Existing startup domain/certificate maintenance behavior is preserved.
- Release manifest hashes were refreshed for the modified source files.

## Compatibility
No route, model, handler, Xray/Sing-box feature, installer capability, or frontend asset was intentionally removed.

## Verification
- Go formatting: PASS
- Web routing matrix: PASS
- Privacy regression: PASS
- Platform self-test: PASS
- Release audit: PASS
- Full Go tests: blocked in this environment because the project requires Go 1.27 and network access to download the toolchain is unavailable.
