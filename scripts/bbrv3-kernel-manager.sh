#!/usr/bin/env bash
# Explicit, fail-closed BBRv3 kernel package workflow for Ubuntu 24.04.
# This tool never downloads third-party kernels and never reboots the host.
set -Eeuo pipefail

STATE_DIR=/var/lib/electruvpn
PENDING="$STATE_DIR/bbrv3-kernel-pending.env"
CONFIRMED_DIR=/etc/electruvpn
CONFIRMED="$CONFIRMED_DIR/bbrv3-kernel-installed"

say() { printf '%s\n' "$*"; }
fail() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }
root_required() { [[ ${EUID:-$(id -u)} -eq 0 ]] || fail 'Run as root (sudo).'; }
read_os() { . /etc/os-release; }
get_value() { sed -n "s/^$1=//p" "$2" | head -n1; }

status() {
  read_os
  local current available cc qdisc secure='unknown' pending='none' confirmed='no'
  current=$(uname -r)
  available=$(sysctl -n net.ipv4.tcp_available_congestion_control 2>/dev/null || true)
  cc=$(sysctl -n net.ipv4.tcp_congestion_control 2>/dev/null || true)
  qdisc=$(sysctl -n net.core.default_qdisc 2>/dev/null || true)
  if command -v mokutil >/dev/null 2>&1; then
    secure=$(mokutil --sb-state 2>/dev/null | head -n1 || true)
  elif [[ -d /sys/firmware/efi/efivars ]]; then
    secure='unknown (mokutil unavailable)'
  else
    secure='disabled/not-UEFI'
  fi
  if [[ -f "$PENDING" ]]; then
    pending="$(get_value EXPECTED_KERNEL "$PENDING")"
    [[ "$current" == "$pending" ]] && pending='booted-awaiting-confirmation'
  fi
  [[ -f "$CONFIRMED" ]] && confirmed='marker-present'
  say "os=${ID:-unknown}"
  say "os_version=${VERSION_ID:-unknown}"
  say "arch=$(dpkg --print-architecture 2>/dev/null || uname -m)"
  say "kernel=$current"
  say "available_cc=${available:-unknown}"
  say "current_cc=${cc:-unknown}"
  say "qdisc=${qdisc:-unknown}"
  say "secure_boot=$secure"
  say "bbrv3_pending=$pending"
  say "bbrv3_marker=$confirmed"
  if [[ -f "$CONFIRMED" ]]; then
    say "bbrv3_provider=$(get_value PROVIDER_URL "$CONFIRMED")"
    say "bbrv3_expected_kernel=$(get_value EXPECTED_KERNEL "$CONFIRMED")"
  fi
}

preflight() {
  read_os
  [[ "${ID:-}" == ubuntu && "${VERSION_ID:-}" == 24.04 ]] || fail 'Kernel package workflow supports Ubuntu 24.04 LTS only.'
  local arch boot_avail root_avail
  arch=$(dpkg --print-architecture)
  [[ "$arch" == amd64 || "$arch" == arm64 ]] || fail "Unsupported architecture: $arch (supported: amd64, arm64)."
  [[ -d /boot/grub || -f /boot/grub/grub.cfg || -d /boot/grub2 ]] || fail 'GRUB was not detected. Refusing kernel package installation.'
  command -v dpkg-deb >/dev/null 2>&1 || fail 'dpkg-deb is required.'
  command -v apt-get >/dev/null 2>&1 || fail 'apt-get is required.'
  boot_avail=$(df -Pk /boot 2>/dev/null | awk 'NR==2 {print $4}')
  root_avail=$(df -Pk / 2>/dev/null | awk 'NR==2 {print $4}')
  [[ "$boot_avail" =~ ^[0-9]+$ && "$boot_avail" -ge 262144 ]] || fail 'At least 256 MiB free space in /boot is required.'
  [[ "$root_avail" =~ ^[0-9]+$ && "$root_avail" -ge 1048576 ]] || fail 'At least 1 GiB free space on / is required.'
  if command -v mokutil >/dev/null 2>&1 && mokutil --sb-state 2>/dev/null | grep -qi 'SecureBoot enabled'; then
    fail 'Secure Boot is enabled. This workflow refuses unsigned/unverified custom kernels; resolve signing and boot trust manually first.'
  fi
  say 'Preflight passed.'
  say "Ubuntu ${VERSION_ID}, architecture ${arch}, current kernel $(uname -r)."
  say 'No packages installed and no reboot performed by preflight.'
}

install_bundle() {
  root_required
  [[ $# -eq 4 ]] || fail 'Usage: install-bundle DIR EXPECTED_KERNEL_RELEASE https://provider.example/path --confirm-kernel-change'
  local dir expected provider confirmation sum_file sum packages package version arch sim
  dir=$(realpath -- "$1")
  expected=$2
  provider=$3
  confirmation=$4
  [[ "$confirmation" == '--confirm-kernel-change' ]] || fail 'Explicit confirmation flag is required.'
  [[ "$expected" =~ ^[A-Za-z0-9.+_-]+$ && "${expected,,}" == *bbrv3* ]] || fail 'Expected kernel release must be exact and contain bbrv3 (e.g. 6.x.y-bbrv3).'
  [[ "$provider" == https://* ]] || fail 'Provider URL must use HTTPS.'
  [[ -d "$dir" && -f "$dir/SHA256SUMS" ]] || fail 'Bundle directory must contain SHA256SUMS and .deb packages.'
  preflight
  # Disallow checksum paths and require all package files to be explicitly hashed.
  if grep -Ev '^[a-fA-F0-9]{64}  [A-Za-z0-9._+~-]+\.deb$' "$dir/SHA256SUMS" >/dev/null; then
    fail 'SHA256SUMS must contain only SHA-256 entries for simple .deb filenames (no paths).'
  fi
  (cd "$dir" && sha256sum -c SHA256SUMS) || fail 'Bundle checksum validation failed.'
  shopt -s nullglob
  local debs=("$dir"/*.deb)
  shopt -u nullglob
  ((${#debs[@]} > 0)) || fail 'No .deb files found in bundle.'
  for deb in "${debs[@]}"; do
    [[ "$(basename "$deb")" =~ ^[A-Za-z0-9._+~-]+\.deb$ ]] || fail "Unexpected package filename: $(basename "$deb")"
    grep -qE "  $(basename "$deb")$" "$dir/SHA256SUMS" || fail "Missing checksum entry for $(basename "$deb")."
    package=$(dpkg-deb -f "$deb" Package 2>/dev/null || true)
    version=$(dpkg-deb -f "$deb" Version 2>/dev/null || true)
    arch=$(dpkg-deb -f "$deb" Architecture 2>/dev/null || true)
    [[ "$package" =~ ^linux-(image|modules|headers|tools)(-|$) ]] || fail "Non-kernel package rejected: ${package:-unknown}"
    [[ "$arch" == all || "$arch" == "$(dpkg --print-architecture)" ]] || fail "Architecture mismatch for $package: $arch"
    if [[ "$package $version" == *bbrv3* ]]; then :; fi
  done
  local image_found=0
  for deb in "${debs[@]}"; do
    package=$(dpkg-deb -f "$deb" Package)
    version=$(dpkg-deb -f "$deb" Version)
    if [[ "$package" == linux-image-* && "${package,,} ${version,,}" == *bbrv3* ]]; then image_found=1; fi
  done
  [[ $image_found -eq 1 ]] || fail 'At least one linux-image package name/version must explicitly identify bbrv3.'
  [[ ! -e "$PENDING" ]] || fail "A kernel install is already pending: $(get_value EXPECTED_KERNEL "$PENDING"). Confirm or cancel it first."
  mkdir -p "$STATE_DIR" "$CONFIRMED_DIR"
  chmod 0750 "$STATE_DIR"
  local current_kernel bundle_hash
  current_kernel=$(uname -r)
  bundle_hash=$(sha256sum "$dir/SHA256SUMS" | awk '{print $1}')
  say 'Reviewing package transaction (simulation only)...'
  sim=$(apt-get -s install "${debs[@]}" 2>&1) || { printf '%s\n' "$sim" >&2; fail 'APT simulation failed; no packages were installed.'; }
  if grep -Eq '^(Remv |The following packages will be REMOVED)' <<<"$sim"; then
    printf '%s\n' "$sim" >&2
    fail 'APT proposes removing packages. Refusing kernel change.'
  fi
  say 'Candidate kernel package bundle passed checks.'
  say "Current kernel: $current_kernel"
  say "Expected kernel after manual reboot: $expected"
  say "Provider: $provider"
  say "Bundle SHA256SUMS digest: $bundle_hash"
  say 'Installing the explicitly supplied local kernel packages. This command will NOT reboot or change GRUB default.'
  DEBIAN_FRONTEND=noninteractive apt-get install -y "${debs[@]}"
  [[ -e "/boot/vmlinuz-$expected" ]] || fail "Packages installed but /boot/vmlinuz-$expected was not found. Old kernel was not removed; inspect packages and GRUB manually."
  command -v update-grub >/dev/null 2>&1 && update-grub
  umask 077
  cat > "$PENDING" <<EOF_PENDING
EXPECTED_KERNEL=$expected
PREVIOUS_KERNEL=$current_kernel
PROVIDER_URL=$provider
BUNDLE_SHA256=$bundle_hash
EOF_PENDING
  chmod 0600 "$PENDING"
  say 'Kernel package staged successfully; no reboot was performed.'
  say "Next: schedule a maintenance window, ensure provider console access, reboot manually, then run: electruvpn-bbrv3-kernel-manager confirm"
}

confirm_kernel() {
  root_required
  [[ -f "$PENDING" ]] || fail 'No pending BBRv3 kernel install found.'
  local expected current available cc
  expected=$(get_value EXPECTED_KERNEL "$PENDING")
  current=$(uname -r)
  [[ -n "$expected" && "$current" == "$expected" ]] || fail "Running kernel is '$current', expected '$expected'. Reboot manually into the candidate kernel before confirming."
  available=$(sysctl -n net.ipv4.tcp_available_congestion_control 2>/dev/null || true)
  grep -qw bbr <<<"$available" || fail 'The running kernel does not advertise BBR; marker not written.'
  cc=$(sysctl -n net.ipv4.tcp_congestion_control 2>/dev/null || true)
  [[ "$cc" == bbr ]] || fail "Current congestion control is '$cc', not bbr. Run the network optimizer and retry."
  mkdir -p "$CONFIRMED_DIR"
  install -m 0600 "$PENDING" "$CONFIRMED"
  rm -f "$PENDING"
  say 'Kernel boot and BBR checks passed; provider-confirmation marker written.'
  say 'Note: Linux exposes BBRv1/v3 under the same name. This confirms the admin-supplied provider bundle and running kernel, not independent cryptographic proof of algorithm source.'
  status
}

cancel_pending() {
  root_required
  [[ -f "$PENDING" ]] || fail 'No pending kernel change to cancel.'
  local expected current
  expected=$(get_value EXPECTED_KERNEL "$PENDING")
  current=$(uname -r)
  [[ "$current" != "$expected" ]] || fail 'The candidate kernel is running. Do not cancel state; verify or boot the previous kernel first.'
  rm -f "$PENDING"
  say 'Pending marker cancelled. Installed kernel packages were intentionally left in place; remove them only after confirming the previous kernel boots and SSH access works.'
}

usage() {
  cat <<'USAGE'
ELECTRUVPN BBRv3 Kernel Manager (Ubuntu 24.04)

Commands:
  status
  preflight
  install-bundle DIR EXPECTED_KERNEL_RELEASE https://provider/path --confirm-kernel-change
  confirm
  cancel-pending

Safety:
  - Never downloads a kernel package itself.
  - Requires a local .deb bundle with SHA256SUMS and bbrv3-labelled image package.
  - Does not change GRUB default and never reboots automatically.
  - Keep the old kernel installed and ensure provider console access before reboot.
USAGE
}

case "${1:-status}" in
  status) status ;;
  preflight) preflight ;;
  install-bundle) shift; install_bundle "$@" ;;
  confirm) confirm_kernel ;;
  cancel-pending) cancel_pending ;;
  -h|--help|help) usage ;;
  *) usage >&2; exit 2 ;;
esac
