# Part 1 — Live clock fix
- web/index.html: removed duplicate `x-init="init()"` (Alpine 3 already calls init()); bumped assets to ?v=beta37.
- web/assets/js/app.js: init() guarded against double run; clock timer lives on window and also ticks on every tab load.
- internal/api/gzip.go + scripts/setup-reverse-proxy.sh: static assets now `Cache-Control: no-cache` (ETag revalidation) instead of 24h blind cache, so browsers receive the fixed app.js.
- RELEASE_SOURCE_MANIFEST.txt regenerated (install.sh verifies it).
