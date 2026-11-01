#!/usr/bin/env bash
#
# ELECTRUVPN BETA1 one-shot installer.
#
# Run this script from the extracted "electruvpn" project folder you
# uploaded to the server (or invoke it by absolute/relative path):
#
#   cd electruvpn
#   sudo bash install.sh          # asks for a yes/no confirmation first
#   sudo bash install.sh -y       # non-interactive, skips the confirmation
#   sudo bash install.sh --no-confirm   # no "Proceed?" question, wizard stays interactive
#
# It installs OS packages + PostgreSQL, Go, Xray-core and sing-box if missing,
# builds the panel,
# writes /etc/electruvpn/electruvpn.env with a freshly generated JWT
# secret, installs it as a systemd service, and opens the panel port in
# ufw. Safe to re-run: it skips steps that are already done and never
# overwrites an existing electruvpn.env.
#
set -euo pipefail

BOLD="\033[1m"; RED="\033[31m"; GREEN="\033[32m"; YELLOW="\033[33m"; CYAN="\033[36m"; RESET="\033[0m"
info() { echo -e "${CYAN}➜${RESET} $*"; }
ok()   { echo -e "${GREEN}✓${RESET} $*"; }
warn() { echo -e "${YELLOW}!${RESET} $*"; }
fail() { echo -e "${RED}✗ $*${RESET}"; exit 1; }

ASSUME_YES=0
SKIP_CONFIRM=0
for _arg in "$@"; do
  case "$_arg" in
    -y|--yes) ASSUME_YES=1 ;;
    # Skip only the "Proceed?" question; the certificate wizard stays interactive.
    --no-confirm) SKIP_CONFIRM=1 ;;
    *) fail "Unknown argument: ${_arg}. Use -y/--yes (unattended) or --no-confirm." ;;
  esac
done

# ask VAR "prompt" "preset": interactive prompt on a terminal; with -y or without a
# TTY it uses the preset (taken from an ELECTRU_CERT_* environment variable) instead
# of aborting silently when `read` hits EOF under `set -e`.
ask() {
  local __var="$1" __prompt="$2" __preset="${3:-}"
  if [[ "$ASSUME_YES" == 1 || ! -t 0 ]]; then
    printf -v "$__var" '%s' "$__preset"
  else
    read -rp "$__prompt" "$__var" || printf -v "$__var" '%s' "$__preset"
  fi
}

# Read the last value of KEY from the existing env file (empty if absent).
env_get() { grep -oP "^$1=\K.*" "${CONFIG_DIR}/electruvpn.env" 2>/dev/null | tail -n1 || true; }

# One cleanup trap for every temp dir (later `trap ... EXIT` calls used to replace
# each other and could leak the Go download / build directory on failure).
GO_TMP_DIR=""; BUILD_DIR=""; STAGE_DIR=""
cleanup_all() { rm -rf "${GO_TMP_DIR:-}" "${BUILD_DIR:-}" "${STAGE_DIR:-}"; }
trap cleanup_all EXIT

# Return success when a TCP port is currently bound by a local listener.
# This helper is used before platform setup, so keep it dependency-light.
port_is_listening() {
  local port="$1"
  [[ "$port" =~ ^[0-9]+$ ]] || return 1
  command -v ss >/dev/null 2>&1 || return 1
  ss -ltnH 2>/dev/null | awk -v p=":${port}" '$4 ~ (p"$") {found=1; exit} END {exit(found ? 0 : 1)}'
}

[[ $EUID -eq 0 ]] || fail "Please run as root, e.g.: sudo bash install.sh"

# Fresh-install preflight: fail early instead of leaving a half-installed panel.
[[ -r /etc/os-release ]] || fail "Cannot detect Linux distribution (/etc/os-release missing)."
. /etc/os-release
case "${ID:-}" in
  ubuntu|debian) ;;
  *) fail "Unsupported distribution: ${PRETTY_NAME:-unknown}. Supported: Ubuntu/Debian with systemd." ;;
esac
command -v systemctl >/dev/null 2>&1 || fail "systemd/systemctl is required on a fresh Ubuntu/Debian server."
command -v apt-get >/dev/null 2>&1 || fail "apt-get is required on a fresh Ubuntu/Debian server."
command -v sha256sum >/dev/null 2>&1 || fail "sha256sum is required."
command -v tar >/dev/null 2>&1 || fail "tar is required."
command -v gzip >/dev/null 2>&1 || fail "gzip is required."

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$SCRIPT_DIR"
cd "$PROJECT_DIR"

# Refuse to build from a suspiciously small/incomplete extraction.
[[ -f "$PROJECT_DIR/go.mod" && -f "$PROJECT_DIR/cmd/server/main.go" && -f "$PROJECT_DIR/web/index.html" ]] || fail "Incomplete project extraction. Re-extract the complete release ZIP into a clean directory."
AVAILABLE_KB=$(df -Pk /opt 2>/dev/null | awk 'NR==2 {print $4}')
[[ "${AVAILABLE_KB:-0}" =~ ^[0-9]+$ ]] && (( AVAILABLE_KB >= 1048576 )) || fail "At least 1 GiB free space is required on /opt."

# Every path/port below can be overridden by exporting the variable before
# running the script, e.g.:  PANEL_PORT=8443 sudo -E bash install.sh
PANEL_PORT="${PANEL_PORT:-2087}"
[[ "$PANEL_PORT" =~ ^[0-9]+$ ]] && (( PANEL_PORT >= 1 && PANEL_PORT <= 65535 )) || fail "Invalid PANEL_PORT=$PANEL_PORT. Choose a TCP port from 1 to 65535."
PUBLIC_HOST="${PUBLIC_HOST:-}"
ELECTRU_BASE_PATH="${ELECTRU_BASE_PATH:-}"   # generated after the apt step (needs openssl)
INSTALL_DIR="${INSTALL_DIR:-/opt/electruvpn}"
CONFIG_DIR="${CONFIG_DIR:-/etc/electruvpn}"
PANEL_TLS_ENABLED="${PANEL_TLS_ENABLED:-0}"
[[ "$PANEL_TLS_ENABLED" == "0" || "$PANEL_TLS_ENABLED" == "1" ]] || fail "PANEL_TLS_ENABLED must be 0 or 1 (got: $PANEL_TLS_ENABLED)."
PANEL_TLS_CERT="${PANEL_TLS_CERT:-}"
PANEL_TLS_KEY="${PANEL_TLS_KEY:-}"
PANEL_TLS_HOST="${PANEL_TLS_HOST:-}"
SUBSCRIPTION_BASE_URL="${SUBSCRIPTION_BASE_URL:-}"
SUBSCRIPTION_DOMAIN="${SUBSCRIPTION_DOMAIN:-}"
CERTBOT_VENV="${CERTBOT_VENV:-$CONFIG_DIR/certbot-venv}"

echo -e "${BOLD}ELECTRUVPN BETA1 installer${RESET}"
echo "  Project dir : $PROJECT_DIR"
echo "  Install dir : $INSTALL_DIR"
echo "  Config dir  : $CONFIG_DIR"
echo "  Panel port  : $PANEL_PORT"
echo

if [[ "$ASSUME_YES" != 1 && "$SKIP_CONFIRM" != 1 ]]; then
  [[ -t 0 ]] || fail "No interactive terminal detected. Re-run with: sudo bash install.sh -y"
  read -rp "Proceed with installation? [Y/n] " REPLY || REPLY="n"
  REPLY="${REPLY:-Y}"
  [[ "$REPLY" =~ ^[Yy]$ ]] || { echo "Aborted."; exit 0; }
fi

# ---------- 1. system packages + PostgreSQL ----------
# Full prerequisite set (apt packages, build tools, PostgreSQL role/db).
# Re-runnable; skips nothing harmful. Override with ELECTRU_SKIP_POSTGRES=1 if needed.
info "Installing all system prerequisites (packages + PostgreSQL)..."
export CONFIG_DIR
# shellcheck source=scripts/install-prerequisites.sh
source "$PROJECT_DIR/scripts/install-prerequisites.sh"
ok "System prerequisites ready."

# Never let an externally supplied TLS flag create a service that can only
# crash-loop because its certificate/key paths are empty or unreadable.
if [[ "$PANEL_TLS_ENABLED" == "1" ]]; then
  [[ -n "$PANEL_TLS_CERT" && -n "$PANEL_TLS_KEY" ]] || fail "PANEL_TLS_ENABLED=1 requires both PANEL_TLS_CERT and PANEL_TLS_KEY, or unset PANEL_TLS_ENABLED and use the certificate wizard."
  [[ -s "$PANEL_TLS_CERT" ]] || fail "TLS certificate file is missing or empty: $PANEL_TLS_CERT"
  [[ -s "$PANEL_TLS_KEY" ]] || fail "TLS private key file is missing or empty: $PANEL_TLS_KEY"
fi

if [[ -z "$ELECTRU_BASE_PATH" ]]; then
  ELECTRU_BASE_PATH="/panel-$(openssl rand -hex 8)"
fi

# ---------- 1.5. HTTPS certificate wizard ----------
# This wizard is intentionally interactive on fresh installs. It can issue a
# publicly trusted Let's Encrypt certificate for a domain or public IP.
# Port 80 is required for the standalone HTTP-01 validation used here.
if [[ ! -f "$CONFIG_DIR/electruvpn.env" ]]; then
  echo
  echo -e "${BOLD}HTTPS certificate setup${RESET}"
  echo "  1) Get one certificate for panel domain + separate subscription domain (recommended)"
  echo "  2) Get a certificate for this server IP"
  echo "  3) Skip certificate setup"
  echo "  For option 1, both domain A records must point to this server and TCP/80 must be reachable."
  CERT_CHOICE_DEFAULT=3
  [[ "$ASSUME_YES" != 1 ]] && CERT_CHOICE_DEFAULT=1
  ask CERT_CHOICE "Choose [1/2/3]: " "${ELECTRU_CERT_CHOICE:-$CERT_CHOICE_DEFAULT}"
  CERT_CHOICE="${CERT_CHOICE:-3}"

  CERT_IDENTITY=""
  CERT_KIND=""
  case "$CERT_CHOICE" in
    1)
      ask CERT_IDENTITY "Enter the panel domain (example: panel.example.com): " "${ELECTRU_CERT_DOMAIN:-}"
      [[ -n "$CERT_IDENTITY" ]] || fail "No panel domain given (non-interactive: export ELECTRU_CERT_DOMAIN=panel.example.com)."
      [[ "$CERT_IDENTITY" =~ ^[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?$ ]] || fail "Invalid panel domain name: $CERT_IDENTITY"
      ask SUBSCRIPTION_DOMAIN "Enter the separate subscription domain (example: sub.example.com): " "${ELECTRU_SUBSCRIPTION_DOMAIN:-}"
      [[ -n "$SUBSCRIPTION_DOMAIN" ]] || fail "No subscription domain given (non-interactive: export ELECTRU_SUBSCRIPTION_DOMAIN=sub.example.com)."
      [[ "$SUBSCRIPTION_DOMAIN" =~ ^[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?$ ]] || fail "Invalid subscription domain name: $SUBSCRIPTION_DOMAIN"
      [[ "$CERT_IDENTITY" != "$SUBSCRIPTION_DOMAIN" ]] || fail "Panel and subscription domains must be different."
      # Do not publish an HTTPS subscription URL until a trusted certificate
      # has actually been issued for the subscription hostname.
      CERT_KIND="domain"
      ;;
    2)
      # Detect the server public IPv4 automatically; never ask the user to type it.
      SERVER_IP_PRE=""
      for IP_SERVICE in \
        "https://api.ipify.org" \
        "https://ifconfig.me/ip" \
        "https://icanhazip.com"; do
        SERVER_IP_PRE=$(curl -4 -fsSL --connect-timeout 5 --max-time 10 "$IP_SERVICE" 2>/dev/null | tr -d '[:space:]' || true)
        if [[ "$SERVER_IP_PRE" =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]]; then
          break
        fi
        SERVER_IP_PRE=""
      done
      if [[ -z "$SERVER_IP_PRE" ]]; then
        SERVER_IP_PRE=$(hostname -I 2>/dev/null | awk '{for(i=1;i<=NF;i++) if ($i !~ /:/ && $i !~ /^127\./) {print $i; exit}}')
      fi
      [[ -n "$SERVER_IP_PRE" ]] || fail "Could not automatically detect the server IP. Check outbound HTTPS/DNS connectivity."
      CERT_IDENTITY="$SERVER_IP_PRE"
      CERT_KIND="ip"
      ok "Detected server public IP: $CERT_IDENTITY"
      ;;
    3)
      info "Skipping certificate setup. The panel will use HTTP until a certificate is configured."
      ;;
    *)
      fail "Invalid certificate choice. Please choose 1, 2, or 3."
      ;;
  esac

  if [[ -n "$CERT_IDENTITY" ]]; then
    if ss -ltnH 2>/dev/null | awk '{print $4}' | grep -Eq '(^|:)80$|\]:80$'; then
      warn "Port 80 is already occupied on this server."
    fi
    echo
    ask PORT80_OK "Is TCP port 80 open/reachable for ACME validation? [y/N]: " "${ELECTRU_CERT_PORT80_OK:-N}"
    PORT80_OK="${PORT80_OK:-N}"
    if [[ "$PORT80_OK" =~ ^[Yy]$ ]]; then
      PANEL_TLS_HOST="$CERT_IDENTITY"
      PANEL_TLS_ENABLED=1
      info "Preparing Let's Encrypt certificate for $CERT_IDENTITY..."
      ufw allow 80/tcp >/dev/null 2>&1 || true

      if [[ ! -x "$CERTBOT_VENV/bin/certbot" ]]; then
        info "Installing a current Certbot in $CERTBOT_VENV..."
        mkdir -p "$CONFIG_DIR"
        python3 -m venv "$CERTBOT_VENV"
        "$CERTBOT_VENV/bin/pip" install --disable-pip-version-check --no-cache-dir -q --upgrade pip certbot
      fi
      CERTBOT="$CERTBOT_VENV/bin/certbot"
      CERTBOT_ARGS=(certonly --standalone --non-interactive --agree-tos --register-unsafely-without-email)
      if [[ "$CERT_KIND" == "domain" ]]; then
        CERTBOT_ARGS+=(--domain "$CERT_IDENTITY" --domain "$SUBSCRIPTION_DOMAIN")
      else
        CERTBOT_ARGS+=(--preferred-profile shortlived --ip-address "$CERT_IDENTITY")
      fi
      if [[ "$CERT_KIND" == "domain" ]]; then
        "$CERTBOT" "${CERTBOT_ARGS[@]}" || fail "Let's Encrypt certificate issuance failed for $CERT_IDENTITY. Confirm DNS points to this server and TCP/80 is reachable."
      else
        "$CERTBOT" "${CERTBOT_ARGS[@]}" || fail "Let's Encrypt IP certificate issuance failed for $CERT_IDENTITY. Confirm this is the server's public IP and TCP/80 is reachable."
      fi
      CERT_DIR="/etc/letsencrypt/live/$CERT_IDENTITY"
      PANEL_TLS_CERT="$CERT_DIR/fullchain.pem"
      PANEL_TLS_KEY="$CERT_DIR/privkey.pem"
      [[ -s "$PANEL_TLS_CERT" && -s "$PANEL_TLS_KEY" ]] || fail "Certificate files were not created by Certbot."
      ok "Certificate issued: $PANEL_TLS_CERT"
      if [[ "$CERT_KIND" == "domain" ]]; then
        SUBSCRIPTION_BASE_URL="https://$SUBSCRIPTION_DOMAIN"
        # VPN links default to IPv4 so a later IP change can be reflected in
        # refreshed subscriptions. The stable subscription URL remains a domain.
        DETECTED_PUBLIC_V4=""
        for IP_SERVICE in "https://api.ipify.org" "https://ifconfig.me/ip" "https://icanhazip.com"; do
          DETECTED_PUBLIC_V4=$(curl -4 -fsSL --connect-timeout 5 --max-time 10 "$IP_SERVICE" 2>/dev/null | tr -d '[:space:]' || true)
          if [[ "$DETECTED_PUBLIC_V4" =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]]; then break; fi
          DETECTED_PUBLIC_V4=""
        done
        if [[ -n "$DETECTED_PUBLIC_V4" ]]; then
          PUBLIC_HOST="${PUBLIC_HOST:-$DETECTED_PUBLIC_V4}"
          PUBLIC_HOST_V4="${PUBLIC_HOST_V4:-$DETECTED_PUBLIC_V4}"
        else
          warn "Could not detect public IPv4 during installation; automatic detection will run at panel startup."
        fi
      fi

      echo
      if [[ "$CERT_KIND" == "domain" ]]; then
        # The chosen mode promises HTTPS for both the panel and the separate
        # subscription hostname, so attach the SAN certificate automatically.
        SET_PANEL_TLS="Y"
        info "Attaching the domain certificate to the panel automatically."
      else
        ask SET_PANEL_TLS "Set this certificate automatically on the ELECTRUVPN BETA1 panel? [y/N]: " "${ELECTRU_CERT_ATTACH:-Y}"
        SET_PANEL_TLS="${SET_PANEL_TLS:-N}"
      fi
      if [[ "$SET_PANEL_TLS" =~ ^[Yy]$ ]]; then
        PANEL_TLS_ENABLED=1
        if ss -ltnH 2>/dev/null | awk '{print $4}' | grep -Eq '(^|:)443$|\]:443$'; then
          warn "TCP 443 is already occupied; keeping panel port $PANEL_PORT and enabling HTTPS there."
        else
          PANEL_PORT=443
        fi
        if [[ "$CERT_KIND" == "domain" && "$PANEL_PORT" != "443" ]]; then
          SUBSCRIPTION_BASE_URL="https://${SUBSCRIPTION_DOMAIN}:${PANEL_PORT}"
        fi
        ok "Panel TLS will be enabled automatically."
      else
        PANEL_TLS_ENABLED=0
        info "Certificate kept on disk but not attached to the panel."
      fi
    else
      info "Skipping certificate issuance because port 80 was not confirmed open."
    fi
  fi
fi

# Re-check after the certificate wizard because TLS setup may switch the panel
# to :443. On a fresh host another service owning the chosen port is fatal.
if port_is_listening "$PANEL_PORT" && ! systemctl is-active --quiet electruvpn 2>/dev/null; then
  fail "Panel port $PANEL_PORT is already in use by another process. Stop it or choose another port."
fi

# ---------- 2. platform / Go toolchain ----------
# Go is installed ONLINE from the official Go distribution only.  The release
# ZIP deliberately does not contain a Go binary.  We pin the exact toolchain
# and verify its SHA-256 before extracting it.
source "$PROJECT_DIR/scripts/platform.sh"
if ! electru_detect_platform "$(uname -m)"; then
  fail "Unsupported Linux architecture: $(uname -m). Supported: x86_64/amd64, x86/386, ARM64/aarch64, ARMv7, ARMv6."
fi
ok "Detected platform: $PLATFORM_LABEL (uname -m: $(uname -m))"

GO_VERSION="${GO_VERSION:-go1.27.1}"
[[ "$GO_VERSION" == "go1.27.1" ]] || fail "This release is pinned to Go 1.27.1. Refusing GO_VERSION=$GO_VERSION."
case "$GO_TARBALL_ARCH" in
  amd64)
    GO_SHA256="63d339f0da5ab53635a56f2490a7984dfe12dfcff22ad749f63edaf590168445" ;;
  arm64)
    GO_SHA256="3450b45a3f9ee8568792736a5c5e70a1f2e9b36c35a8f74958c03e51d7d92bec" ;;
  386)
    GO_SHA256="3b72028095439d2bc0ce84e271cc70328a878d879020c5721eaa46df5f72fbc0" ;;
  armv6l)
    GO_SHA256="44893f200fb034791d4188df9fc9b9e73eadbb5fceafd5166703f0b9bab73fc2" ;;
  *)
    fail "No pinned Go 1.27.1 checksum is configured for archive architecture: $GO_TARBALL_ARCH" ;;
esac
GO_TARBALL="${GO_VERSION}.linux-${GO_TARBALL_ARCH}.tar.gz"
GO_URL="https://go.dev/dl/${GO_TARBALL}"
GO_TOOLCHAIN_ROOT="${GO_TOOLCHAIN_ROOT:-/opt/electruvpn-toolchain}"
GO_TOOLCHAIN_DIR="$GO_TOOLCHAIN_ROOT/$GO_VERSION"
GO_BIN="$GO_TOOLCHAIN_DIR/go/bin/go"

# Exact pinned Go is used even if the host has an older/newer system Go.
# This keeps fresh-server builds reproducible and avoids accidental toolchain
# switching. Existing exact Go is reused only when it is already in our
# dedicated toolchain location.
if [[ -x "$GO_BIN" ]] && "$GO_BIN" version 2>/dev/null | grep -q "^go version ${GO_VERSION} "; then
  ok "Pinned Go already installed: $($GO_BIN version)"
else
  command -v curl >/dev/null 2>&1 || fail "curl is required to download Go from go.dev."
  command -v tar >/dev/null 2>&1 || fail "tar is required to install Go."
  command -v gzip >/dev/null 2>&1 || fail "gzip is required to unpack Go."
  command -v sha256sum >/dev/null 2>&1 || fail "sha256sum is required to verify Go."
  info "Downloading official Go ${GO_VERSION} (${GO_TARBALL_ARCH}) from ${GO_URL}..."
  GO_TMP_DIR="$(mktemp -d /tmp/electruvpn-go.XXXXXX)"
  GO_TMP="$GO_TMP_DIR/$GO_TARBALL"
  curl --fail --show-error --location --proto '=https' --tlsv1.2 \
    --retry 5 --retry-delay 2 --retry-all-errors --connect-timeout 20 --max-time 600 \
    -o "$GO_TMP" "$GO_URL" || fail "Could not download Go from the official go.dev URL. Check DNS/HTTPS connectivity."
  echo "${GO_SHA256}  ${GO_TMP}" | sha256sum -c - || fail "Go SHA-256 verification failed. Refusing to install an unverified toolchain."
  tar -tzf "$GO_TMP" >/dev/null 2>&1 || fail "Downloaded Go archive is invalid or incomplete."
  mkdir -p "$GO_TOOLCHAIN_ROOT"
  GO_STAGE="$GO_TOOLCHAIN_ROOT/.${GO_VERSION}.staging.$$"
  rm -rf "$GO_STAGE"
  mkdir -p "$GO_STAGE"
  tar -xzf "$GO_TMP" -C "$GO_STAGE"
  [[ -x "$GO_STAGE/go/bin/go" ]] || fail "Go archive did not contain the expected go/bin/go binary."
  "$GO_STAGE/go/bin/go" version | grep -q "^go version ${GO_VERSION} " || fail "Downloaded Go reports an unexpected version."
  rm -rf "$GO_TOOLCHAIN_DIR"
  mv "$GO_STAGE" "$GO_TOOLCHAIN_DIR"
  rm -rf "$GO_TMP_DIR"
  ok "Installed verified Go ${GO_VERSION} at ${GO_TOOLCHAIN_DIR}."
fi

export PATH="$GO_TOOLCHAIN_DIR/go/bin:$PATH"
export GOTOOLCHAIN=local
export GOPATH="${GOPATH:-/var/lib/electruvpn/go}"
export GOMODCACHE="${GOMODCACHE:-$GOPATH/pkg/mod}"
mkdir -p "$GOPATH" "$GOMODCACHE"
chmod 700 "$GOPATH"
cat >/etc/profile.d/electruvpn-go.sh <<EOF
export PATH="${GO_TOOLCHAIN_DIR}/go/bin:\$PATH"
export GOTOOLCHAIN=local
export GOPATH="/var/lib/electruvpn/go"
export GOMODCACHE="/var/lib/electruvpn/go/pkg/mod"
EOF
chmod 644 /etc/profile.d/electruvpn-go.sh
hash -r
[[ -x "$GO_BIN" ]] || fail "Pinned Go binary is missing: $GO_BIN"
"$GO_BIN" version | grep -q "^go version ${GO_VERSION} " || fail "Pinned Go verification failed."
ok "Go verified: $($GO_BIN version)"

# ---------- 3. Xray-core ----------
if [[ -x /usr/local/bin/xray ]]; then
  ok "Xray-core already installed, skipping."
else
  info "Installing Xray-core (official XTLS installer)..."
  XRAY_INSTALLER="$(mktemp /tmp/electruvpn-xray-install.XXXXXX.sh)"
  if ! curl --retry 5 --retry-delay 2 --retry-all-errors -fsSL --connect-timeout 15 --max-time 120 \
       -o "$XRAY_INSTALLER" https://github.com/XTLS/Xray-install/raw/main/install-release.sh || [[ ! -s "$XRAY_INSTALLER" ]]; then
    rm -f "$XRAY_INSTALLER"; fail "Could not download the official Xray installer. Check DNS/HTTPS access to github.com."
  fi
  bash "$XRAY_INSTALLER" install || { rm -f "$XRAY_INSTALLER"; fail "Xray-core installation failed (see output above)."; }
  rm -f "$XRAY_INSTALLER"
  ok "Xray-core installed."
fi
command -v /usr/local/bin/xray >/dev/null 2>&1 || fail "Xray-core binary was not installed at /usr/local/bin/xray."

# ---------- 3.5. sing-box ----------
if [[ -x /usr/local/bin/sing-box ]]; then
  ok "sing-box already installed, skipping."
else
  info "Installing sing-box for ${PLATFORM_LABEL}..."
  SB_API_JSON="$(mktemp /tmp/electruvpn-singbox-api.XXXXXX.json)"
  if ! curl --retry 8 --retry-delay 2 --retry-all-errors -fsSL --connect-timeout 15 --max-time 120 https://api.github.com/repos/SagerNet/sing-box/releases/latest -o "$SB_API_JSON"; then
    : > "$SB_API_JSON"   # API unreachable/rate-limited: fall back to the release redirect below
  fi
  SB_TAG=""; SB_NAME=""; SB_URL=""; SB_DIGEST=""
  read -r SB_TAG SB_NAME SB_URL SB_DIGEST < <(python3 - "$SB_API_JSON" "$SBARCH" 2>/dev/null <<'PYBOX'
import json, re, sys
p, arch = sys.argv[1:]
data=json.load(open(p, encoding='utf-8'))
pattern=re.compile(rf'^sing-box-.*-linux-{re.escape(arch)}\.tar\.gz$')
for a in data.get('assets', []):
    name=a.get('name','')
    if pattern.match(name):
        print(data.get('tag_name',''), name, a.get('browser_download_url',''), a.get('digest','') or '')
        raise SystemExit(0)
raise SystemExit(1)
PYBOX
  ) || { SB_TAG=""; SB_NAME=""; SB_URL=""; SB_DIGEST=""; }
  rm -f "$SB_API_JSON"
  if [[ -z "$SB_URL" ]]; then
    warn "GitHub API unavailable, rate-limited, or no matching asset; resolving the latest sing-box tag via the release redirect (no published digest to verify)."
    SB_TAG="$(curl -fsSIL -o /dev/null -w '%{url_effective}' --connect-timeout 15 --max-time 60 https://github.com/SagerNet/sing-box/releases/latest | sed 's#.*/tag/##')" || SB_TAG=""
    [[ "$SB_TAG" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.]+)?$ ]] || fail "Could not determine the latest sing-box release (GitHub unreachable or rate-limited). Retry in a few minutes."
    SB_NAME="sing-box-${SB_TAG#v}-linux-${SBARCH}.tar.gz"
    SB_URL="https://github.com/SagerNet/sing-box/releases/download/${SB_TAG}/${SB_NAME}"
    SB_DIGEST=""
  fi
  SB_TMP=$(mktemp /tmp/electruvpn-sing-box.XXXXXX.tar.gz)
  rm -rf /tmp/sing-box-extract; mkdir -p /tmp/sing-box-extract
  info "Downloading sing-box ${SB_TAG} (${SB_NAME})..."
  if ! curl --retry 8 --retry-delay 2 --retry-all-errors -fL --connect-timeout 15 --max-time 300 --output "$SB_TMP" "$SB_URL"; then rm -f "$SB_TMP"; fail "Could not download sing-box for ${PLATFORM_LABEL}."; fi
  [[ -s "$SB_TMP" ]] || { rm -f "$SB_TMP"; fail "Downloaded sing-box archive is empty."; }
  tar -tzf "$SB_TMP" >/dev/null 2>&1 || { rm -f "$SB_TMP"; fail "Downloaded sing-box archive is invalid or incomplete."; }
  if [[ "$SB_DIGEST" == sha256:* ]]; then
    EXPECTED_SHA="${SB_DIGEST#sha256:}"; ACTUAL_SHA="$(sha256sum "$SB_TMP" | awk '{print $1}')"
    [[ "$EXPECTED_SHA" == "$ACTUAL_SHA" ]] || { rm -f "$SB_TMP"; fail "sing-box SHA-256 verification failed."; }
  fi
  tar -C /tmp/sing-box-extract -xzf "$SB_TMP"
  SB_BIN=$(find /tmp/sing-box-extract -type f -name sing-box -perm -u+x | head -n1)
  [[ -n "$SB_BIN" ]] || { rm -f "$SB_TMP"; rm -rf /tmp/sing-box-extract; fail "sing-box binary was not found in release archive."; }
  install -m 0755 "$SB_BIN" /usr/local/bin/sing-box
  rm -f "$SB_TMP"; rm -rf /tmp/sing-box-extract
  /usr/local/bin/sing-box version >/dev/null 2>&1 || fail "Installed sing-box does not execute on ${PLATFORM_LABEL}."
  ok "sing-box ${SB_TAG} installed for ${SBARCH}."
fi


# ELECTRUVPN BETA1 runs its own xray-core subprocess with its own generated
# config, so the installer's default "xray" systemd service (with ITS OWN
# default config) must be disabled -- otherwise the two fight over ports.
if systemctl is-enabled --quiet xray 2>/dev/null || systemctl is-active --quiet xray 2>/dev/null; then
  info "Disabling the default xray systemd service (ELECTRUVPN BETA1 manages xray itself)..."
  systemctl disable --now xray >/dev/null 2>&1 || true
  ok "Default xray service disabled."
fi

# ---------- 4. build the panel ----------
cd "$PROJECT_DIR"
# Release-source guard: fail early if a partially overlaid/stale tree is being built.
if [[ -f RELEASE_SOURCE_MANIFEST.txt ]]; then
  info "Verifying release source manifest..."
  while read -r expected file; do
    [[ -n "$expected" && -n "$file" ]] || continue
    actual=$(sha256sum "$file" 2>/dev/null | awk '{print $1}')
    [[ "$actual" == "$expected" ]] || fail "Release source mismatch: $file (expected $expected, got ${actual:-missing}). Extract the complete release archive into a clean project directory."
  done < <(tail -n +4 RELEASE_SOURCE_MANIFEST.txt)
  ok "Release source manifest verified."
fi

# IMPORTANT: never build a Go module directly from /root or another location
# that may contain GOPATH/GOMODCACHE underneath the module tree. Go recursively
# discovers packages below the module root, so a cache such as /root/go/pkg/mod
# can be mistaken for source code and produce errors like
#   github.com/electruvpn/electruvpn/go/pkg/mod/...@vX: should not have @version
# Build from a clean temporary tree and keep the Go workspace outside it.
BUILD_DIR="$(mktemp -d /tmp/electruvpn-build.XXXXXX)"
BUILD_GO_ROOT="/var/lib/electruvpn/go"
mkdir -p "$BUILD_GO_ROOT"
chmod 700 "$BUILD_GO_ROOT"
info "Creating isolated build workspace: $BUILD_DIR"

tar -C "$PROJECT_DIR" \
  --exclude='./go' \
  --exclude='./go-*' \
  --exclude='./.electru-go' \
  --exclude='./pkg/mod' \
  --exclude='./node_modules' \
  -cf - . | tar -C "$BUILD_DIR" -xf -

[[ -f "$BUILD_DIR/go.mod" && -f "$BUILD_DIR/cmd/server/main.go" && -f "$BUILD_DIR/web/index.html" ]] || fail "Isolated build workspace is incomplete."
cd "$BUILD_DIR"

info "Downloading Go module dependencies into $BUILD_GO_ROOT..."
export GOTOOLCHAIN=local
export GOPATH="$BUILD_GO_ROOT"
export GOMODCACHE="$BUILD_GO_ROOT/pkg/mod"
GO_BUILD_VERSION=$(go version | grep -oE 'go[0-9]+\.[0-9]+(\.[0-9]+)?' | head -n1 || true)
[[ "$GO_BUILD_VERSION" == "$GO_VERSION" ]] || fail "Unexpected Go toolchain: ${GO_BUILD_VERSION:-unknown}. Required exactly: $GO_VERSION."
export GOPROXY="${GOPROXY:-https://proxy.golang.org,direct}"
export GOSUMDB="${GOSUMDB:-sum.golang.org}"
if ! go mod download; then
  fail "Go modules could not be downloaded. Check DNS/HTTPS access to proxy.golang.org and github.com, then rerun."
fi

# `go mod download` alone does not guarantee that go.sum contains every
# checksum required by the packages selected by the build (especially when
# the release intentionally omits a generated go.sum). Resolve the complete
# module graph in the isolated workspace before compiling. This also prevents
# the fresh-server failure where `go build` stops with `missing go.sum entry`.
info "Resolving and checksumming the complete Go module graph..."
if ! go mod tidy; then
  fail "Go module graph resolution failed. Check DNS/HTTPS access to proxy.golang.org and github.com, then rerun."
fi
[[ -s "$BUILD_DIR/go.sum" ]] || fail "Go module resolution did not produce go.sum. The dependency graph could not be resolved."
ok "Go modules downloaded, resolved, and checksummed."

info "Building electruvpn-server..."
if ! go build -trimpath -mod=readonly -o electruvpn-server ./cmd/server; then
  fail "electruvpn-server build failed after module resolution. The isolated workspace and generated go.sum were not sufficient to compile the release."
fi
ok "Build complete."

# ---------- 4.5 vendor frontend libraries ----------
# The panel's UI uses Tailwind/Alpine.js/Chart.js/QRCode.js. Fetching them
# from a public CDN in the ADMIN'S BROWSER is a common point of failure --
# if that network can't reach the CDN (blocked, censored, just flaky), the
# whole panel renders as a blank, unstyled, non-interactive page even
# though the server and API are perfectly healthy. This server already has
# working internet (it just used it for Go and Xray-core), so we download
# the exact same files here, once, and serve them ourselves -- the admin's
# browser then never needs to reach any third-party CDN at all.
VENDOR_DIR="$BUILD_DIR/web/assets/vendor"
mkdir -p "$VENDOR_DIR"
info "Vendoring frontend libraries for offline-safe serving..."
fetch_vendor() {
  local url="$1" out="$2"
  if [[ -s "$out" ]]; then
    return 0
  fi
  if curl -fsSL "$url" -o "$out"; then
    return 0
  fi
  rm -f "$out"
  warn "Could not download $url -- the panel UI will not work until this file exists at $out"
  return 1
}
# Vendor browser libraries locally. The installed panel never loads these from a CDN.
# Prefer GitHub/jsDelivr fallbacks because some servers can reach GitHub while a
# particular CDN hostname is unavailable. Downloads are written atomically.
fetch_vendor_any() {
  local out="$1"; shift
  local url tmp
  tmp="${out}.tmp.$$"
  rm -f "$tmp"
  for url in "$@"; do
    info "Fetching frontend library from ${url}"
    if curl --retry 5 --retry-delay 2 --connect-timeout 10 --max-time 120 -fsSL "$url" -o "$tmp" \
      && [[ -s "$tmp" ]] \
      && [[ $(wc -c < "$tmp") -gt 1000 ]]; then
      mv -f "$tmp" "$out"
      return 0
    fi
    rm -f "$tmp"
  done
  warn "Could not vendor frontend library: $out"
  return 1
}

fetch_vendor_any "$VENDOR_DIR/tailwind.js" \
  "https://cdn.jsdelivr.net/npm/@tailwindcss/browser@4" \
  "https://unpkg.com/@tailwindcss/browser@4" \
  "https://cdn.tailwindcss.com" || true
fetch_vendor_any "$VENDOR_DIR/alpine.min.js" \
  "https://raw.githubusercontent.com/alpinejs/alpine/v3.14.1/dist/cdn.min.js" \
  "https://cdn.jsdelivr.net/npm/alpinejs@3.14.1/dist/cdn.min.js" \
  "https://cdnjs.cloudflare.com/ajax/libs/alpinejs/3.14.1/cdn.min.js" || true
fetch_vendor_any "$VENDOR_DIR/chart.umd.min.js" \
  "https://raw.githubusercontent.com/chartjs/Chart.js/v4.4.3/dist/chart.umd.min.js" \
  "https://cdn.jsdelivr.net/npm/chart.js@4.4.3/dist/chart.umd.min.js" \
  "https://cdnjs.cloudflare.com/ajax/libs/Chart.js/4.4.3/chart.umd.min.js" || true
fetch_vendor_any "$VENDOR_DIR/qrcode.min.js" \
  "https://raw.githubusercontent.com/davidshimjs/qrcodejs/master/qrcode.min.js" \
  "https://cdn.jsdelivr.net/gh/davidshimjs/qrcodejs@master/qrcode.min.js" \
  "https://cdnjs.cloudflare.com/ajax/libs/qrcodejs/1.0.0/qrcode.min.js" || true

# Never continue with a partially vendored dashboard.
for required in \
  "$VENDOR_DIR/tailwind.js" \
  "$VENDOR_DIR/alpine.min.js" \
  "$VENDOR_DIR/chart.umd.min.js" \
  "$VENDOR_DIR/qrcode.min.js"; do
  [[ -s "$required" ]] || fail "Required frontend library missing: $required"
done
ok "Frontend libraries vendored locally -- the panel no longer depends on any external CDN."

if [[ "${CERT_KIND:-}" == "domain" ]]; then
  # Keep generated client links on the auto-detected IPv4; the panel/subscription
  # access endpoints use their stable DNS names instead.
  PUBLIC_HOST="${PUBLIC_HOST:-}"
else
  PUBLIC_HOST="${PUBLIC_HOST:-$PANEL_TLS_HOST}"
fi

# ---------- 5. config ----------
mkdir -p "$CONFIG_DIR"
if [[ ! -f "$CONFIG_DIR/electruvpn.env" ]]; then
  info "Writing $CONFIG_DIR/electruvpn.env with fresh production secrets..."
  JWT_SECRET=$(openssl rand -hex 32)
  INITIAL_ADMIN_PASSWORD=$(openssl rand -hex 18)
  INITIAL_ADMIN_PASSWORD_FILE="$CONFIG_DIR/initial-admin-password"
  printf "%s" "$INITIAL_ADMIN_PASSWORD" > "$INITIAL_ADMIN_PASSWORD_FILE"
  chmod 600 "$INITIAL_ADMIN_PASSWORD_FILE"
  cat > "$CONFIG_DIR/electruvpn.env" <<EOF
ELECTRU_PANEL_PORT=$PANEL_PORT
ELECTRU_PANEL_BIND_ADDR=${PANEL_BIND_ADDR:-0.0.0.0}
ELECTRU_BASE_PATH=$ELECTRU_BASE_PATH
# PostgreSQL is preferred (auto-provisioned by install-prerequisites.sh).
# SQLite path remains as emergency fallback only.
ELECTRU_DB_DSN=${ELECTRU_DB_DSN:-}
ELECTRU_DB_PATH=$CONFIG_DIR/electruvpn.db
ELECTRU_JWT_SECRET=$JWT_SECRET
ELECTRU_CONFIG_ENCRYPTION_KEY=$(openssl rand -hex 32)
ELECTRU_XRAY_BIN=/usr/local/bin/xray
ELECTRU_XRAY_CONFIG=$CONFIG_DIR/xray-config.json
ELECTRU_XRAY_API_PORT=10085
ELECTRU_PUBLIC_HOST=$PUBLIC_HOST
ELECTRU_PUBLIC_HOST_V4=${PUBLIC_HOST_V4:-${ELECTRU_PUBLIC_HOST_V4:-}}
ELECTRU_PREVIOUS_PUBLIC_HOST_V4=${ELECTRU_PREVIOUS_PUBLIC_HOST_V4:-}
ELECTRU_PUBLIC_HOST_V6=${PUBLIC_HOST_V6:-${ELECTRU_PUBLIC_HOST_V6:-}}
ELECTRU_PUBLIC_HOST_DOMAIN=${PUBLIC_HOST_DOMAIN:-${ELECTRU_PUBLIC_HOST_DOMAIN:-}}
ELECTRU_SECURITY_MODE=strict
ELECTRU_SUBSCRIPTION_BASE_URL=${SUBSCRIPTION_BASE_URL:-}
ELECTRU_SUBSCRIPTION_BASE_URL_V4=${SUBSCRIPTION_BASE_URL_V4:-${ELECTRU_SUBSCRIPTION_BASE_URL_V4:-}}
ELECTRU_SUBSCRIPTION_BASE_URL_V6=${SUBSCRIPTION_BASE_URL_V6:-${ELECTRU_SUBSCRIPTION_BASE_URL_V6:-}}
ELECTRU_SUBSCRIPTION_BASE_URL_DOMAIN=${SUBSCRIPTION_BASE_URL_DOMAIN:-${ELECTRU_SUBSCRIPTION_BASE_URL_DOMAIN:-}}
ELECTRU_PANEL_VPN_ACCESS=1
ELECTRU_DNS_RESOLVERS=https+local://1.1.1.1/dns-query,https+local://8.8.8.8/dns-query
ELECTRU_PANEL_TLS_ENABLED=$PANEL_TLS_ENABLED
ELECTRU_PANEL_TLS_CERT=$PANEL_TLS_CERT
ELECTRU_PANEL_TLS_KEY=$PANEL_TLS_KEY
ELECTRU_PANEL_TLS_HOST=$PANEL_TLS_HOST
ELECTRU_INITIAL_ADMIN_PASSWORD_FILE=$INITIAL_ADMIN_PASSWORD_FILE
ELECTRU_SINGBOX_BIN=/usr/local/bin/sing-box
ELECTRU_SINGBOX_CONFIG=$CONFIG_DIR/sing-box.json
TELEGRAM_BOT_TOKEN=${TELEGRAM_BOT_TOKEN:-}
TELEGRAM_CHAT_ID=${TELEGRAM_CHAT_ID:-}
ELECTRU_BACKUP_INTERVAL_MIN=${ELECTRU_BACKUP_INTERVAL_MIN:-1440}
ELECTRU_BACKUP_RETENTION=${ELECTRU_BACKUP_RETENTION:-14}
ELECTRU_BACKUP_REMOTE_DIR=${ELECTRU_BACKUP_REMOTE_DIR:-}
ELECTRU_LOG_RETENTION=${ELECTRU_LOG_RETENTION:-1h}
ELECTRU_LOG_MAX_SIZE=${ELECTRU_LOG_MAX_SIZE:-64M}
ELECTRU_LOG_LEVEL=${ELECTRU_LOG_LEVEL:-info}
ELECTRU_LOG_FORMAT=${ELECTRU_LOG_FORMAT:-json}
ELECTRU_ACCESS_LOG=${ELECTRU_ACCESS_LOG:-0}
EOF
  chmod 600 "$CONFIG_DIR/electruvpn.env"
  ok "Config written."
else
  chmod 600 "$CONFIG_DIR/electruvpn.env"
  warn "$CONFIG_DIR/electruvpn.env already exists -- leaving it untouched."
  PANEL_PORT=$(grep -oP '^ELECTRU_PANEL_PORT=\K.*' "$CONFIG_DIR/electruvpn.env" || echo "$PANEL_PORT")
  ELECTRU_BASE_PATH=$(grep -oP '^ELECTRU_BASE_PATH=\K.*' "$CONFIG_DIR/electruvpn.env" || echo "$ELECTRU_BASE_PATH")
  # Re-runs must keep the HTTPS settings of the existing install (otherwise the summary
  # says http://, the renewal hook is skipped and ufw 80/443 rules are not refreshed).
  _v="$(env_get ELECTRU_PANEL_TLS_ENABLED)"; [[ -z "$_v" ]] || PANEL_TLS_ENABLED="$_v"
  _v="$(env_get ELECTRU_PANEL_TLS_CERT)";    [[ -z "$_v" ]] || PANEL_TLS_CERT="$_v"
  _v="$(env_get ELECTRU_PANEL_TLS_HOST)";    [[ -z "$_v" ]] || PANEL_TLS_HOST="$_v"
  # A previous run that failed before the health check leaves the one-time admin
  # password on disk; show it now instead of losing it forever.
  if [[ -s "$CONFIG_DIR/initial-admin-password" ]]; then
    INITIAL_ADMIN_PASSWORD_FILE="$CONFIG_DIR/initial-admin-password"
    INITIAL_ADMIN_PASSWORD="$(cat "$INITIAL_ADMIN_PASSWORD_FILE")"
  fi
fi

# Re-validate values loaded from an existing production env file. A stale or
# manually edited port/TLS setting must fail before the service is replaced.
[[ "$PANEL_PORT" =~ ^[0-9]+$ ]] && (( PANEL_PORT >= 1 && PANEL_PORT <= 65535 )) || fail "Existing ELECTRU_PANEL_PORT is invalid: $PANEL_PORT"
[[ "$PANEL_TLS_ENABLED" == "0" || "$PANEL_TLS_ENABLED" == "1" ]] || fail "Existing ELECTRU_PANEL_TLS_ENABLED must be 0 or 1 (got: $PANEL_TLS_ENABLED)."
if [[ "$PANEL_TLS_ENABLED" == "1" ]]; then
  PANEL_TLS_CERT="$(env_get ELECTRU_PANEL_TLS_CERT)"
  PANEL_TLS_KEY="$(env_get ELECTRU_PANEL_TLS_KEY)"
  [[ -n "$PANEL_TLS_CERT" && -n "$PANEL_TLS_KEY" ]] || fail "Existing config enables panel TLS but certificate/key paths are missing."
  [[ -s "$PANEL_TLS_CERT" ]] || fail "Configured TLS certificate is missing or empty: $PANEL_TLS_CERT"
  [[ -s "$PANEL_TLS_KEY" ]] || fail "Configured TLS private key is missing or empty: $PANEL_TLS_KEY"
fi

# ---------- 6. atomic runtime deployment ----------
# Never overlay a new release on a live tree. A staging directory is validated
# first, then the service is stopped for a short atomic-ish swap. This prevents
# mixed v24/v25/v26 source/runtime trees after interrupted upgrades.
info "Preparing atomic runtime deployment..."
STAGE_DIR="${INSTALL_DIR}.staging.$$"
rm -rf "$STAGE_DIR"
mkdir -p "$STAGE_DIR"
install -m 0755 "$BUILD_DIR/electruvpn-server" "$STAGE_DIR/electruvpn-server"
cp -a "$BUILD_DIR/web" "$STAGE_DIR/web"
install -m 0750 "$BUILD_DIR/scripts/panel-settings-apply.sh" "$STAGE_DIR/panel-settings-apply.sh"
install -m 0750 "$BUILD_DIR/scripts/restore-backup.sh" "$STAGE_DIR/restore-backup.sh"
install -m 0750 "$BUILD_DIR/scripts/enable-privacy-mode.sh" "$STAGE_DIR/enable-privacy-mode.sh"
install -m 0750 "$BUILD_DIR/scripts/security-selftest.sh" "$STAGE_DIR/security-selftest.sh"
install -m 0750 "$BUILD_DIR/scripts/privacy-regression-test.sh" "$STAGE_DIR/privacy-regression-test.sh"
install -m 0750 "$BUILD_DIR/scripts/deep-audit-v35.sh" "$STAGE_DIR/deep-audit-v35.sh"
install -m 0750 "$BUILD_DIR/scripts/electruvpn-log-vacuum.sh" "$STAGE_DIR/log-vacuum.sh"
install -m 0750 "$BUILD_DIR/scripts/geo-update.sh" "$STAGE_DIR/geo-update.sh"
[[ -x "$STAGE_DIR/electruvpn-server" && -f "$STAGE_DIR/web/index.html" ]] || fail "Staged runtime validation failed."
if systemctl is-active --quiet electruvpn 2>/dev/null; then
  systemctl stop electruvpn
fi
BACKUP_RUNTIME="${INSTALL_DIR}.previous"
rm -rf "$BACKUP_RUNTIME"
HAD_PREVIOUS_RUNTIME=0
if [[ -d "$INSTALL_DIR" ]]; then
  mv "$INSTALL_DIR" "$BACKUP_RUNTIME"
  HAD_PREVIOUS_RUNTIME=1
fi
mv "$STAGE_DIR" "$INSTALL_DIR"
ok "Runtime staged atomically."

# Keep the previous runtime until the NEW service has actually passed its
# startup health check. An interrupted upgrade must never leave a broken
# binary as the only runnable copy. The previous tree is removed only after
# the new service is confirmed active below.
rollback_runtime() {
  systemctl stop electruvpn >/dev/null 2>&1 || true
  rm -rf "$INSTALL_DIR"
  if [[ "$HAD_PREVIOUS_RUNTIME" == "1" && -d "$BACKUP_RUNTIME" ]]; then
    mv "$BACKUP_RUNTIME" "$INSTALL_DIR"
  fi
}


# Ensure PostgreSQL DSN is written when prerequisites provisioned it.
if [[ -n "${ELECTRU_DB_DSN:-}" && -f "$CONFIG_DIR/electruvpn.env" ]]; then
  if grep -q '^ELECTRU_DB_DSN=$' "$CONFIG_DIR/electruvpn.env" || ! grep -q '^ELECTRU_DB_DSN=.' "$CONFIG_DIR/electruvpn.env"; then
    info "Writing auto-provisioned PostgreSQL DSN into electruvpn.env..."
    awk -v dsn="$ELECTRU_DB_DSN" '
      BEGIN { done=0 }
      /^ELECTRU_DB_DSN=/ { print "ELECTRU_DB_DSN=" dsn; done=1; next }
      { print }
      END { if (!done) print "ELECTRU_DB_DSN=" dsn }
    ' "$CONFIG_DIR/electruvpn.env" > "$CONFIG_DIR/electruvpn.env.tmp" \
      && mv "$CONFIG_DIR/electruvpn.env.tmp" "$CONFIG_DIR/electruvpn.env"
    chmod 600 "$CONFIG_DIR/electruvpn.env" 2>/dev/null || true
    ok "ELECTRU_DB_DSN configured for PostgreSQL."
  fi
fi

# ---------- 7. systemd service ----------
info "Installing the systemd service..."
cp "$BUILD_DIR/deploy/electruvpn.service" /etc/systemd/system/electruvpn.service
cp "$BUILD_DIR/deploy/electruvpn-security-audit.service" /etc/systemd/system/electruvpn-security-audit.service
cp "$BUILD_DIR/deploy/electruvpn-security-audit.timer" /etc/systemd/system/electruvpn-security-audit.timer
cp "$BUILD_DIR/deploy/electruvpn-log-vacuum.service" /etc/systemd/system/electruvpn-log-vacuum.service
cp "$BUILD_DIR/deploy/electruvpn-log-vacuum.timer" /etc/systemd/system/electruvpn-log-vacuum.timer
cp "$BUILD_DIR/deploy/electruvpn-geo-update.service" /etc/systemd/system/electruvpn-geo-update.service
cp "$BUILD_DIR/deploy/electruvpn-geo-update.timer" /etc/systemd/system/electruvpn-geo-update.timer
mkdir -p /usr/local/share/xray
# Management CLI: `electruvpn` (menu, port/path change, password reset, update, ...).
install -m 0755 "$BUILD_DIR/scripts/electruvpn-cli.sh" /usr/local/bin/electruvpn
mkdir -p /etc/systemd/journald.conf.d
install -m 0644 "$BUILD_DIR/deploy/90-electruvpn-journald.conf" /etc/systemd/journald.conf.d/90-electruvpn-journald.conf
sed -i "s#^WorkingDirectory=.*#WorkingDirectory=$INSTALL_DIR#"       /etc/systemd/system/electruvpn.service
sed -i "s#^EnvironmentFile=.*#EnvironmentFile=$CONFIG_DIR/electruvpn.env#" /etc/systemd/system/electruvpn.service
sed -i "s#^ExecStart=.*#ExecStart=$INSTALL_DIR/electruvpn-server#"   /etc/systemd/system/electruvpn.service

systemctl daemon-reload
systemctl enable electruvpn >/dev/null 2>&1
systemctl enable --now electruvpn-security-audit.timer >/dev/null 2>&1 || warn "Could not enable the daily security audit timer automatically; check systemctl status electruvpn-security-audit.timer."
systemctl enable --now electruvpn-log-vacuum.timer >/dev/null 2>&1 || warn "Could not enable the hourly log cleanup timer automatically; check systemctl status electruvpn-log-vacuum.timer."
systemctl enable --now electruvpn-geo-update.timer >/dev/null 2>&1 || warn "Could not enable the weekly geo data update timer; check systemctl status electruvpn-geo-update.timer."
systemctl restart systemd-journald >/dev/null 2>&1 || warn "Could not reload systemd-journald; log retention limits will apply after journald reload."
systemctl start electruvpn-log-vacuum.service >/dev/null 2>&1 || warn "Could not perform the initial log vacuum; the hourly timer will retry automatically."
# First geo data download happens before the first start so Xray loads it immediately.
# A failure here is never fatal: Xray ships its own default data.
info "Fetching geoip/geosite data (Iran-aware)..."
bash "$INSTALL_DIR/geo-update.sh" update --no-restart || warn "Geo data download failed; the weekly timer will retry (or run: electruvpn geo update)."
systemctl restart electruvpn
info "Waiting for the panel to listen on port ${PANEL_PORT}..."
PANEL_HEALTHY=0
for _ in $(seq 1 30); do
  sleep 1
  if systemctl is-active --quiet electruvpn && port_is_listening "$PANEL_PORT"; then
    sleep 3   # catch crash-loops that only die a moment after binding
    if systemctl is-active --quiet electruvpn; then PANEL_HEALTHY=1; fi
    break
  fi
done
if [[ "$PANEL_HEALTHY" == 1 ]]; then
  ok "electruvpn service is running."
  if [[ -n "${INITIAL_ADMIN_PASSWORD_FILE:-}" && -f "$INITIAL_ADMIN_PASSWORD_FILE" ]]; then
    rm -f "$INITIAL_ADMIN_PASSWORD_FILE"
    sed -i '/^ELECTRU_INITIAL_ADMIN_PASSWORD_FILE=/d' "$CONFIG_DIR/electruvpn.env"
    systemctl daemon-reload
  fi
else
  journalctl -u electruvpn -n 80 --no-pager || true
  warn "New runtime failed its startup health check. Rolling back to the previous runtime..."
  rollback_runtime
  systemctl daemon-reload
  if [[ "$HAD_PREVIOUS_RUNTIME" == "1" ]] && systemctl start electruvpn >/dev/null 2>&1 && systemctl is-active --quiet electruvpn; then
    ok "Previous runtime restored and is healthy."
  else
    warn "Automatic runtime rollback could not confirm a healthy previous service."
  fi
  fail "electruvpn service failed to start; previous runtime was restored when available."
fi

if [[ "$HAD_PREVIOUS_RUNTIME" == "1" && -d "$BACKUP_RUNTIME" ]]; then
  rm -rf "$BACKUP_RUNTIME"
fi

# ---------- 7.5 certificate renewal ----------
if [[ "$PANEL_TLS_ENABLED" == "1" && -n "$PANEL_TLS_CERT" ]]; then
  mkdir -p /etc/letsencrypt/renewal-hooks/deploy
  cat >/etc/letsencrypt/renewal-hooks/deploy/electruvpn-restart.sh <<'EOF'
#!/usr/bin/env bash
systemctl restart electruvpn.service
EOF
  chmod 700 /etc/letsencrypt/renewal-hooks/deploy/electruvpn-restart.sh
  ok "Certificate renewal deploy hook configured."
  # Certbot lives in a venv here, so there is NO packaged renewal timer. Without this
  # the certificate silently expires (90 days; ~6 days for IP certificates).
  if [[ -x "$CERTBOT_VENV/bin/certbot" ]]; then
    cat >/etc/systemd/system/electruvpn-certbot-renew.service <<EOF
[Unit]
Description=Renew ELECTRUVPN Let's Encrypt certificate
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
ExecStart=$CERTBOT_VENV/bin/certbot renew --quiet
EOF
    cat >/etc/systemd/system/electruvpn-certbot-renew.timer <<'EOF'
[Unit]
Description=Twice-daily ELECTRUVPN certificate renewal check

[Timer]
OnCalendar=*-*-* 00,12:00:00
RandomizedDelaySec=30m
Persistent=true

[Install]
WantedBy=timers.target
EOF
    systemctl daemon-reload
    systemctl enable --now electruvpn-certbot-renew.timer >/dev/null 2>&1 || warn "Could not enable certificate auto-renewal; check systemctl status electruvpn-certbot-renew.timer."
    ok "Certificate auto-renewal timer enabled."
  fi
fi

# ---------- 8. firewall ----------
# Client configs (any inbound port) do not work while ufw is active, so after EVERY
# fresh install and EVERY update ufw is DISABLED by default.
# The SSH/panel allow rules are still added first, so if you enable ufw by hand later
# you will not lock yourself out.
# Set ELECTRU_KEEP_UFW=1 to restore the old behaviour (fresh install enables ufw,
# update preserves its previous state).
if command -v ufw >/dev/null 2>&1; then
  info "Configuring firewall (ufw) rules..."
  detect_ssh_ports() {
    {
      sshd -T 2>/dev/null | awk '$1=="port"{print $2}'
      ss -ltnHp 2>/dev/null | awk '/"sshd"/ {n=split($4,a,":"); print a[n]}'
      systemctl cat ssh.socket 2>/dev/null | awk -F= '/^ListenStream=/ {n=split($2,a,":"); print a[n]}'
      if [[ -n "${SSH_CONNECTION:-}" ]]; then awk '{print $4}' <<<"$SSH_CONNECTION"; fi
    } | grep -E '^[0-9]+$' | sort -un || true
  }
  SSH_PORTS="$(detect_ssh_ports)"
  [[ -n "$SSH_PORTS" ]] || SSH_PORTS=22
  for _p in $SSH_PORTS; do
    ufw allow "${_p}/tcp" >/dev/null 2>&1 || true
  done
  ufw allow OpenSSH >/dev/null 2>&1 || true
  ufw deny 53 >/dev/null 2>&1 || true
  ufw deny 853 >/dev/null 2>&1 || true
  ufw allow "${PANEL_PORT}/tcp" >/dev/null 2>&1 || true
  if [[ "$PANEL_TLS_ENABLED" == "1" && "$PANEL_PORT" == "443" ]]; then
    ufw allow 443/tcp >/dev/null 2>&1 || true
  fi
  if [[ "$PANEL_TLS_ENABLED" == "1" && -n "$PANEL_TLS_CERT" ]]; then
    ufw allow 80/tcp >/dev/null 2>&1 || true
  fi

  if [[ "${ELECTRU_KEEP_UFW:-0}" == "1" ]]; then
    if [[ "${ELECTRU_UPDATE_MODE:-0}" == "1" && "${ELECTRU_UFW_WAS_ACTIVE:-unknown}" != "1" ]]; then
      warn "ELECTRU_KEEP_UFW=1: ufw state preserved during update."
    elif ! ufw status | grep -q "Status: active"; then
      ufw --force enable >/dev/null 2>&1 || warn "Could not enable ufw automatically -- check your firewall settings manually."
    fi
    ok "Firewall rules checked for SSH and port $PANEL_PORT (ufw kept as configured)."
  else
    ufw --force disable >/dev/null 2>&1 || true
    if ufw status 2>/dev/null | grep -qi "inactive"; then
      ok "ufw is DISABLED (required for client configs to work). Enable later with: ufw enable"
    else
      warn "Could not confirm that ufw is disabled -- run manually: ufw disable"
    fi
  fi
else
  warn "ufw is not installed; nothing to disable."
fi

# ---------- 8.5 fail2ban ----------
info "Configuring fail2ban for panel login abuse..."
install -m 0644 "$BUILD_DIR/scripts/electruvpn-fail2ban.filter" /etc/fail2ban/filter.d/electruvpn-auth.conf
sed "s/port = 2087/port = $PANEL_PORT/" "$BUILD_DIR/scripts/electruvpn-fail2ban.local" > /etc/fail2ban/jail.d/electruvpn-auth.local
if ! fail2ban-client -t >/dev/null 2>&1; then
  warn "fail2ban configuration test failed; removing the ELECTRUVPN jail so fail2ban itself stays healthy. Details: fail2ban-client -t"
  rm -f /etc/fail2ban/jail.d/electruvpn-auth.local
fi
systemctl enable fail2ban >/dev/null 2>&1 || true
systemctl restart fail2ban >/dev/null 2>&1 || warn "Could not restart fail2ban automatically; check systemctl status fail2ban."
ok "fail2ban login protection configured."

# Honeypot auto-ban jail (multi-layer with ufw action)
if [[ -f "$BUILD_DIR/scripts/electruvpn-honeypot.fail2ban.filter" ]]; then
  install -m 0644 "$BUILD_DIR/scripts/electruvpn-honeypot.fail2ban.filter" /etc/fail2ban/filter.d/electruvpn-honeypot.conf
  install -m 0644 "$BUILD_DIR/scripts/electruvpn-honeypot.fail2ban.local" /etc/fail2ban/jail.d/electruvpn-honeypot.local
  if ! fail2ban-client -t >/dev/null 2>&1; then
    warn "honeypot fail2ban jail failed config test; removing jail file"
    rm -f /etc/fail2ban/jail.d/electruvpn-honeypot.local
  else
    systemctl restart fail2ban >/dev/null 2>&1 || true
    ok "fail2ban honeypot jail configured (ufw multi-layer bans)."
  fi
fi


# ---------- 8.8 safe automatic TCP optimization ----------
# Automatically enable BBR + fq when the currently running kernel supports it.
# Do NOT silently replace the host kernel: a patched BBRv3 kernel is a kernel
# upgrade requiring bootloader validation and a reboot, which can lock out a VPS.
info "Checking TCP congestion-control support and applying safe BBR tuning..."
install -m 0750 "$BUILD_DIR/scripts/network-optimizer.sh" /usr/local/bin/electruvpn-network-optimizer
install -m 0750 "$BUILD_DIR/scripts/bbrv3-kernel-manager.sh" /usr/local/bin/electruvpn-bbrv3-kernel-manager
if bash /usr/local/bin/electruvpn-network-optimizer apply; then
  ok "Kernel BBR tuning applied (BBR implementation depends on the running kernel)."
else
  warn "BBR tuning was not applied; the running kernel may not expose BBR. Panel installation will continue."
fi
info "Applying raise-only high-concurrency kernel headroom (socket backlog, file handles, QUIC buffers)..."
if bash /usr/local/bin/electruvpn-network-optimizer apply-highconn; then
  ok "High-concurrency kernel headroom checked/applied (undo: electruvpn-network-optimizer rollback-highconn)."
else
  warn "High-concurrency kernel tuning was not applied; panel installation will continue."
fi
if [[ -f /etc/electruvpn/bbrv3-kernel-installed ]]; then
  ok "BBRv3 kernel marker detected."
else
  warn "BBRv3 kernel is not confirmed. BBR sysctl settings alone cannot install BBRv3; a patched kernel and manual reboot are required."
fi
info "BBRv3 kernel workflow installed: electruvpn-bbrv3-kernel-manager status | preflight | install-bundle | confirm | cancel-pending"
info "No custom kernel is downloaded or installed automatically; kernel changes require an explicit admin command and a manual reboot."

# ---------- 9. summary ----------
SERVER_IP=$(curl -fsSL -4 ifconfig.me 2>/dev/null || hostname -I | awk '{print $1}')

echo
echo -e "${BOLD}${GREEN}ELECTRUVPN BETA1 is installed and running.${RESET}"
echo
if [[ "$PANEL_TLS_ENABLED" == "1" ]]; then
  PANEL_SCHEME="https"
else
  PANEL_SCHEME="http"
fi
PANEL_HOST_FOR_URL="${PANEL_TLS_HOST:-$SERVER_IP}"
if [[ "$PANEL_PORT" == "443" ]]; then
  PANEL_URL="${PANEL_SCHEME}://${PANEL_HOST_FOR_URL}${ELECTRU_BASE_PATH}"
elif [[ "$PANEL_PORT" == "80" ]]; then
  PANEL_URL="${PANEL_SCHEME}://${PANEL_HOST_FOR_URL}${ELECTRU_BASE_PATH}"
else
  PANEL_URL="${PANEL_SCHEME}://${PANEL_HOST_FOR_URL}:${PANEL_PORT}${ELECTRU_BASE_PATH}"
fi
echo -e "  Panel URL     : ${CYAN}${PANEL_URL}${RESET}"
if [[ -n "${SUBSCRIPTION_BASE_URL:-}" ]]; then
  echo -e "  Subscription  : ${CYAN}${SUBSCRIPTION_BASE_URL}${RESET}"
fi
if [[ "$PANEL_TLS_ENABLED" == "1" ]]; then
  if [[ "$PANEL_PORT" == "443" ]]; then
    IP_FALLBACK_URL="https://${SERVER_IP}${ELECTRU_BASE_PATH}"
  else
    IP_FALLBACK_URL="https://${SERVER_IP}:${PANEL_PORT}${ELECTRU_BASE_PATH}"
  fi
  echo -e "  IP fallback   : ${CYAN}${IP_FALLBACK_URL}${RESET} (certificate warning expected for IP access)"
fi
INITIAL_ADMIN_PASSWORD="${INITIAL_ADMIN_PASSWORD:-}"
if [[ -n "$INITIAL_ADMIN_PASSWORD" ]]; then
  echo -e "  Initial login : ${BOLD}admin / ${INITIAL_ADMIN_PASSWORD}${RESET}"
  echo -e "  ${RED}Save this password now and change it after first login.${RESET}"
else
  echo -e "  Initial login : ${BOLD}admin / generated password from previous install${RESET}"
fi
echo
echo "  Useful commands:"
echo "    systemctl status electruvpn"
echo "    journalctl -u electruvpn -f"
echo "    systemctl restart electruvpn"
echo "    electruvpn            (management menu: port, path, password reset, update, backup)"
echo
echo "  Next steps:"
echo "    1) Log in and immediately change the generated admin password."
echo "    2) Generate a REALITY keypair on this server:  xray x25519"
echo "    3) Create your first config from the Inbounds page."
echo "    4) Hysteria2/TUIC/WireGuard/AnyTLS/ShadowTLS are managed by sing-box when enabled."
echo "    5) If you put the panel behind a reverse proxy, set ELECTRU_PANEL_BIND_ADDR=127.0.0.1."
echo "    6) ufw was disabled by the installer so every config port works."
echo "       (If you enable ufw yourself later: ufw allow <port>/tcp for each config port.)"
echo
