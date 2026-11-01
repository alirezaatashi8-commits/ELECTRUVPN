# ELECTRUVPN BETA — corrective patch notes

## Changes in this build

- Fixed the JavaScript syntax error in `web/assets/js/app.js` and restored a single functional `loadSettingsCenter()` implementation.
- Checked every JavaScript file under `web/` with `node --check` successfully.
- Changed the device-management handlers to check database errors instead of silently ignoring subscription-token invalidation failures.
- Made “revoke all devices” rotate client credentials and invalidate subscription tokens in a single database transaction, and report an Xray regeneration/restart failure instead of returning success silently.
- Made the credential reset operation update client credentials and device revocations transactionally, and check the result of Xray regeneration/restart.
- Ensured a new device requiring manual approval can be recorded as pending before subscription policy rejects its configuration request. Pending devices remain inactive and cannot pass the access policy until approved.

## Verification limits

- JavaScript syntax validation passed for all `.js` files under `web/`.
- Go tests and a full build were not run successfully in this environment: the project requires Go 1.27, while the available local toolchain is Go 1.23.2 and network access to download the required toolchain was unavailable.
- Run `go test ./...` and the project's install/integration tests with Go 1.27 in a clean staging environment before production deployment.
