#!/usr/bin/env bash
set -euo pipefail
# Production asset build. Pin esbuild so CI/release output is reproducible.
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
ESBUILD_VERSION="0.25.11"
if [[ -x "node_modules/.bin/esbuild" ]]; then
  ESBUILD=(node_modules/.bin/esbuild)
elif command -v esbuild >/dev/null 2>&1; then
  ESBUILD=(esbuild)
else
  echo "esbuild ${ESBUILD_VERSION} is required. Run: npm ci" >&2
  exit 1
fi
"${ESBUILD[@]}" web/assets/js/app.js --minify --target=es2022 --outfile=web/assets/js/app.min.js
"${ESBUILD[@]}" web/assets/css/style.css --minify --outfile=web/assets/css/style.min.css
node --check web/assets/js/app.min.js
printf 'Built minified assets: app.min.js, style.min.css\n'
