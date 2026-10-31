#!/usr/bin/env bash
set -euo pipefail
ENV_FILE="${ENV_FILE:-/etc/electruvpn/electruvpn.env}"
fail=0
check(){ if "$@"; then printf 'PASS %s\n' "${*:2}"; else printf 'FAIL %s\n' "${*:2}"; fail=1; fi; }
if [[ -f "$ENV_FILE" ]]; then . "$ENV_FILE"; else echo "FAIL env file missing: $ENV_FILE"; exit 1; fi
[[ "${ELECTRU_SECURITY_MODE:-}" == "strict" ]] && echo 'PASS strict security mode' || { echo 'FAIL strict security mode'; fail=1; }
host_ok=0
for h in "${ELECTRU_PUBLIC_HOST:-}" "${ELECTRU_PUBLIC_HOST_V4:-}" "${ELECTRU_PUBLIC_HOST_V6:-}" "${ELECTRU_PUBLIC_HOST_DOMAIN:-}"; do
  if [[ -n "$h" && "$h" != *://* && "$h" != */* ]]; then host_ok=1; break; fi
done
[[ $host_ok -eq 1 ]] && echo 'PASS at least one public VPN host configured' || { echo 'FAIL public VPN host'; fail=1; }
sub_ok=0
for u in "${ELECTRU_SUBSCRIPTION_BASE_URL:-}" "${ELECTRU_SUBSCRIPTION_BASE_URL_V4:-}" "${ELECTRU_SUBSCRIPTION_BASE_URL_V6:-}" "${ELECTRU_SUBSCRIPTION_BASE_URL_DOMAIN:-}"; do
  [[ "$u" == https://* ]] && { sub_ok=1; break; }
done
[[ $sub_ok -eq 1 ]] && echo 'PASS at least one HTTPS subscription base configured' || { echo 'FAIL subscription base HTTPS'; fail=1; }
[[ "${ELECTRU_PANEL_VPN_ACCESS:-1}" == 1 ]] && echo 'PASS panel VPN access enabled' || { echo 'FAIL panel VPN access disabled'; fail=1; }
if [[ "${ELECTRU_PANEL_BIND_ADDR:-}" == 0.0.0.0 || "${ELECTRU_PANEL_BIND_ADDR:-}" == :: ]]; then echo 'PASS panel listener permits VPN/proxy access'; else echo 'PASS panel listener restricted (VPN access depends on proxy/reverse-proxy)'; fi
if command -v fail2ban-client >/dev/null 2>&1; then
  if systemctl is-active --quiet fail2ban 2>/dev/null; then
    echo 'PASS fail2ban service active'
  else
    echo 'FAIL fail2ban service inactive'; fail=1
  fi
  if [[ -f /etc/fail2ban/filter.d/electruvpn-auth.conf ]] && grep -q '\"msg\":\"authentication failure\"' /etc/fail2ban/filter.d/electruvpn-auth.conf; then
    echo 'PASS fail2ban matches structured authentication logs'
  else
    echo 'FAIL fail2ban filter does not match structured authentication logs'; fail=1
  fi
  if [[ -f /etc/fail2ban/jail.d/electruvpn-auth.local ]]; then
    echo 'PASS electruvpn fail2ban jail installed'
  else
    echo 'FAIL electruvpn fail2ban jail missing'; fail=1
  fi
fi
if command -v ufw >/dev/null 2>&1 && ufw status | grep -q 'Status: active'; then
  ufw status | grep -Eq '53/(tcp|udp).*DENY|53.*DENY' && echo 'PASS public DNS blocked' || { echo 'FAIL public DNS may be exposed'; fail=1; }
  ufw status | grep -Eq '853.*DENY' && echo 'PASS public DoT blocked' || { echo 'FAIL public DoT may be exposed'; fail=1; }
  PANEL_PORT="${ELECTRU_PANEL_PORT:-2087}"
  ufw status | grep -Eq "${PANEL_PORT}/tcp.*ALLOW|${PANEL_PORT}.*ALLOW" && echo "PASS panel access is allowed on ${PANEL_PORT}/tcp" || { echo "FAIL panel access rule missing for ${PANEL_PORT}/tcp"; fail=1; }
fi
if grep -RInq 'localhost' /etc/electruvpn/xray-config.json 2>/dev/null; then echo 'WARN xray config contains localhost DNS; review manually'; fi
if grep -RInq '"queryStrategy"' /etc/electruvpn/xray-config.json 2>/dev/null; then echo 'PASS xray DNS policy present'; else echo 'FAIL xray DNS policy missing'; fail=1; fi
if [[ $fail -ne 0 ]]; then exit 1; fi
echo 'SECURITY SELFTEST: PASS'
