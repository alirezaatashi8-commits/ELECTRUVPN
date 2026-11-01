#!/usr/bin/env bash
# Rebuild RELEASE_SOURCE_MANIFEST.txt from the current tree.
# Run after every source change before packaging a release ZIP.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"
out="$root/RELEASE_SOURCE_MANIFEST.txt"
tmp="$(mktemp)"
{
  echo "# ELECTRUVPN release source manifest"
  echo "# Format: SHA-256  relative/path"
  echo "# Integrity entries below are verified by install.sh before the build."
  # Portable file list: regular files only, sorted, relative paths.
  # Exclude VCS and local caches if present.
  find . -type f \
    ! -path './.git/*' \
    ! -path './.idea/*' \
    ! -path './.vscode/*' \
    ! -path '*/node_modules/*' \
    ! -path './go/pkg/*' \
    ! -name '*.zip' \
    ! -name 'electruvpn' \
    ! -name 'electruvpn-test' \
    ! -name '.DS_Store' \
    | sed 's|^\./||' \
    | LC_ALL=C sort \
    | while read -r f; do
        # skip the manifest itself while hashing (rewritten at end)
        [[ "$f" == "RELEASE_SOURCE_MANIFEST.txt" ]] && continue
        sha256sum -- "$f"
      done
} > "$tmp"
# sha256sum prints "HASH  file" (two spaces); normalize to single space for install.sh
awk 'NR<=3 {print; next} {print $1 "  " substr($0, index($0,$2))}' "$tmp" > "$out"
rm -f "$tmp"
# Count entries
n=$(tail -n +4 "$out" | wc -l | tr -d ' ')
echo "Wrote $out ($n files)"
