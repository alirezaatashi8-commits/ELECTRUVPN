#!/usr/bin/env bash
set -u
ENV_FILE="${1:-/etc/electruvpn/electruvpn.env}"
BACKUP_DIR="${2:-}"
NEW_PORT="${3:-}"
SERVICE="electruvpn"

log(){ logger -t electruvpn-panel-settings "$*" 2>/dev/null || true; }
restore_file(){
  local backup="$1" target="$2"
  if [[ -f "$backup" ]]; then cp -f "$backup" "$target"; chmod 600 "$target" 2>/dev/null || true
  else rm -f "$target"; fi
}

sleep 1
if command -v ufw >/dev/null 2>&1 && ufw status 2>/dev/null | grep -q "Status: active"; then
  ufw allow "${NEW_PORT}/tcp" >/dev/null 2>&1 || true
fi
systemctl restart "$SERVICE" || true
ok=0
for _ in $(seq 1 20); do
  if systemctl is-active --quiet "$SERVICE"; then
    if [[ "$NEW_PORT" =~ ^[0-9]+$ ]]; then
      if timeout 1 bash -c "</dev/tcp/127.0.0.1/$NEW_PORT" 2>/dev/null; then ok=1; break; fi
    else
      ok=1; break
    fi
  fi
  sleep 1
done

if [[ "$ok" == "1" ]]; then
  log "panel settings apply succeeded on port $NEW_PORT"
  rm -rf "$BACKUP_DIR" 2>/dev/null || true
  exit 0
fi

log "panel settings apply failed; restoring previous configuration"
if [[ -n "$BACKUP_DIR" && -d "$BACKUP_DIR" ]]; then
  restore_file "$BACKUP_DIR/electruvpn.env" "$ENV_FILE"
  restore_file "$BACKUP_DIR/panel-tls.crt" "$(dirname "$ENV_FILE")/panel-tls.crt"
  restore_file "$BACKUP_DIR/panel-tls.key" "$(dirname "$ENV_FILE")/panel-tls.key"
fi
systemctl restart "$SERVICE" || true
exit 1
