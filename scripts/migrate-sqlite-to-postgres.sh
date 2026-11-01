#!/usr/bin/env bash
# Migrate ElectruVPN SQLite database → PostgreSQL (schema via panel AutoMigrate + data copy).
# Usage:
#   export ELECTRU_DB_PATH=/etc/electruvpn/electruvpn.db
#   export ELECTRU_DB_DSN="host=127.0.0.1 user=electruvpn password=secret dbname=electruvpn port=5432 sslmode=disable"
#   ./scripts/migrate-sqlite-to-postgres.sh
set -euo pipefail

SQLITE_PATH="${ELECTRU_DB_PATH:-/etc/electruvpn/electruvpn.db}"
DSN="${ELECTRU_DB_DSN:-}"

if [[ -z "$DSN" ]]; then
  echo "ERROR: set ELECTRU_DB_DSN to the target PostgreSQL connection string" >&2
  exit 1
fi
if [[ ! -f "$SQLITE_PATH" ]]; then
  echo "ERROR: SQLite file not found: $SQLITE_PATH" >&2
  exit 1
fi

command -v sqlite3 >/dev/null || { echo "ERROR: sqlite3 CLI required" >&2; exit 1; }
command -v psql >/dev/null || { echo "ERROR: psql CLI required" >&2; exit 1; }

# Parse simple key=value DSN for psql
pg_host=$(echo "$DSN" | sed -n 's/.*host=\([^ ]*\).*/\1/p')
pg_user=$(echo "$DSN" | sed -n 's/.*user=\([^ ]*\).*/\1/p')
pg_pass=$(echo "$DSN" | sed -n 's/.*password=\([^ ]*\).*/\1/p')
pg_db=$(echo "$DSN" | sed -n 's/.*dbname=\([^ ]*\).*/\1/p')
pg_port=$(echo "$DSN" | sed -n 's/.*port=\([^ ]*\).*/\1/p')
pg_host=${pg_host:-127.0.0.1}
pg_port=${pg_port:-5432}
pg_user=${pg_user:-electruvpn}
pg_db=${pg_db:-electruvpn}

export PGPASSWORD="${pg_pass:-}"

echo "==> Checking PostgreSQL connectivity..."
psql -h "$pg_host" -p "$pg_port" -U "$pg_user" -d "$pg_db" -c "SELECT version();" >/dev/null

DUMP_DIR=$(mktemp -d)
trap 'rm -rf "$DUMP_DIR"' EXIT

echo "==> Exporting SQLite tables to CSV..."
tables=$(sqlite3 "$SQLITE_PATH" "SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name;")

# Let the panel create schema first if binary is available
if command -v electruvpn-server >/dev/null 2>&1 || [[ -x ./electruvpn-server ]]; then
  echo "==> Tip: start the panel once with ELECTRU_DB_DSN set so AutoMigrate creates tables, then re-run this script for data."
fi

for t in $tables; do
  cols=$(sqlite3 "$SQLITE_PATH" "PRAGMA table_info($t);" | awk -F'|' '{print $2}' | paste -sd, -)
  if [[ -z "$cols" ]]; then
    continue
  fi
  out="$DUMP_DIR/$t.csv"
  sqlite3 -header -csv "$SQLITE_PATH" "SELECT * FROM \"$t\";" > "$out"
  rows=$(($(wc -l < "$out") - 1))
  if [[ "$rows" -le 0 ]]; then
    echo "    skip empty table $t"
    continue
  fi
  # Only import if table exists in Postgres
  exists=$(psql -h "$pg_host" -p "$pg_port" -U "$pg_user" -d "$pg_db" -tAc "SELECT to_regclass('public.$t') IS NOT NULL;")
  if [[ "$exists" != "t" ]]; then
    echo "    WARN: table $t missing in Postgres (run panel once for AutoMigrate)"
    continue
  fi
  echo "    import $t ($rows rows)"
  # TRUNCATE then COPY — operator responsibility; backup first
  psql -h "$pg_host" -p "$pg_port" -U "$pg_user" -d "$pg_db" -v ON_ERROR_STOP=1 -c "TRUNCATE TABLE \"$t\" CASCADE;" >/dev/null 2>&1 || true
  psql -h "$pg_host" -p "$pg_port" -U "$pg_user" -d "$pg_db" -v ON_ERROR_STOP=1 -c "\\COPY \"$t\" FROM '$out' WITH (FORMAT csv, HEADER true)" || {
    echo "    WARN: COPY failed for $t (type mismatch possible); continue"
  }
done

echo "==> Done. Point ELECTRU_DB_DSN at PostgreSQL and restart the panel."
echo "    Keep a backup of $SQLITE_PATH until you verify data."
