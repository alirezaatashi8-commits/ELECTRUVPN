#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fail=0
assert(){ if eval "$1"; then echo "PASS $2"; else echo "FAIL $2"; fail=1; fi; }
assert "grep -q 'ELECTRU_PANEL_VPN_ACCESS' '$root/internal/config/config.go'" 'panel VPN access setting exists'
assert "grep -q 'ELECTRU_PANEL_VPN_ACCESS 1' '$root/scripts/enable-privacy-mode.sh'" 'privacy mode preserves panel VPN access'
assert "grep -q '0.0.0.0/0.*::/0' '$root/internal/xray/subscription.go'" 'WireGuard full tunnel defaults'
assert "grep -q '1.1.1.1.*8.8.8.8' '$root/internal/xray/subscription.go'" 'WireGuard secure DNS defaults'
assert "grep -q 'geoip:private' '$root/internal/xray/config.go'" 'private network blocking'
assert "grep -q 'https+local://1.1.1.1/dns-query' '$root/internal/xray/config.go'" 'encrypted Xray DNS default'
assert "grep -q 'type.*https' '$root/internal/singbox/config.go'" 'encrypted sing-box DNS'
exit "$fail"
