#!/usr/bin/env bash
set -euo pipefail
# Usage: DOMAIN=panel.example.com [PANEL_PORT=2087] [TLS_CERT=/path/fullchain.pem TLS_KEY=/path/privkey.pem] bash scripts/setup-reverse-proxy.sh
[[ $EUID -eq 0 ]] || { echo "Run as root"; exit 1; }
DOMAIN="${DOMAIN:-}"; PANEL_PORT="${PANEL_PORT:-2087}"
BASE_PATH="${ELECTRU_BASE_PATH:-/}"
BASE_PATH="/${BASE_PATH#/}"
BASE_PATH="${BASE_PATH%/}"
[[ -n "$BASE_PATH" ]] || BASE_PATH="/"
if [[ "$BASE_PATH" == "/" ]]; then
  NGINX_BASE_LOCATION="/"
  NGINX_LOGIN_LOCATION="/api/login"
  NGINX_SUB_LOCATION="/sub/"
  NGINX_XUI_IMPORT_LOCATION="/api/inbounds/import-3x-ui"
  NGINX_BACKUP_UPLOAD_LOCATION="/api/pro/backups/upload"
else
  NGINX_BASE_LOCATION="${BASE_PATH}/"
  NGINX_LOGIN_LOCATION="${BASE_PATH}/api/login"
  NGINX_SUB_LOCATION="${BASE_PATH}/sub/"
  NGINX_XUI_IMPORT_LOCATION="${BASE_PATH}/api/inbounds/import-3x-ui"
  NGINX_BACKUP_UPLOAD_LOCATION="${BASE_PATH}/api/pro/backups/upload"
fi
[[ -n "$DOMAIN" ]] || { echo "DOMAIN is required"; exit 1; }
apt-get update -y -qq
apt-get install -y -qq nginx ufw >/dev/null
CONF=/etc/nginx/sites-available/electruvpn
cat >"$CONF" <<EOF
limit_req_zone \$binary_remote_addr zone=electruvpn_login:10m rate=10r/m;
limit_req_zone \$binary_remote_addr zone=electruvpn_api:10m rate=30r/s;
server {
    listen 80;
    server_name ${DOMAIN};
    client_max_body_size 2m;
    gzip on;
    gzip_vary on;
    gzip_min_length 1024;
    gzip_types text/plain text/css application/javascript application/json image/svg+xml;
    # Large uploads are permitted only on the two authenticated import/restore endpoints.
    # All other requests retain the small server-wide request limit.
    location = ${NGINX_XUI_IMPORT_LOCATION} {
        client_max_body_size 130m;
        limit_req zone=electruvpn_api burst=10 nodelay;
        proxy_pass http://127.0.0.1:${PANEL_PORT};
        proxy_http_version 1.1;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        proxy_set_header Connection "";
        proxy_read_timeout 300s;
        proxy_send_timeout 300s;
    }
    location = ${NGINX_BACKUP_UPLOAD_LOCATION} {
        client_max_body_size 1538m;
        limit_req zone=electruvpn_api burst=5 nodelay;
        proxy_pass http://127.0.0.1:${PANEL_PORT};
        proxy_http_version 1.1;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        proxy_set_header Connection "";
        proxy_read_timeout 900s;
        proxy_send_timeout 900s;
    }
    location ${NGINX_BASE_LOCATION} {
        limit_req zone=electruvpn_api burst=60 nodelay;
        add_header X-Content-Type-Options nosniff always;
        add_header X-Frame-Options DENY always;
        add_header Referrer-Policy no-referrer always;
        add_header Permissions-Policy "camera=(), microphone=(), geolocation=(), payment=(), usb=()" always;
        proxy_pass http://127.0.0.1:${PANEL_PORT};
        proxy_http_version 1.1;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        proxy_set_header Connection "";
        proxy_read_timeout 60s;
        proxy_send_timeout 60s;
    }
    location ^~ ${NGINX_BASE_LOCATION}assets/ {
        access_log off;
        add_header Cache-Control "no-cache" always;
        proxy_pass http://127.0.0.1:${PANEL_PORT};
        proxy_http_version 1.1;
        proxy_set_header Host \$host;
        proxy_set_header X-Forwarded-Proto \$scheme;
        proxy_set_header Connection "";
        proxy_read_timeout 20s;
        proxy_send_timeout 20s;
    }
    location ^~ ${NGINX_SUB_LOCATION} {
        access_log off;
        proxy_pass http://127.0.0.1:${PANEL_PORT};
        proxy_http_version 1.1;
        proxy_set_header Host \$host;
        proxy_set_header X-Forwarded-Proto \$scheme;
        proxy_set_header Connection "";
        proxy_read_timeout 20s;
        proxy_send_timeout 20s;
    }
    location = ${NGINX_LOGIN_LOCATION} {
        limit_req zone=electruvpn_login burst=5 nodelay;
        proxy_pass http://127.0.0.1:${PANEL_PORT};
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
    }
}
EOF
ln -sf "$CONF" /etc/nginx/sites-enabled/electruvpn
rm -f /etc/nginx/sites-enabled/default
nginx -t
systemctl enable --now nginx
# Bind the panel to localhost after nginx is ready.
if [[ -f /etc/electruvpn/electruvpn.env ]]; then
  if grep -q '^ELECTRU_PANEL_BIND_ADDR=' /etc/electruvpn/electruvpn.env; then
    sed -i 's/^ELECTRU_PANEL_BIND_ADDR=.*/ELECTRU_PANEL_BIND_ADDR=127.0.0.1/' /etc/electruvpn/electruvpn.env
  else
    printf '\nELECTRU_PANEL_BIND_ADDR=127.0.0.1\n' >> /etc/electruvpn/electruvpn.env
  fi
  systemctl restart electruvpn
fi
ufw allow 'Nginx Full' >/dev/null 2>&1 || ufw allow 80/tcp >/dev/null 2>&1 || true
ufw delete allow "${PANEL_PORT}/tcp" >/dev/null 2>&1 || true
echo "Reverse proxy configured for http://${DOMAIN}${BASE_PATH}. Add TLS with your certificate provider before using it for sensitive administration."
