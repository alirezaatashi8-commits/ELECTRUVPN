#!/usr/bin/env bash
set -euo pipefail
[[ $EUID -eq 0 ]] || { echo 'Run as root'; exit 1; }
ENV_FILE="/etc/electruvpn/electruvpn.env"
[[ -f "$ENV_FILE" ]] || { echo "$ENV_FILE not found"; exit 1; }
# Load existing endpoint settings so family-specific configuration is preserved.
set -a
. "$ENV_FILE"
set +a
PUBLIC_HOST="${ELECTRU_PUBLIC_HOST:-${ELECTRU_PUBLIC_HOST_DOMAIN:-${ELECTRU_PUBLIC_HOST_V4:-${ELECTRU_PUBLIC_HOST_V6:-}}}}"
SUB_BASE="${ELECTRU_SUBSCRIPTION_BASE_URL:-${ELECTRU_SUBSCRIPTION_BASE_URL_DOMAIN:-${ELECTRU_SUBSCRIPTION_BASE_URL_V4:-${ELECTRU_SUBSCRIPTION_BASE_URL_V6:-}}}}"
[[ -n "$PUBLIC_HOST" ]] || { echo 'At least one public VPN host is required'; exit 1; }
[[ "$PUBLIC_HOST" != *://* && "$PUBLIC_HOST" != */* ]] || { echo 'ELECTRU_PUBLIC_HOST must be a host name or IP, not a URL/path'; exit 1; }
[[ -n "$SUB_BASE" ]] || { echo 'At least one HTTPS subscription base URL is required'; exit 1; }
[[ "$SUB_BASE" =~ ^https:// ]] || { echo 'Subscription base must use HTTPS'; exit 1; }
upsert(){ local k="$1" v="$2"; if grep -q "^${k}=" "$ENV_FILE"; then sed -i "s#^${k}=.*#${k}=${v}#" "$ENV_FILE"; else printf '%s=%s\n' "$k" "$v" >> "$ENV_FILE"; fi; }
upsert ELECTRU_SECURITY_MODE strict
upsert ELECTRU_PUBLIC_HOST "$PUBLIC_HOST"
upsert ELECTRU_PANEL_VPN_ACCESS 1
if [[ -n "${ELECTRU_PUBLIC_HOST_V4:-}" ]]; then upsert ELECTRU_PUBLIC_HOST_V4 "$ELECTRU_PUBLIC_HOST_V4"; fi
if [[ -n "${ELECTRU_PUBLIC_HOST_V6:-}" ]]; then upsert ELECTRU_PUBLIC_HOST_V6 "$ELECTRU_PUBLIC_HOST_V6"; fi
if [[ -n "${ELECTRU_PUBLIC_HOST_DOMAIN:-}" ]]; then upsert ELECTRU_PUBLIC_HOST_DOMAIN "$ELECTRU_PUBLIC_HOST_DOMAIN"; fi
upsert ELECTRU_SUBSCRIPTION_BASE_URL "$SUB_BASE"
if [[ -n "${ELECTRU_SUBSCRIPTION_BASE_URL_V4:-}" ]]; then upsert ELECTRU_SUBSCRIPTION_BASE_URL_V4 "$ELECTRU_SUBSCRIPTION_BASE_URL_V4"; fi
if [[ -n "${ELECTRU_SUBSCRIPTION_BASE_URL_V6:-}" ]]; then upsert ELECTRU_SUBSCRIPTION_BASE_URL_V6 "$ELECTRU_SUBSCRIPTION_BASE_URL_V6"; fi
if [[ -n "${ELECTRU_SUBSCRIPTION_BASE_URL_DOMAIN:-}" ]]; then upsert ELECTRU_SUBSCRIPTION_BASE_URL_DOMAIN "$ELECTRU_SUBSCRIPTION_BASE_URL_DOMAIN"; fi
PANEL_BIND="0.0.0.0"
# The operator explicitly wants panel access through the same VPN config.
# Keep the listener reachable; authentication/TLS/rate limits remain mandatory.
upsert ELECTRU_PANEL_BIND_ADDR "$PANEL_BIND"
cat >/etc/sysctl.d/99-electruvpn-privacy.conf <<'SYSCTL'
# Network hardening; does not disable IPv6 globally because VPN endpoints may use it.
net.ipv4.conf.all.accept_source_route=0
net.ipv4.conf.default.accept_source_route=0
net.ipv4.conf.all.accept_redirects=0
net.ipv4.conf.default.accept_redirects=0
net.ipv4.conf.all.send_redirects=0
net.ipv4.conf.default.send_redirects=0
net.ipv4.tcp_syncookies=1
net.ipv4.conf.all.rp_filter=1
net.ipv4.conf.default.rp_filter=1
net.ipv6.conf.all.accept_source_route=0
net.ipv6.conf.default.accept_source_route=0
net.ipv6.conf.all.accept_redirects=0
net.ipv6.conf.default.accept_redirects=0
SYSCTL
sysctl --system >/dev/null

if command -v ufw >/dev/null 2>&1; then
  # Do not expose DNS/DoT listeners from the VPN host to the Internet.
  ufw deny 53/tcp >/dev/null 2>&1 || true
  ufw deny 53/udp >/dev/null 2>&1 || true
  ufw deny 853/tcp >/dev/null 2>&1 || true
  ufw deny 853/udp >/dev/null 2>&1 || true
  # Keep the panel port reachable so an operator can open the panel through
  # the same VPN/proxy configuration. Do not delete the existing allow rule.
  PANEL_PORT="${ELECTRU_PANEL_PORT:-${PANEL_PORT:-2087}}"
  ufw allow "${PANEL_PORT}/tcp" >/dev/null 2>&1 || true
fi
systemctl restart electruvpn
printf '%s\n' 'Privacy mode enabled.' 'Panel: reachable for VPN/proxy access (operator requested)'  "VPN endpoint: $PUBLIC_HOST" "Subscription: $SUB_BASE"
