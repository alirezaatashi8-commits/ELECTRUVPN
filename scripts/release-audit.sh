#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fail=0
pass(){ echo "PASS $1"; }
failf(){ echo "FAIL $1"; fail=1; }
[[ -f "$root/go.mod" ]] && pass 'go.mod present' || failf 'go.mod missing'
[[ "$(grep -E '^go ' "$root/go.mod" | awk '{print $2}')" == "1.27.1" ]] && pass 'Go version aligned to 1.27.1' || failf 'Go version mismatch'
grep -Fq 'go mod tidy' "$root/install.sh" && grep -Fq 'go build -trimpath -mod=readonly' "$root/install.sh" && pass 'fresh-build module checksum hardening' || failf 'fresh-build module checksum hardening missing'
grep -RInq 'func requestBodyLimit' "$root/internal/api/security.go" && pass 'request body limiter implemented' || failf 'request body limiter missing'
! grep -RInq '2FA\|TOTP\|totp' "$root/internal" "$root/web" && pass '2FA runtime code absent' || failf '2FA runtime code found'
node --check "$root/web/assets/js/app.js" >/dev/null && pass 'frontend syntax' || failf 'frontend syntax'
bash -n "$root/install.sh" && bash -n "$root/scripts"/*.sh && pass 'shell syntax' || failf 'shell syntax'
python3 "$root/scripts/web-routing-selftest.py" >/dev/null && pass 'routing matrix' || failf 'routing matrix'
! grep -RInq 'ReadWritePaths=.* /opt/electruvpn' "$root/deploy/electruvpn.service" && pass 'panel cannot write install directory' || failf 'panel install directory is writable'
[[ $fail -eq 0 ]]
