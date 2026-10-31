#!/usr/bin/env bash
set -euo pipefail

ENV_FILE="${ELECTRU_ENV_FILE:-/etc/electruvpn/electruvpn.env}"
if [[ -r "$ENV_FILE" ]]; then
  # shellcheck disable=SC1090
  source "$ENV_FILE"
fi

RETENTION="${ELECTRU_LOG_RETENTION:-1h}"
MAX_SIZE="${ELECTRU_LOG_MAX_SIZE:-64M}"

# Keep the values intentionally simple: they are root-owned configuration,
# but rejecting whitespace/shell metacharacters prevents accidental bad calls.
[[ "$RETENTION" =~ ^[0-9]+(s|min|h|days|weeks)$ ]] || RETENTION="1h"
[[ "$MAX_SIZE" =~ ^[0-9]+(K|M|G)$ ]] || MAX_SIZE="64M"

if ! command -v journalctl >/dev/null 2>&1; then
  exit 0
fi

# Vacuum only archived journal data. The currently-open journal is never
# deleted, so active logging remains uninterrupted.
journalctl --vacuum-time="$RETENTION" --vacuum-size="$MAX_SIZE" >/dev/null 2>&1 || true
