# ELECTRUVPN BTA1 — final review pass

## Fixed
1. **Gift-order approve race still open:** the atomic claim accepted `processing` rows, so two concurrent approvals
   could both pass. Claim now accepts only `pending_*`, or a `processing` row older than 5 minutes (crash recovery).
2. **Multi-Hop save wiped hop flow:** the UI never sends `midFlow` / `mid2Flow` / `exitFlow`, so every save reset a stored
   flow (e.g. `xtls-rprx-vision`) to empty and broke the chain. Empty flow values no longer overwrite stored ones.
3. **Multi-Hop save hid Xray reload failures:** the reload error was discarded; the response now carries a `warning`.
4. All traffic-scaling leftovers removed from code, API, UI, docs and changelogs. Usage is the raw measured byte count only.

## Verified (static, no Go toolchain in this environment)
- Go: brace balance, imports, cross-package symbols, duplicate declarations, SQL placeholder/argument pairs
- Alpine UI: every `tunnelChain.*` binding and every method called from HTML exists in `app.js`
- `node --check` on app.js and all inline scripts, `bash -n` on all shell scripts
- `release-audit.sh`, `web-routing-selftest.py`, `openapi-route-selftest.py` pass
- `RELEASE_SOURCE_MANIFEST.txt` regenerated and verified with the installer's own check
- Run `go build ./... && go vet ./... && go test ./...` on the target server before release.
