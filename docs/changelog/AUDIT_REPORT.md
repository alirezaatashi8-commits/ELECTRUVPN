# ELECTRUVPN BETA — Full Engineering Audit (v6)

## Scope

Audited the full repository with emphasis on:

- Go source structure and formatting
- Xray/sing-box config generation
- runtime process lifecycle and restart safety
- database/rollback behavior
- API authentication/authorization and request limits
- subscription/API secret exposure
- webhook SSRF defenses
- client bulk operations and WireGuard address allocation
- frontend syntax/routing/static checks
- installer/systemd/shell validation

## Fixed in v6

1. **Atomic runtime config writes**
   - Xray and sing-box configs are written through temp files + fsync + rename.
   - Prevents partially written JSON from becoming the active runtime config.

2. **Graceful engine shutdown**
   - Xray and sing-box receive SIGTERM first.
   - A bounded wait is used; SIGKILL is the fallback after timeout.
   - Restart waits for the old process to actually exit.

3. **Runtime transaction/rollback**
   - Generated Xray and sing-box configs are validated before replacement.
   - Previous runtime files are preserved during apply.
   - Xray failure restores the previous Xray runtime.
   - sing-box failure restores both engines/config files where applicable.

4. **Immediate process-crash detection**
   - Engine start now detects processes that exit during the startup window instead of blindly reporting success.

5. **API request-size limits**
   - Authenticated JSON API bodies are capped at 4 MiB.
   - Agent heartbeat/result bodies are capped at 2 MiB.

6. **Authentication guard memory bound**
   - Login failure tracking now cleans expired entries when the keyset grows, preventing unbounded growth from unique usernames.

7. **API v1 secret exposure reduction**
   - `GET /api/v1/inbounds` no longer preloads and serializes client credentials.
   - Client credentials remain available only through the explicit clients API scope/route.

8. **Webhook SSRF hardening**
   - Only HTTP/HTTPS webhook URLs without embedded userinfo are accepted.
   - Loopback, private, link-local, unspecified, non-global and CGNAT address space is rejected during DNS resolution/dialing.
   - Redirects remain disabled.

9. **Bulk client correctness**
   - Bulk creation is transactional.
   - Partial success is no longer silently returned.
   - WireGuard key/address generation failures abort the whole operation.

10. **WireGuard client address allocation**
    - Allocation scans existing addresses and reuses free slots instead of relying on row count, avoiding collisions after deletions.

11. **Client input hardening**
    - Client email is trimmed and bounded.
    - Existing non-negative quota/expiry/IP-limit checks remain enforced.

12. **Inbound initial-client creation correctness**
    - Requested initial client count must be fully created.
    - WireGuard key/address failures roll back the newly created inbound instead of silently producing fewer clients.

13. **Automated tests added**
    - client quota/expiry behavior
    - Xray config generation filtering
    - Xray/sing-box protocol separation
    - atomic runtime config writing
    - basic sing-box Hysteria2 generation

## Verification completed in this environment

- `node --check web/assets/js/app.js` — PASS
- `bash -n install.sh` — PASS
- `bash -n scripts/*.sh` — PASS
- `python3 scripts/web-routing-selftest.py` — PASS
- `bash scripts/validate.sh` — PASS
- `bash scripts/platform-selftest.sh` — PASS
- `go test ./internal/models ./internal/xray ./internal/singbox` — PASS
- ZIP integrity check — PASS

## Environment limitation

A full `go test ./...` / production build could not be completed inside this build environment because external Go module downloads are blocked by the environment's DNS/network policy. The repository's installer already runs `go mod tidy` and `go build ./cmd/server` on the target server, but this local audit does not claim a full dependency-backed build when the dependencies were unavailable.

## Remaining deployment-level verification

On the target Ubuntu server, after extracting v6, the final verification should include:

```bash
go mod tidy
go test ./...
go build -o electruvpn-server ./cmd/server
systemctl restart electruvpn
systemctl is-active electruvpn
journalctl -u electruvpn -n 100 --no-pager
```

Then exercise one test inbound, one client, a clone/rollback, a client rotation, and a service restart before production use.

## v7 Enterprise Hardening Addendum

- Added enterprise metrics, deterministic/deduplicated operational alerts, alert resolution, and global search APIs.
- Added graceful HTTP shutdown on SIGINT/SIGTERM with a bounded 15-second drain window.
- New API keys no longer default to wildcard `*`; callers must request at least one allowlisted scope. Existing wildcard keys remain backward compatible.

### Verification limitation

The current build environment has no outbound DNS/network access, so `go mod download` cannot populate `go.sum` here. `gofmt`, JavaScript syntax checks, shell syntax checks, routing self-test,. Full dependency-backed `go test ./...` and production binary compilation must be run on the Ubuntu build/install host where the installer can download modules.

## Enterprise Expansion Pass

This release adds a persistent automation/scheduler layer instead of UI-only controls:
- `AutomationRule`: event, condition JSON, action, payload, cooldown, last-run/error state.
- `ScheduledTask`: `@every <duration>`, `@hourly`, and `@daily` schedules with next/last-run state.
- Background scheduler tick with overlap protection.
- Manual scheduler execution endpoint.
- CRUD API and Operations UI for rules/tasks.
- Regression tests for schedule calculation.
- API v1 pagination and credential-safe response DTOs.
- GitHub CI workflow covering Go tests/vet, shell validation, routing self-test and JS syntax.

The project intentionally does not include 2FA/TOTP.

## Final Deep Audit Pass (v12)

Verified and fixed release-blocking issues found during the final source audit:

1. Added the missing `requestBodyLimit` middleware referenced by the router. The previous archive could not compile once module dependencies were available.
2. Aligned `go.mod` and CI/installer documentation on Go 1.23.
3. Bounded LoginGuard and request-rate-limiter memory under attacker-controlled unique keys.
4. Added strict validation for automation events/actions, scheduler kinds/schedules and JSON payloads; invalid legacy schedules are disabled instead of executing every scheduler tick.
5. Fixed IPv6 public-host share-link construction using `net.JoinHostPort` and validated public host input.
6. Agent enrollment now refuses to issue an HTTP agent endpoint; agent enrollment requires HTTPS.
7. Panel systemd service no longer grants itself write access to `/opt/electruvpn`, reducing post-compromise self-modification risk.
8. Backup restore now runs through a transient `systemd-run` unit so the restore process is not killed when the panel service stops; restore helper stops the panel before replacing the SQLite database/configs and attempts recovery on failure.
9. Static assets are no longer cached for a full day under fixed filenames; `no-cache` avoids stale JS/CSS after upgrades.
10. Added release-audit checks and regression tests for the new bounded-limiters and public-host validation.

### Test evidence in this environment

- `gofmt -l` across all Go files: PASS
- `node --check web/assets/js/app.js`: PASS
- `bash -n install.sh`: PASS
- `bash -n scripts/*.sh`: PASS
- `python3 scripts/web-routing-selftest.py`: PASS
- `bash scripts/release-audit.sh`: PASS
- `go test ./internal/models ./internal/xray ./internal/singbox`: PASS
- `go test ./internal/privacy ./internal/models ./internal/xray ./internal/singbox`: PASS

The full repository `go test ./...` remains environment-limited because this execution environment cannot download the external Go module graph and the release source does not currently contain a generated `go.sum`. The release CI now runs `go mod tidy` before the full test/vet suite on Go 1.23. This limitation is not represented as a false PASS.
