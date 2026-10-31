#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"
fail(){ echo "FAIL $1" >&2; exit 1; }
pass(){ echo "PASS $1"; }
[[ "$(grep -c 'ELECTRU PANNE V35 Command Center' web/index.html)" == "1" ]] || fail "duplicate command center UI"
! grep -q 'latestVerified": false' internal/api/handlers/v35_handler.go || fail "hardcoded recovery verification"
grep -q 'verifyBackupArchive' internal/api/handlers/v35_handler.go || fail "recovery verifier missing"
grep -q 'Where("enable = ?", true)' internal/api/handlers/v35_handler.go || fail "preflight includes disabled inbounds"
grep -q 'ProtocolHysteria2' internal/api/handlers/v35_handler.go || fail "engine-aware preflight missing"
! grep -q 'احراز هویت دومرحله‌ای' web/index.html || fail "stale two-factor wording remains"
node --check web/assets/js/app.js
bash -n install.sh
for f in scripts/*.sh; do bash -n "$f"; done
python3 -m py_compile scripts/web-routing-selftest.py
pass "V35 deep-audit invariants"
