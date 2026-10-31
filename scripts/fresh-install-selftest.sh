#!/usr/bin/env bash
set -euo pipefail
fail(){ echo "FAIL: $*" >&2; exit 1; }
[[ ${EUID:-$(id -u)} -eq 0 ]] || fail "run as root"
[[ -r /etc/os-release ]] || fail "missing /etc/os-release"
. /etc/os-release
case "${ID:-}" in ubuntu|debian) ;; *) fail "unsupported OS: ${PRETTY_NAME:-unknown}";; esac
command -v systemctl >/dev/null || fail "systemd missing"
command -v ss >/dev/null || fail "iproute2/ss missing"
command -v ufw >/dev/null || echo "WARN: ufw not installed (installer will install it)"
command -v curl >/dev/null || fail "curl missing"
command -v openssl >/dev/null || fail "openssl missing"
free_kb=$(df -Pk /opt 2>/dev/null | awk 'NR==2{print $4}')
[[ "$free_kb" =~ ^[0-9]+$ ]] && (( free_kb >= 1048576 )) || fail "need at least 1GiB free on /opt"
echo "Fresh-install preflight: PASS (${PRETTY_NAME:-unknown})"
