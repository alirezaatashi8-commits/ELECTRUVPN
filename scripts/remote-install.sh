#!/usr/bin/env bash
#
# One-line remote installer for ELECTRUVPN from GitHub main.
# Usage (on a fresh Ubuntu/Debian server as root):
#
#   curl -fsSL https://raw.githubusercontent.com/alirezaatashi8-commits/ELECTRUVPN/main/scripts/remote-install.sh | bash
#
# The installer runs INTERACTIVELY (HTTPS certificate wizard: domain / server IP /
# skip) even when this script is piped from curl: the terminal (/dev/tty) is
# re-attached to install.sh. Without any terminal (cloud-init, CI) or with
# ELECTRU_NONINTERACTIVE=1 it falls back to the unattended mode (-y).
#
set -euo pipefail

REPO_ZIP="${ELECTRU_REPO_ZIP:-https://github.com/alirezaatashi8-commits/ELECTRUVPN/archive/refs/heads/main.zip}"
WORK="$(mktemp -d /tmp/electruvpn-remote-install.XXXXXX)"
cleanup() { rm -rf "$WORK"; }
trap cleanup EXIT

if [[ ${EUID:-$(id -u)} -ne 0 ]]; then
  echo "Run as root (or pipe into: curl ... | sudo bash)" >&2
  exit 1
fi

export DEBIAN_FRONTEND=noninteractive
if ! command -v curl >/dev/null 2>&1 || ! command -v unzip >/dev/null 2>&1; then
  apt-get update -qq
  apt-get install -y -qq curl unzip ca-certificates >/dev/null
fi

echo "➜ Downloading ELECTRUVPN from GitHub..."
curl --fail --location --proto '=https' --tlsv1.2 --retry 3 --connect-timeout 20 --max-time 300 \
  -o "$WORK/src.zip" "$REPO_ZIP"

echo "➜ Extracting..."
unzip -q "$WORK/src.zip" -d "$WORK/src"
ROOT="$WORK/src"
if [[ ! -f "$ROOT/install.sh" ]]; then
  ROOT="$(dirname "$(find "$WORK/src" -maxdepth 2 -name install.sh | head -n1)")"
fi
[[ -f "$ROOT/install.sh" && -f "$ROOT/go.mod" ]] || { echo "✗ install.sh/go.mod not found in archive"; exit 1; }

echo "➜ Running installer from $ROOT ..."
cd "$ROOT"

# `curl ... | bash` makes stdin a pipe, so install.sh could never ask its
# questions. Re-attach the real terminal so the certificate wizard
# (domain / IP / skip) is shown exactly like a manual `bash install.sh`.
HAVE_TTY=0
if ( : </dev/tty ) 2>/dev/null; then HAVE_TTY=1; fi

if [[ "${ELECTRU_NONINTERACTIVE:-0}" != "1" && "$HAVE_TTY" == "1" ]]; then
  # --no-confirm: no extra "Proceed? [Y/n]" (the user already ran the command),
  # but the certificate wizard stays interactive.
  bash install.sh --no-confirm </dev/tty
else
  echo "! No interactive terminal available: running unattended (-y)."
  echo "  Set ELECTRU_CERT_CHOICE / ELECTRU_CERT_DOMAIN / ELECTRU_SUBSCRIPTION_DOMAIN to preseed the wizard."
  bash install.sh -y
fi
