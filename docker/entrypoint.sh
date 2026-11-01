#!/bin/sh
# Container entrypoint: generates and persists secrets on first start, then runs the panel.
set -eu

DATA="${ELECTRU_DATA_DIR:-/data}"
mkdir -p "$DATA"
umask 077

rand_hex() { head -c "$1" /dev/urandom | od -An -tx1 | tr -d ' \n'; }

if [ -z "${ELECTRU_JWT_SECRET:-}" ]; then
  [ -s "$DATA/jwt.secret" ] || rand_hex 32 > "$DATA/jwt.secret"
  ELECTRU_JWT_SECRET="$(cat "$DATA/jwt.secret")"
  export ELECTRU_JWT_SECRET
fi

if [ -z "${ELECTRU_BASE_PATH:-}" ]; then
  [ -s "$DATA/base-path" ] || printf '/panel-%s' "$(rand_hex 8)" > "$DATA/base-path"
  ELECTRU_BASE_PATH="$(cat "$DATA/base-path")"
  export ELECTRU_BASE_PATH
fi

if [ ! -f "$ELECTRU_DB_PATH" ] && [ -z "${ELECTRU_INITIAL_ADMIN_PASSWORD:-}" ]; then
  ELECTRU_INITIAL_ADMIN_PASSWORD="$(rand_hex 12)"
  export ELECTRU_INITIAL_ADMIN_PASSWORD
  echo "=============================================================="
  echo " ELECTRUVPN first start"
  echo " Login    : admin / ${ELECTRU_INITIAL_ADMIN_PASSWORD}"
  echo " Port     : ${ELECTRU_PANEL_PORT:-2087}   Base path: ${ELECTRU_BASE_PATH}"
  echo " Change the password after the first login."
  echo "=============================================================="
fi

exec "$@"
