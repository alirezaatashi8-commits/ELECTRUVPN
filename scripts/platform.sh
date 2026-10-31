#!/usr/bin/env bash
# Shared Linux architecture normalization for the installer and diagnostics.
# Sets: PLATFORM_LABEL, GOARCH, GOARM, GO_TARBALL_ARCH, SBARCH.
electru_detect_platform() {
  local raw="${1:-$(uname -m)}"
  case "$raw" in
    x86_64|amd64)
      PLATFORM_LABEL="x86_64 / AMD64 (Intel or AMD 64-bit)"
      GOARCH="amd64"; GOARM=""; GO_TARBALL_ARCH="amd64"; SBARCH="amd64" ;;
    i386|i486|i586|i686|x86)
      PLATFORM_LABEL="x86 32-bit (386)"
      GOARCH="386"; GOARM=""; GO_TARBALL_ARCH="386"; SBARCH="386" ;;
    aarch64|arm64)
      PLATFORM_LABEL="ARM64 / AArch64"
      GOARCH="arm64"; GOARM=""; GO_TARBALL_ARCH="arm64"; SBARCH="arm64" ;;
    armv7l|armv7|armhf|armv8l)
      PLATFORM_LABEL="ARMv7 32-bit"
      GOARCH="arm"; GOARM="7"; GO_TARBALL_ARCH="armv6l"; SBARCH="armv7" ;;
    armv6l|armv6)
      PLATFORM_LABEL="ARMv6 32-bit"
      GOARCH="arm"; GOARM="6"; GO_TARBALL_ARCH="armv6l"; SBARCH="armv6" ;;
    *)
      return 1
      ;;
  esac
  export PLATFORM_LABEL GOARCH GO_TARBALL_ARCH SBARCH
  if [[ -n "$GOARM" ]]; then export GOARM; else unset GOARM 2>/dev/null || true; fi
}
