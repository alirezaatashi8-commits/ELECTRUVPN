#!/usr/bin/env bash
# geo-update.sh -- keep Xray's geoip.dat / geosite.dat fresh.
#
# Default source is Chocolate4U/Iran-v2ray-rules (standard v2fly data plus Iran-specific
# categories such as geoip:ir and geosite:category-ir). If it is unreachable the
# standard Loyalsoldier/v2ray-rules-dat build is used instead.
#
#   geo-update.sh status
#   geo-update.sh update [--no-restart]
#
# Environment (all optional):
#   ELECTRU_GEO_DIR     target directory   (default /usr/local/share/xray)
#   ELECTRU_GEO_SOURCE  iran | standard    (default iran)
set -euo pipefail

ENV_FILE="${ELECTRU_ENV_FILE:-/etc/electruvpn/electruvpn.env}"
if [[ -r "$ENV_FILE" ]]; then
  # shellcheck disable=SC1090
  source "$ENV_FILE" || true
fi

GEO_DIR="${ELECTRU_GEO_DIR:-/usr/local/share/xray}"
SOURCE="${ELECTRU_GEO_SOURCE:-iran}"
FILES=(geoip.dat geosite.dat)
MIN_BYTES=100000
LOCK=/run/electruvpn-geo.lock

IRAN_BASE="https://github.com/Chocolate4U/Iran-v2ray-rules/releases/latest/download"
STD_BASE="https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download"

log()  { echo "[geo] $*"; }
fail() { echo "[geo] ERROR: $*" >&2; exit 1; }

cmd_status() {
  echo "Directory: $GEO_DIR   Source: $SOURCE"
  for f in "${FILES[@]}"; do
    if [[ -f "$GEO_DIR/$f" ]]; then
      printf '%-14s %10s bytes   updated %s\n' "$f" "$(stat -c %s "$GEO_DIR/$f")" "$(date -d "@$(stat -c %Y "$GEO_DIR/$f")" '+%Y-%m-%d %H:%M')"
    else
      printf '%-14s missing\n' "$f"
    fi
  done
}

# fetch BASE FILE OUT  -> 0 on a verified/plausible download
fetch() {
  local base="$1" f="$2" out="$3" sumfile want got
  curl --fail --silent --show-error --location --proto '=https' --tlsv1.2 \
       --retry 3 --retry-delay 2 --connect-timeout 15 --max-time 180 \
       -o "$out" "$base/$f" || return 1
  [[ "$(stat -c %s "$out")" -ge "$MIN_BYTES" ]] || { log "$f is suspiciously small"; return 1; }
  sumfile="$(mktemp)"
  if curl --fail --silent --location --proto '=https' --tlsv1.2 --connect-timeout 10 --max-time 30 \
          -o "$sumfile" "$base/$f.sha256sum" 2>/dev/null; then
    want="$(awk '{print tolower($1); exit}' "$sumfile")"
    got="$(sha256sum "$out" | awk '{print $1}')"
    rm -f "$sumfile"
    if [[ "$want" =~ ^[0-9a-f]{64}$ ]]; then
      [[ "$want" == "$got" ]] || { log "SHA-256 mismatch for $f"; return 1; }
    else
      log "no usable checksum published for $f (size check only)"
    fi
  else
    rm -f "$sumfile"
    log "no checksum file published for $f (size check only)"
  fi
  return 0
}

cmd_update() {
  local restart=1 tmp changed=0 f base ok
  [[ "${1:-}" == "--no-restart" ]] && restart=0
  command -v curl >/dev/null || fail "curl is required"
  exec 9>"$LOCK"; flock -n 9 || fail "another geo update is already running"
  mkdir -p "$GEO_DIR"
  tmp="$(mktemp -d /tmp/electruvpn-geo.XXXXXX)"
  # Expand $tmp now: it is a function-local, and an EXIT trap runs after the
  # function returns, where `set -u` would make a lazy "$tmp" an unbound variable.
  trap "rm -rf -- '$tmp'" EXIT

  local bases=()
  if [[ "$SOURCE" == "standard" ]]; then bases=("$STD_BASE"); else bases=("$IRAN_BASE" "$STD_BASE"); fi

  ok=0
  for base in "${bases[@]}"; do
    rm -f "$tmp"/*.dat
    local good=1
    for f in "${FILES[@]}"; do
      log "downloading $f from ${base##*github.com/}"
      fetch "$base" "$f" "$tmp/$f" || { good=0; break; }
    done
    if [[ $good == 1 ]]; then ok=1; break; fi
    log "source failed, trying the next one"
  done
  [[ $ok == 1 ]] || fail "could not download verified geo data from any source (existing files were left untouched)"

  for f in "${FILES[@]}"; do
    if [[ -f "$GEO_DIR/$f" ]] && cmp -s "$tmp/$f" "$GEO_DIR/$f"; then
      log "$f is already up to date"
      continue
    fi
    [[ -f "$GEO_DIR/$f" ]] && cp -p "$GEO_DIR/$f" "$GEO_DIR/$f.bak"
    install -m 0644 "$tmp/$f" "$GEO_DIR/.$f.new"
    mv -f "$GEO_DIR/.$f.new" "$GEO_DIR/$f"
    log "$f updated"
    changed=1
  done

  if [[ $changed == 1 && $restart == 1 ]] && systemctl is-active --quiet electruvpn 2>/dev/null; then
    log "restarting electruvpn so Xray loads the new data"
    systemctl restart electruvpn
  fi
  log "done"
}

case "${1:-status}" in
  status) cmd_status ;;
  update) shift; cmd_update "${1:-}" ;;
  *) echo "Usage: $0 [status|update [--no-restart]]"; exit 1 ;;
esac
