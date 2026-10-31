#!/usr/bin/env bash
set -euo pipefail
BACKUP="${1:?backup path required}"
DB="${2:?database path required}"
XRAY="${3:?xray config path required}"
SING="${4:?sing-box config path required}"
TMPDIR="$(mktemp -d /tmp/electruvpn-restore.XXXXXX)"
restore_service=0
cleanup() { rc=$?; if [[ $restore_service -eq 1 ]] && ! systemctl is-active --quiet electruvpn; then systemctl start electruvpn || true; fi; rm -rf "$TMPDIR"; exit $rc; }
trap cleanup EXIT
command -v unzip >/dev/null 2>&1 || { echo 'unzip is required' >&2; exit 1; }
command -v systemctl >/dev/null 2>&1 || { echo 'systemctl is required' >&2; exit 1; }
[[ -f "$BACKUP" ]] || { echo 'backup archive not found' >&2; exit 1; }
[[ "$DB" = /* && "$XRAY" = /* && "$SING" = /* ]] || { echo 'restore paths must be absolute' >&2; exit 1; }

DBBASE="$(basename "$DB")"

# Resolve database member: exact basename first, then any .db file
mapfile -t MEMBERS < <(unzip -Z1 "$BACKUP" 2>/dev/null || true)
DBMEMBER=""
for m in "${MEMBERS[@]}"; do
  [[ "$m" == "$DBBASE" ]] && DBMEMBER="$m" && break
done
if [[ -z "$DBMEMBER" ]]; then
  for m in "${MEMBERS[@]}"; do
    case "$m" in
      *.db) DBMEMBER="$m"; break ;;
    esac
  done
fi
if [[ -z "$DBMEMBER" ]]; then
  for m in "${MEMBERS[@]}"; do
    case "$m" in
      postgres.sql|DATABASE_IS_POSTGRES.txt)
        echo "this backup is PostgreSQL-oriented ($m); file-copy restore is for SQLite only" >&2
        echo "for postgres.sql: psql \"\$ELECTRU_DB_DSN\" < extracted.sql" >&2
        exit 1
        ;;
    esac
  done
  echo 'database artifact missing (expected .db)' >&2
  exit 1
fi

unzip -p "$BACKUP" "$DBMEMBER" > "$TMPDIR/db"
[[ -s "$TMPDIR/db" ]] || { echo 'database artifact is empty' >&2; exit 1; }
chmod 600 "$TMPDIR/db"

if systemctl is-active --quiet electruvpn; then
  systemctl stop electruvpn
  restore_service=1
fi
install -D -m 600 "$TMPDIR/db" "$DB"
for pair in "$XRAY" "$SING"; do
  base="$(basename "$pair")"
  if unzip -Z1 "$BACKUP" | grep -Fx "$base" >/dev/null; then
    unzip -p "$BACKUP" "$base" > "$TMPDIR/$base"
    [[ -s "$TMPDIR/$base" ]] || { echo "empty config artifact: $base" >&2; exit 1; }
    install -D -m 600 "$TMPDIR/$base" "$pair"
  fi
done
# Accept common alternate config names from older layouts when exact basename is missing
if [[ ! -s "$XRAY" ]]; then
  for alt in config.json xray.json xray-config.json; do
    if unzip -Z1 "$BACKUP" | grep -Fx "$alt" >/dev/null; then
      unzip -p "$BACKUP" "$alt" > "$TMPDIR/$alt"
      if [[ -s "$TMPDIR/$alt" ]]; then
        install -D -m 600 "$TMPDIR/$alt" "$XRAY"
        break
      fi
    fi
  done
fi
if [[ ! -s "$SING" ]]; then
  for alt in sing-box.json singbox.json; do
    if unzip -Z1 "$BACKUP" | grep -Fx "$alt" >/dev/null; then
      unzip -p "$BACKUP" "$alt" > "$TMPDIR/$alt"
      if [[ -s "$TMPDIR/$alt" ]]; then
        install -D -m 600 "$TMPDIR/$alt" "$SING"
        break
      fi
    fi
  done
fi
systemctl start electruvpn
restore_service=0
echo "restore completed from $BACKUP (db member: $DBMEMBER)"
