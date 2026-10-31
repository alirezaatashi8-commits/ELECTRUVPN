#!/usr/bin/env bash
set -euo pipefail
SERVER="${1:-${ELECTRUVPN_AGENT_SERVER:-}}"
TOKEN="${2:-${ELECTRUVPN_AGENT_TOKEN:-}}"
if [[ -z "$SERVER" || -z "$TOKEN" ]]; then
  echo "Usage: sudo bash scripts/install-agent.sh https://panel.example.com/<base>/api/agent AGENT_TOKEN"
  exit 2
fi
case "$SERVER" in
  https://*) ;;
  *) echo "Agent endpoint must use HTTPS"; exit 1 ;;
esac
[[ "$SERVER" != *$'\r'* && "$SERVER" != *$'\n'* ]] || { echo "Agent endpoint contains an invalid newline"; exit 1; }
[[ "$TOKEN" =~ ^[A-Za-z0-9._-]{16,512}$ ]] || { echo "Agent token contains invalid characters"; exit 1; }
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
command -v go >/dev/null || { echo "Go 1.27+ is required"; exit 1; }
if [[ "$(id -u)" -ne 0 ]]; then echo "Run this installer as root"; exit 1; fi
GO_VERSION_RAW="$(go version | awk '{print $3}')"
GO_MAJOR="${GO_VERSION_RAW#go}"
GO_MAJOR="${GO_MAJOR%%.*}"
GO_MINOR="${GO_VERSION_RAW#go*.}"
GO_MINOR="${GO_MINOR%%.*}"
if [[ "$GO_MAJOR" -lt 1 || ( "$GO_MAJOR" -eq 1 && "$GO_MINOR" -lt 27 ) ]]; then
  echo "Go 1.27+ is required; found ${GO_VERSION_RAW}"
  exit 1
fi
cd "$ROOT_DIR"
# A fresh source bundle may not carry go.sum. Downloading the pinned module graph
# first makes installation deterministic without silently changing go.mod versions.
go mod download
install -d -m 0750 /etc/electruvpn /var/lib/electruvpn-agent/config-history/xray /var/lib/electruvpn-agent/config-history/singbox
go build -trimpath -o /usr/local/bin/electruvpn-agent ./cmd/agent
install -m 0750 /usr/local/bin/electruvpn-agent /usr/local/bin/electruvpn-agent
umask 077
cat >/etc/electruvpn/agent.env <<ENV
ELECTRUVPN_AGENT_SERVER=$SERVER
ELECTRUVPN_AGENT_TOKEN=$TOKEN
ENV
chmod 0600 /etc/electruvpn/agent.env
install -m 0644 "$ROOT_DIR/deploy/electruvpn-agent.service" /etc/systemd/system/electruvpn-agent.service
systemctl daemon-reload
systemctl enable --now electruvpn-agent
systemctl --no-pager --full status electruvpn-agent || true
echo "Agent installed. Token is stored in /etc/electruvpn/agent.env (0600)."
