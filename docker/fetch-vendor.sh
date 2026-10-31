#!/bin/sh
# Downloads the browser libraries the panel UI needs into web/assets/vendor
# (same files and fallbacks as install.sh). Used by the Docker build.
set -eu
OUT="${1:?usage: fetch-vendor.sh <vendor-dir>}"
mkdir -p "$OUT"
get() {
  dest="$1"; shift
  for url in "$@"; do
    echo "fetching $url"
    if curl --retry 5 --retry-delay 2 --connect-timeout 10 --max-time 120 -fsSL "$url" -o "$dest.tmp" \
       && [ "$(wc -c < "$dest.tmp")" -gt 1000 ]; then
      mv -f "$dest.tmp" "$dest"; return 0
    fi
    rm -f "$dest.tmp"
  done
  echo "FAILED to fetch $dest" >&2; return 1
}
get "$OUT/tailwind.js" \
  "https://cdn.jsdelivr.net/npm/@tailwindcss/browser@4" \
  "https://unpkg.com/@tailwindcss/browser@4"
get "$OUT/alpine.min.js" \
  "https://cdn.jsdelivr.net/npm/alpinejs@3.14.1/dist/cdn.min.js" \
  "https://cdnjs.cloudflare.com/ajax/libs/alpinejs/3.14.1/cdn.min.js"
get "$OUT/chart.umd.min.js" \
  "https://cdn.jsdelivr.net/npm/chart.js@4.4.3/dist/chart.umd.min.js" \
  "https://cdnjs.cloudflare.com/ajax/libs/Chart.js/4.4.3/chart.umd.min.js"
get "$OUT/qrcode.min.js" \
  "https://cdn.jsdelivr.net/gh/davidshimjs/qrcodejs@master/qrcode.min.js" \
  "https://cdnjs.cloudflare.com/ajax/libs/qrcodejs/1.0.0/qrcode.min.js"
echo "vendor libraries ready"
