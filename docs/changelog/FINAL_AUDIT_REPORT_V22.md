# ELECTRUVPN BETA v22 — Quick WebSocket / HTTPUpgrade Audit

## Added
- Quick WebSocket wizard: quota, expiry, custom port or random free port, IPv4/IPv6/Dual, address type, optional names.
- Quick HTTPUpgrade wizard with the same controls.
- Automatic managed TLS certificate selection for WebSocket/HTTPUpgrade when usable certificate material exists.
- Strict-security fail-closed behavior: no insecure Quick WS/HTTPUpgrade is created without usable TLS material.
- API routes and OpenAPI documentation.
- UI actions and result/copy-link views.
- Existing Quick gRPC behavior preserved.

## Security decisions
- Xray REALITY is not used for WebSocket or HTTPUpgrade because current Xray transport compatibility does not support that combination.
- WebSocket and HTTPUpgrade use TLS when a valid managed certificate is available.
- In strict security mode, missing/invalid certificate material returns HTTP 412 instead of creating `security=none`.
- HTTPUpgrade remains available for compatibility; current Xray documentation recommends XHTTP for new deployments.
- 2FA/TOTP/Google Authenticator/Google OAuth were not added.

## Validation
- `gofmt`: PASS
- `node --check web/assets/js/app.js`: PASS
- `python3 scripts/web-routing-selftest.py`: PASS
- `bash -n install.sh`: PASS
- `bash -n scripts/*.sh`: PASS
- OpenAPI YAML parse: PASS
- Quick transport route/source scan: PASS
- 2FA/Google-auth runtime source scan: CLEAN
- ZIP integrity: verified after packaging

## Environment limitation
Full Go tests were attempted. The repository requires Go 1.27 and the current execution environment has no external DNS/network access, so the Go toolchain download from `proxy.golang.org` was blocked before compilation. This is reported as an environment limitation, not a test pass.
