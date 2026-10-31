#!/usr/bin/env bash
# electruvpn -- server-side management CLI for the ELECTRUVPN panel.
# Installed to /usr/local/bin/electruvpn by install.sh. Must run as root.
set -euo pipefail

ENV_FILE="${ELECTRU_ENV_FILE:-/etc/electruvpn/electruvpn.env}"
SERVICE="electruvpn"
INSTALL_DIR="${INSTALL_DIR:-/opt/electruvpn}"

RED=$'\033[31m'; GREEN=$'\033[32m'; YELLOW=$'\033[33m'; CYAN=$'\033[36m'; BOLD=$'\033[1m'; RESET=$'\033[0m'
die()  { echo "${RED}✗ $*${RESET}" >&2; exit 1; }
ok()   { echo "${GREEN}✓${RESET} $*"; }
info() { echo "${CYAN}➜${RESET} $*"; }
warn() { echo "${YELLOW}!${RESET} $*"; }

[[ $EUID -eq 0 ]] || die "Run as root: sudo electruvpn"
[[ -r "$ENV_FILE" ]] || die "Config not found: $ENV_FILE (is ELECTRUVPN installed?)"

env_get() { grep -oP "^$1=\K.*" "$ENV_FILE" 2>/dev/null | tail -n1 || true; }

# Replace KEY=value in the env file, or append it. The value is written
# through awk (not sed) so characters like / & # in values are safe.
env_set() {
  local key="$1" val="$2" tmp
  tmp="$(mktemp "${ENV_FILE}.XXXXXX")"
  if grep -q "^${key}=" "$ENV_FILE"; then
    awk -v k="$key" -v v="$val" 'BEGIN{FS=OFS="="} $1==k {print k "=" v; next} {print}' "$ENV_FILE" > "$tmp"
  else
    cat "$ENV_FILE" > "$tmp"
    printf '%s=%s\n' "$key" "$val" >> "$tmp"
  fi
  chmod 600 "$tmp"
  mv -f "$tmp" "$ENV_FILE"
}

# Use the configured DSN when available. On older installations the env file can
# have an empty DSN even though install-prerequisites.sh created the local
# PostgreSQL role/database and saved its password. In that case, recover the
# local connection details from the protected password file.
pg_local_ready() {
  [[ -s /etc/electruvpn/.pg_password ]] && command -v psql >/dev/null 2>&1 && command -v pg_isready >/dev/null 2>&1 && pg_isready -h 127.0.0.1 -p 5432 >/dev/null 2>&1
}

# The panel's Go driver accepts extra keys such as TimeZone=UTC in the DSN, but
# libpq tools (psql/pg_dump) reject them ("invalid connection option").
# Strip those keys before handing the DSN to a libpq client.
pg_libpq_dsn() {
  printf '%s' "$1" | sed -E \
    -e 's/(^|[[:space:]])(TimeZone|timezone)=[^[:space:]]*//g' \
    -e 's/([?&])(TimeZone|timezone)=[^&]*&?/\1/g' \
    -e 's/^[[:space:]]+//'
}

pg_psql() {
  local dsn="$1"; shift
  if [[ -n "$dsn" ]]; then
    dsn="$(pg_libpq_dsn "$dsn")"
    PGTZ=UTC psql "$dsn" "$@"
  else
    local passfile=/etc/electruvpn/.pg_password
    [[ -s "$passfile" ]] || return 1
    PGPASSWORD="$(cat "$passfile")" psql -h 127.0.0.1 -p 5432 \
      -U "$(env_get ELECTRU_PG_USER)" -d "$(env_get ELECTRU_PG_DB)" "$@"
  fi
}

pg_dump_local_or_dsn() {
  local dsn="$1" out="$2"
  if [[ -n "$dsn" ]]; then
    pg_dump "$(pg_libpq_dsn "$dsn")" > "$out"
  else
    local passfile=/etc/electruvpn/.pg_password user db
    [[ -s "$passfile" ]] || return 1
    user="$(env_get ELECTRU_PG_USER)"; db="$(env_get ELECTRU_PG_DB)"
    [[ -n "$user" ]] || user=electruvpn
    [[ -n "$db" ]] || db=electruvpn
    PGPASSWORD="$(cat "$passfile")" pg_dump -h 127.0.0.1 -p 5432 -U "$user" -d "$db" > "$out"
  fi
}

panel_scheme() { [[ "$(env_get ELECTRU_PANEL_TLS_ENABLED)" == "1" ]] && echo https || echo http; }

panel_url() {
  local host port base scheme
  host="$(env_get ELECTRU_PANEL_TLS_HOST)"
  [[ -n "$host" ]] || host="$(env_get ELECTRU_PUBLIC_HOST)"
  [[ -n "$host" ]] || host="$(curl -4 -fsS --max-time 5 https://api.ipify.org 2>/dev/null || hostname -I | awk '{print $1}')"
  port="$(env_get ELECTRU_PANEL_PORT)"; base="$(env_get ELECTRU_BASE_PATH)"; scheme="$(panel_scheme)"
  [[ "$base" == "/" ]] && base=""
  if [[ ( "$scheme" == https && "$port" == 443 ) || ( "$scheme" == http && "$port" == 80 ) ]]; then
    echo "${scheme}://${host}${base}"
  else
    echo "${scheme}://${host}:${port}${base}"
  fi
}

cmd_status() {
  systemctl --no-pager --lines=0 status "$SERVICE" || true
  echo
  echo "Panel URL : $(panel_url)"
  echo "Port      : $(env_get ELECTRU_PANEL_PORT)"
  echo "Base path : $(env_get ELECTRU_BASE_PATH)"
  echo "TLS       : $(env_get ELECTRU_PANEL_TLS_ENABLED)"
}

cmd_url() { panel_url; }

cmd_logs() { exec journalctl -u "$SERVICE" -n "${1:-100}" -f --no-pager; }

cmd_port() {
  local p="${1:-}"
  [[ "$p" =~ ^[0-9]+$ ]] && (( p >= 1 && p <= 65535 )) || die "Usage: electruvpn port <1-65535>"
  local old; old="$(env_get ELECTRU_PANEL_PORT)"
  env_set ELECTRU_PANEL_PORT "$p"
  ufw allow "${p}/tcp" >/dev/null 2>&1 || true
  [[ -n "$old" && "$old" != "$p" ]] && ufw delete allow "${old}/tcp" >/dev/null 2>&1 || true
  if [[ -f /etc/fail2ban/jail.d/electruvpn-auth.local ]]; then
    sed -i "s/^port = .*/port = $p/" /etc/fail2ban/jail.d/electruvpn-auth.local
    systemctl restart fail2ban >/dev/null 2>&1 || true
  fi
  systemctl restart "$SERVICE"
  ok "Panel port set to $p. New URL: $(panel_url)"
}

cmd_path() {
  local np="${1:-}"
  if [[ "$np" == "random" || -z "$np" ]]; then np="/panel-$(openssl rand -hex 8)"; fi
  [[ "$np" == /* ]] || np="/$np"
  [[ "$np" =~ ^/[A-Za-z0-9._~-]+$ || "$np" == "/" ]] || die "Invalid path. Use letters, digits, - _ . ~ (example: /my-panel) or 'random'."
  env_set ELECTRU_BASE_PATH "$np"
  systemctl restart "$SERVICE"
  ok "Base path set to $np. New URL: $(panel_url)"
}

# Return true only when a DSN points at this host. This lets us recover from a
# stale local password in ELECTRU_DB_DSN without ever changing a remote database.
pg_dsn_param() {
  local key="$1" dsn="$2"
  python3 - "$key" "$dsn" <<'PYDSN'
import re, shlex, sys, urllib.parse
key, dsn = sys.argv[1], sys.argv[2]
try:
    if dsn.startswith(("postgres://", "postgresql://")):
        u = urllib.parse.urlparse(dsn)
        if key == "user": print(urllib.parse.unquote(u.username or ""))
        elif key == "dbname": print(urllib.parse.unquote((u.path or "/").lstrip("/")))
        else: print("")
    else:
        values = {}
        for item in shlex.split(dsn):
            if "=" in item:
                k, v = item.split("=", 1)
                values[k] = v
        print(values.get(key, ""))
except Exception:
    print("")
PYDSN
}

pg_dsn_is_local() {
  local dsn="$1" host=""
  [[ -z "$dsn" ]] && return 0
  if [[ "$dsn" == postgres://* || "$dsn" == postgresql://* ]]; then
    host="$(python3 - "$dsn" <<'PYURL'
import sys, urllib.parse
try:
    u = urllib.parse.urlparse(sys.argv[1])
    print((u.hostname or "").lower())
except Exception:
    print("")
PYURL
)"
  else
    host="$(printf '%s\n' "$dsn" | grep -oP '(?:^|\s)host=\K[^\s]+' | tail -n1 || true)"
  fi
  [[ -z "$host" || "$host" == localhost || "$host" == 127.0.0.1 || "$host" == ::1 || "$host" == /var/run/postgresql ]]
}

# Inspect the live schema first and update only columns that really exist.
# Output is the affected row id, or an actionable error on stderr.
pg_reset_admin_row() {
  local dsn="$1" hash="$2" cols updated sets
  cols="$(pg_psql "$dsn" -v ON_ERROR_STOP=1 -Atqc \
    "SELECT column_name FROM information_schema.columns WHERE table_schema = 'public' AND table_name = 'users' ORDER BY ordinal_position;" 2>&1)" || {
      printf '%s\n' "$cols" >&2
      return 1
    }
  if ! grep -Fxq 'password_hash' <<<"$cols" || ! grep -Fxq 'id' <<<"$cols"; then
    echo "The connected database has no compatible public.users table (required columns: id, password_hash)." >&2
    return 1
  fi
  sets="password_hash = :'pw_hash'"
  if grep -Fxq 'must_change_password' <<<"$cols"; then sets+=", must_change_password = TRUE"; fi
  if grep -Fxq 'password_changed_at' <<<"$cols"; then sets+=", password_changed_at = NOW()"; fi

  if grep -Fxq 'username' <<<"$cols"; then
    updated="$(printf '%s\n' "UPDATE public.users SET $sets WHERE id = (SELECT id FROM public.users WHERE username = 'admin' ORDER BY id LIMIT 1) RETURNING id;" \
      | pg_psql "$dsn" -v ON_ERROR_STOP=1 -v "pw_hash=$hash" -Atq 2>&1)" || {
        printf '%s\n' "$updated" >&2
        return 1
      }
  else
    updated=""
  fi
  if [[ -z "$updated" ]] && grep -Fxq 'role' <<<"$cols"; then
    updated="$(printf '%s\n' "UPDATE public.users SET $sets WHERE id = (SELECT id FROM public.users WHERE role = 'owner' ORDER BY id LIMIT 1) RETURNING id;" \
      | pg_psql "$dsn" -v ON_ERROR_STOP=1 -v "pw_hash=$hash" -Atq 2>&1)" || {
        printf '%s\n' "$updated" >&2
        return 1
      }
  fi
  [[ -n "$updated" ]] || { echo "No admin/owner user was found in public.users." >&2; return 1; }
  printf '%s\n' "$updated"
}

cmd_resetpw() {
  local db dsn newpw hash updated err local_user local_db local_pass local_dsn
  dsn="$(env_get ELECTRU_DB_DSN)"
  db="$(env_get ELECTRU_DB_PATH)"
  [[ -n "$db" ]] || db="/etc/electruvpn/electruvpn.db"
  command -v python3 >/dev/null || die "python3 is required"
  python3 -c 'import bcrypt' 2>/dev/null || {
    info "Installing the python bcrypt module (one time)..."
    apt-get install -y -qq python3-bcrypt >/dev/null 2>&1 || die "Could not install python3-bcrypt"
  }
  newpw="${1:-$(openssl rand -hex 12)}"
  [[ ${#newpw} -ge 8 ]] || die "Password must be at least 8 characters."
  hash="$(ELECTRU_NEWPW="$newpw" python3 -c 'import os,bcrypt; print(bcrypt.hashpw(os.environ["ELECTRU_NEWPW"].encode(), bcrypt.gensalt(10)).decode())')" || die "Could not hash the new password."

  if [[ -n "$dsn" || ( ! -f "$db" && -s /etc/electruvpn/.pg_password ) ]]; then
    command -v psql >/dev/null || die "psql is required for PostgreSQL password reset (install postgresql-client)."
    err=""
    if updated="$(pg_reset_admin_row "$dsn" "$hash" 2>&1)"; then
      :
    else
      err="$updated"
      updated=""
      # Retry only against installer-managed local PostgreSQL when the configured
      # DSN points locally. Never reset an unrelated remote database by accident.
      if pg_dsn_is_local "$dsn" && [[ -s /etc/electruvpn/.pg_password ]] && pg_local_ready; then
        local_user="$(env_get ELECTRU_PG_USER)"; local_db="$(env_get ELECTRU_PG_DB)"
        [[ -n "$local_user" ]] || local_user="$(pg_dsn_param user "$dsn")"
        [[ -n "$local_db" ]] || local_db="$(pg_dsn_param dbname "$dsn")"
        [[ -n "$local_user" ]] || local_user=electruvpn
        [[ -n "$local_db" ]] || local_db=electruvpn
        env_set ELECTRU_PG_USER "$local_user"
        env_set ELECTRU_PG_DB "$local_db"
        if updated="$(pg_reset_admin_row "" "$hash" 2>&1)"; then
          # If the configured local DSN had stale credentials, repair it too so
          # the panel can reconnect to the same database after its restart.
          local_pass="$(tr -d '\r\n' < /etc/electruvpn/.pg_password)"
          local_dsn="host=127.0.0.1 user=$local_user password=$local_pass dbname=$local_db port=5432 sslmode=disable TimeZone=UTC"
          env_set ELECTRU_DB_DSN "$local_dsn"
          warn "The configured local PostgreSQL DSN was repaired using the installer-managed credentials."
          if systemctl restart "$SERVICE"; then
            ok "Panel service restarted with the repaired database connection."
          else
            warn "Password was reset, but the panel did not restart cleanly. Check: journalctl -u $SERVICE -n 80 --no-pager"
          fi
        else
          err+=$'\nLocal PostgreSQL retry:\n'"$updated"
          updated=""
        fi
      fi
      if [[ -z "$updated" ]]; then
        echo "PostgreSQL password reset failed. Diagnostic (connection passwords are redacted):" >&2
        printf '%s\n' "$err" | tail -n 8 | sed -E 's/(password=)[^[:space:]]+/\1[REDACTED]/Ig' >&2
        die "No password was changed. Check PostgreSQL connectivity, the configured database, and the users table schema."
      fi
    fi
    ok "Admin password reset. Login: admin / ${newpw} (you will be asked to change it)"
  else
    [[ -f "$db" ]] || die "Database not found: $db (and ELECTRU_DB_DSN is empty; check $ENV_FILE)."
    ELECTRU_DB="$db" ELECTRU_HASH="$hash" python3 - <<'PYSQL'
import os, sqlite3, datetime
con = sqlite3.connect(os.environ["ELECTRU_DB"])
try:
    cols = {r[1] for r in con.execute("PRAGMA table_info(users)")}
    if not cols:
        raise SystemExit("users table not found in SQLite database")
    row = con.execute("SELECT id FROM users WHERE username='admin' ORDER BY id LIMIT 1").fetchone() if "username" in cols else None
    if not row and "role" in cols:
        row = con.execute("SELECT id FROM users WHERE role='owner' ORDER BY id LIMIT 1").fetchone()
    if not row:
        raise SystemExit("no admin/owner user found")
    sets, args = ["password_hash=?"], [os.environ["ELECTRU_HASH"]]
    if "must_change_password" in cols:
        sets.append("must_change_password=1")
    if "password_changed_at" in cols:
        sets.append("password_changed_at=?")
        args.append(datetime.datetime.utcnow().strftime("%Y-%m-%d %H:%M:%S"))
    args.append(row[0])
    con.execute("UPDATE users SET " + ",".join(sets) + " WHERE id=?", args)
    con.commit()
finally:
    con.close()
PYSQL
    [[ $? -eq 0 ]] || die "SQLite password reset failed."
    ok "Admin password reset. Login: admin / ${newpw} (you will be asked to change it)"
  fi
}

cmd_backup() {
  local db dsn dir out tmp
  dsn="$(env_get ELECTRU_DB_DSN)"
  db="$(env_get ELECTRU_DB_PATH)"
  [[ -n "$db" ]] || db="/etc/electruvpn/electruvpn.db"
  dir="$(dirname "$db")/backups"
  mkdir -p "$dir"
  out="$dir/manual-$(date +%Y%m%d-%H%M%S).tar.gz"
  tmp="$(mktemp -d "$dir/.backup.XXXXXX")"
  if [[ -n "$dsn" || ( ! -f "$db" && -s /etc/electruvpn/.pg_password ) ]]; then
    command -v pg_dump >/dev/null || { rm -rf "$tmp"; die "pg_dump is required for PostgreSQL backups (install postgresql-client)."; }
    if [[ -z "$dsn" ]]; then
      pg_local_ready || { rm -rf "$tmp"; die "PostgreSQL DSN is empty and local PostgreSQL is not ready."; }
      [[ -n "$(env_get ELECTRU_PG_USER)" ]] || env_set ELECTRU_PG_USER electruvpn
      [[ -n "$(env_get ELECTRU_PG_DB)" ]] || env_set ELECTRU_PG_DB electruvpn
    fi
    if ! pg_dump_local_or_dsn "$dsn" "$tmp/postgres.sql"; then rm -rf "$tmp"; die "PostgreSQL backup failed; check database credentials and connectivity."; fi
    tar -czf "$out" -C "$tmp" postgres.sql || { rm -rf "$tmp"; die "Could not package PostgreSQL backup."; }
  else
    [[ -f "$db" ]] || { rm -rf "$tmp"; die "Database not found: $db (and ELECTRU_DB_DSN is empty)."; }
    command -v sqlite3 >/dev/null || { rm -rf "$tmp"; die "sqlite3 is required for a consistent SQLite backup."; }
    sqlite3 "$db" ".backup '$tmp/electruvpn.db'" || { rm -rf "$tmp"; die "SQLite backup failed."; }
    tar -czf "$out" -C "$tmp" electruvpn.db || { rm -rf "$tmp"; die "Could not package SQLite backup."; }
  fi
  rm -rf "$tmp"
  chmod 600 "$out"
  ok "Database backup written: $out"
}

cmd_geo() { exec /opt/electruvpn/geo-update.sh "$@"; }

cmd_metrics() {
  case "${1:-status}" in
    enable)  local t; t="$(openssl rand -hex 24)"; env_set ELECTRU_METRICS_TOKEN "$t"; systemctl restart "$SERVICE"
             ok "Metrics enabled. Scrape: $(panel_url)/metrics  with header  Authorization: Bearer $t" ;;
    disable) env_set ELECTRU_METRICS_TOKEN ""; systemctl restart "$SERVICE"; ok "Metrics disabled." ;;
    status)  [[ -n "$(env_get ELECTRU_METRICS_TOKEN)" ]] && echo "enabled: $(panel_url)/metrics" || echo "disabled (run: electruvpn metrics enable)" ;;
    *) die "Usage: electruvpn metrics [enable|disable|status]" ;;
  esac
}

# update <zip-file|https-url> [sha256]
# Runs the installer from the new release. install.sh keeps the previous runtime
# and rolls back automatically if the new service fails its health check.
cmd_update() {
  local src="${1:-}" want="${2:-}" work zip
  if [[ -z "$src" ]]; then
    [[ -t 0 ]] || die "Usage: electruvpn update <release.zip | https://url/release.zip> [sha256]"
    read -rp "Release ZIP path or HTTPS URL: " src || true
    [[ -n "$src" ]] || die "No release source provided."
    read -rp "Expected SHA-256 (optional, recommended): " want || true
  fi
  command -v unzip >/dev/null || apt-get install -y -qq unzip >/dev/null
  work="$(mktemp -d /tmp/electruvpn-update.XXXXXX)"
  trap 'rm -rf "$work"' RETURN
  if [[ "$src" =~ ^https:// ]]; then
    zip="$work/release.zip"
    info "Downloading $src ..."
    curl --fail --location --proto '=https' --tlsv1.2 --retry 3 --connect-timeout 20 --max-time 300 -o "$zip" "$src" || die "Download failed"
  else
    [[ -f "$src" ]] || die "File not found: $src"
    zip="$src"
  fi
  if [[ -n "$want" ]]; then
    [[ "$(sha256sum "$zip" | awk '{print $1}')" == "${want,,}" ]] || die "SHA-256 mismatch; refusing to update."
    ok "SHA-256 verified."
  else
    warn "No SHA-256 given: archive integrity is not verified against a published hash."
  fi
  unzip -tq "$zip" >/dev/null || die "Archive is corrupt."
  # reject path traversal / absolute paths
  if unzip -Z1 "$zip" | grep -qE '(^/|(^|/)\.\.(/|$))'; then die "Archive contains unsafe paths."; fi
  unzip -q "$zip" -d "$work/src"
  local root="$work/src"
  [[ -f "$root/install.sh" ]] || root="$(dirname "$(find "$work/src" -maxdepth 2 -name install.sh | head -n1)")"
  [[ -f "$root/install.sh" && -f "$root/go.mod" ]] || die "install.sh/go.mod not found in the archive."
  if ! ( cmd_backup ); then
    if [[ -t 0 ]]; then
      local proceed=""
      warn "A database backup could not be created. Updating without a database backup is risky."
      read -rp "Continue anyway? Type YES to proceed: " proceed || true
      [[ "$proceed" == "YES" ]] || die "Update cancelled; database backup was not created."
    else
      die "Update stopped because database backup failed. Fix the database configuration or run interactively and explicitly confirm proceeding without a backup."
    fi
  fi
  # install.sh disables ufw after every install/update (client configs need it off),
  # unless ELECTRU_KEEP_UFW=1 is exported. The previous state is passed along only
  # for that opt-in mode.
  local ufw_was_active=unknown
  if command -v ufw >/dev/null 2>&1; then
    if ufw status 2>/dev/null | grep -q '^Status: active'; then
      ufw_was_active=1
    else
      ufw_was_active=0
    fi
  fi
  info "Running the installer from the new release (existing config is kept)..."
  if [[ "$ufw_was_active" == "0" ]]; then
    ELECTRU_UPDATE_MODE=1 ELECTRU_UFW_WAS_ACTIVE=0 \
      bash -c 'cd "$1" && bash install.sh -y' _ "$root"
  elif [[ "$ufw_was_active" == "1" ]]; then
    ELECTRU_UPDATE_MODE=1 ELECTRU_UFW_WAS_ACTIVE=1 \
      bash -c 'cd "$1" && bash install.sh -y' _ "$root"
  else
    ELECTRU_UPDATE_MODE=1 \
      bash -c 'cd "$1" && bash install.sh -y' _ "$root"
  fi
  ok "Update finished."
}

cmd_help() {
  cat <<H
${BOLD}electruvpn${RESET} -- ELECTRUVPN server management

  electruvpn                 interactive menu
  electruvpn status          service + panel info
  electruvpn url             print the panel URL
  electruvpn start|stop|restart
  electruvpn logs [N]        follow service logs
  electruvpn port <port>     change the panel port (updates ufw/fail2ban)
  electruvpn path [random|/p] change the hidden base path
  electruvpn resetpw [pass]  reset the admin password (PostgreSQL/SQLite)
  electruvpn backup          create a manual database backup
  electruvpn geo [status|update]  Iran-aware geoip/geosite data
  electruvpn metrics [enable|disable|status]  Prometheus endpoint
  electruvpn update [zip|url] [sha256]  upgrade from a release archive (interactive if omitted)
H
}

menu() {
  while true; do
    echo; echo "${BOLD}ELECTRUVPN${RESET}"
    echo " 1) Status      2) Restart    3) Logs        4) Panel URL"
    echo " 5) Change port 6) Change path 7) Reset admin password"
    echo " 8) Backup      9) Update geo data 10) Update panel 0) Exit"
    read -rp "Choose: " c || exit 0
    case "$c" in
      1) cmd_status ;; 2) systemctl restart "$SERVICE" && ok restarted ;;
      3) cmd_logs 50 ;; 4) cmd_url ;;
      5) read -rp "New port: " v; cmd_port "$v" ;;
      6) read -rp "New base path (or 'random'): " v; cmd_path "$v" ;;
      7) cmd_resetpw ;; 8) cmd_backup ;; 9) cmd_geo update ;; 10) cmd_update ;;
      0) exit 0 ;; *) warn "Invalid choice" ;;
    esac
  done
}

case "${1:-menu}" in
  menu) menu ;;
  status) cmd_status ;; url) cmd_url ;;
  start|stop|restart) systemctl "$1" "$SERVICE" && ok "$1 done" ;;
  logs) shift; cmd_logs "${1:-100}" ;;
  port) shift; cmd_port "${1:-}" ;;
  path) shift; cmd_path "${1:-}" ;;
  resetpw) shift; cmd_resetpw "${1:-}" ;;
  backup) cmd_backup ;;
  geo) shift; cmd_geo "$@" ;;
  metrics) shift; cmd_metrics "${1:-status}" ;;
  update) shift; cmd_update "${1:-}" "${2:-}" ;;
  help|-h|--help) cmd_help ;;
  *) cmd_help; exit 1 ;;
esac
