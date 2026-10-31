#!/usr/bin/env bash
#
# ElectruVPN — install every OS prerequisite the panel needs.
# Sourced or executed by install.sh (root). Safe to re-run.
#
# Covers: apt packages, PostgreSQL  (server+client), build toolchain for Go/CGO,
# network tools, and optional helpers used by backup/migrate scripts.
#
set -euo pipefail

: "${BOLD:=}"; : "${RED:=}"; : "${GREEN:=}"; : "${YELLOW:=}"; : "${CYAN:=}"; : "${RESET:=}"
info() { echo -e "${CYAN}➜${RESET} $*"; }
ok()   { echo -e "${GREEN}✓${RESET} $*"; }
warn() { echo -e "${YELLOW}!${RESET} $*"; }
fail() { echo -e "${RED}✗ $*${RESET}"; exit 1; }

[[ ${EUID:-$(id -u)} -eq 0 ]] || fail "install-prerequisites.sh must run as root."

command -v apt-get >/dev/null 2>&1 || fail "Only Debian/Ubuntu (apt-get) is supported for automatic prerequisites."

export DEBIAN_FRONTEND=noninteractive
APT_OPTS=(-o DPkg::Lock::Timeout=300 -o Acquire::Retries=5)

info "Updating apt package index..."
apt-get "${APT_OPTS[@]}" update -y -qq

# Core runtime + build (Go builds with CGO for sqlite driver and some crypto).
PKGS=(
  ca-certificates
  curl
  wget
  unzip
  tar
  gzip
  git
  openssl
  gnupg
  lsb-release
  build-essential
  pkg-config
  gcc
  g++
  make
  libsqlite3-dev
  # networking / ops
  iproute2
  ufw
  fail2ban
  wireguard-tools
  # python helpers (geo, import, selftests)
  python3
  python3-venv
  python3-pip
  python3-systemd
  # PostgreSQL (server + client for pg_dump backups)
  postgresql
  postgresql-contrib
  postgresql-client
  # utilities
  jq
  sqlite3
)

info "Installing system packages (${#PKGS[@]} packages)..."
apt-get "${APT_OPTS[@]}" install -y -qq "${PKGS[@]}"
ok "System packages installed."

# ---------- PostgreSQL: enable + create panel role/db ----------
install_postgres_role() {
  local db_user="${ELECTRU_PG_USER:-electruvpn}"
  local db_name="${ELECTRU_PG_DB:-electruvpn}"
  local db_pass="${ELECTRU_PG_PASSWORD:-}"
  local conf_dir="${CONFIG_DIR:-/etc/electruvpn}"
  local pass_file="${conf_dir}/.pg_password"

  systemctl enable postgresql >/dev/null 2>&1 || true
  systemctl start postgresql >/dev/null 2>&1 || systemctl restart postgresql >/dev/null 2>&1 || true

  # Wait until postgres accepts connections (up to ~30s).
  local i=0
  until sudo -u postgres psql -Atqc "SELECT 1" >/dev/null 2>&1; do
    i=$((i + 1))
    if [[ $i -gt 30 ]]; then
      warn "PostgreSQL did not become ready in time; panel can still use SQLite fallback."
      return 1
    fi
    sleep 1
  done
  ok "PostgreSQL is running."

  mkdir -p "$conf_dir"
  chmod 750 "$conf_dir" 2>/dev/null || true

  if [[ -z "$db_pass" && -f "$pass_file" ]]; then
    db_pass="$(tr -d '\n\r' < "$pass_file")"
  fi
  if [[ -z "$db_pass" ]]; then
    db_pass="$(openssl rand -hex 24)"
    umask 077
    printf '%s' "$db_pass" > "$pass_file"
    chmod 600 "$pass_file"
  fi

  # Role
  if ! sudo -u postgres psql -Atqc "SELECT 1 FROM pg_roles WHERE rolname='${db_user}'" | grep -q 1; then
    sudo -u postgres psql -v ON_ERROR_STOP=1 -c "CREATE USER ${db_user} WITH PASSWORD '${db_pass}';" >/dev/null
    ok "Created PostgreSQL role: ${db_user}"
  else
    # Keep password in sync with file so reinstalls stay consistent.
    sudo -u postgres psql -v ON_ERROR_STOP=1 -c "ALTER USER ${db_user} WITH PASSWORD '${db_pass}';" >/dev/null
    ok "PostgreSQL role already exists: ${db_user} (password refreshed)"
  fi

  # Database
  if ! sudo -u postgres psql -Atqc "SELECT 1 FROM pg_database WHERE datname='${db_name}'" | grep -q 1; then
    sudo -u postgres psql -v ON_ERROR_STOP=1 -c "CREATE DATABASE ${db_name} OWNER ${db_user};" >/dev/null
    ok "Created database: ${db_name}"
  else
    ok "Database already exists: ${db_name}"
  fi

  sudo -u postgres psql -v ON_ERROR_STOP=1 -d "$db_name" -c "GRANT ALL ON SCHEMA public TO ${db_user};" >/dev/null 2>&1 || true

  # Export for parent install.sh
  export ELECTRU_PG_USER="$db_user"
  export ELECTRU_PG_DB="$db_name"
  export ELECTRU_PG_PASSWORD="$db_pass"
  export ELECTRU_DB_DSN="host=127.0.0.1 user=${db_user} password=${db_pass} dbname=${db_name} port=5432 sslmode=disable TimeZone=UTC"
  ok "PostgreSQL DSN ready for ElectruVPN."
  return 0
}

if [[ "${ELECTRU_SKIP_POSTGRES:-0}" != "1" ]]; then
  info "Configuring PostgreSQL for ElectruVPN..."
  install_postgres_role || warn "PostgreSQL auto-setup incomplete; set ELECTRU_DB_DSN manually or use SQLite."
else
  warn "ELECTRU_SKIP_POSTGRES=1 — skipping PostgreSQL setup."
fi

# Quick visibility of versions (helps support / debugging)
info "Prerequisite versions:"
echo "  go will be installed by install.sh (pinned go1.27.1)"
command -v psql >/dev/null && echo "  psql: $(psql --version | head -1)" || true
command -v gcc >/dev/null && echo "  gcc:  $(gcc --version | head -1)" || true
command -v git >/dev/null && echo "  git:  $(git --version)" || true
command -v python3 >/dev/null && echo "  python3: $(python3 --version 2>&1)" || true
command -v jq >/dev/null && echo "  jq:   $(jq --version 2>&1)" || true

ok "All OS prerequisites processed."
