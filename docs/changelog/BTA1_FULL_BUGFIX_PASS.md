# ELECTRUVPN BTA1 — full bug-fix pass

## Critical / correctness
1. **Certificate revision lookup** (`internal/automation/manager.go`): `Where("certificate_id = ?")` had no argument → always failed/wrong match. Now passes `cert.ID`.
2. **Gift order double-approve race** (`shop_handler.go`): concurrent approves could issue two gift cards. Order is now claimed atomically (`pending_*` → `processing`) before `IssueGiftFromPlan`; failure rolls back to `pending_review`.
3. **Wallet auto-renew without Xray reload** (`telegram/autorenew.go` + `cmd/server/main.go`): successful renews updated DB but never regenerated the core. `RunAutoRenew` now accepts `onSuccess` and main regenerates Xray after any successful pass.
4. **Trial TrafficLimitBytes + AutoExtendGB loop** (prior pass, verified): extending GB also extends the byte cap.
5. **Multi-Hop Mid2**: UI/JS + `active_mid2_host` default/trim on save (prior + this pass).

## Hardening
6. **Bulk client actions**: max 500 IDs per request.
7. **Version string** on Telegram notification test: `ELECTRUVPN BTA1` (was BETA).

## Verification (this environment)
- `node --check web/assets/js/app.js`
- Inline JS syntax on TMA / portal / login
- `bash -n` all shell scripts
- `openapi-route-selftest.py` PASS
- `web-routing-selftest.py` ok
- `release-audit.sh` PASS (frontend/shell/routing)
- Go toolchain 1.27 not available here — run `go build ./...` and `go test ./...` on the target server
