#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=scripts/platform.sh
source "$ROOT/scripts/platform.sh"
check() {
  local raw="$1" expected_goarch="$2" expected_goarm="$3" expected_sb="$4"
  electru_detect_platform "$raw"
  [[ "$GOARCH" == "$expected_goarch" ]] || { echo "FAIL $raw: GOARCH=$GOARCH"; exit 1; }
  [[ "${GOARM:-}" == "$expected_goarm" ]] || { echo "FAIL $raw: GOARM=${GOARM:-}"; exit 1; }
  [[ "$SBARCH" == "$expected_sb" ]] || { echo "FAIL $raw: SBARCH=$SBARCH"; exit 1; }
}
check x86_64 amd64 "" amd64
check amd64 amd64 "" amd64
check i386 386 "" 386
check i686 386 "" 386
check aarch64 arm64 "" arm64
check arm64 arm64 "" arm64
check armv7l arm 7 armv7
check armv7 arm 7 armv7
check armv6l arm 6 armv6
if electru_detect_platform riscv64; then echo 'FAIL unsupported architecture accepted'; exit 1; fi
echo 'platform matrix: ok'
