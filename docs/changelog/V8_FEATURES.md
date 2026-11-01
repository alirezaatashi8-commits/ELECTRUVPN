# ELECTRUVPN BETA1 — V8 feature pass

## New
- **`electruvpn` CLI** (installed to `/usr/local/bin/electruvpn`): interactive menu plus `status`, `url`,
  `start|stop|restart`, `logs`, `port`, `path`, `resetpw`, `backup`, `geo`, `metrics`, `update`.
- **Geo data auto-update**: `scripts/geo-update.sh` + weekly `electruvpn-geo-update.timer`. Default source
  is Chocolate4U/Iran-v2ray-rules (adds `geoip:ir`, `geosite:category-ir`), falling back to the standard
  Loyalsoldier build. Downloads are size/SHA-256 checked and replaced atomically; existing files are
  untouched on failure. `ELECTRU_GEO_SOURCE=standard` switches source.
- **Prometheus `/metrics`**: off by default; `electruvpn metrics enable` generates a token. Aggregate
  counters only (no client names, emails, IPs or links).
- **Update command**: `electruvpn update <zip|https-url> [sha256]` backs up the DB and runs the new
  release's installer, which keeps the previous runtime and rolls back if the new service is unhealthy.
- **3x-ui import**: `python3 scripts/import-3xui.py /path/x-ui.db out/` writes one importable JSON per
  inbound (vless/vmess/trojan/shadowsocks) with clients, quotas, expiries and usage. Unsupported items
  are reported as warnings, never silently converted.
- **Docker**: `Dockerfile`, `docker-compose.yml`, `docker/entrypoint.sh` (secrets and hidden base path are
  generated once and kept in the `/data` volume).

## Fixed
- `.github/workflows/ci.yml` was invalid YAML (the OpenAPI step); GitHub would have rejected the whole
  workflow. CI now also runs `go mod tidy`, `go build`, a docker build, a real per-file `bash -n` loop
  (the old `bash -n install.sh scripts/*.sh` only checked install.sh), Python syntax and the release audit.
- `release-audit.sh` expects `go 1.27.1`.

## Not verified in the authoring environment
No Go toolchain or network was available: `internal/api/metrics.go` and the router line were reviewed by
hand and by static checks but not compiled. Run CI (or `go build ./... && go vet ./...`) first.
The Docker image and the geo download URLs were not built/fetched here either.

## V8.1 hotfix
- Removed unreachable `return false, ""` after the complete switch in `evaluateRule` (`internal/automation/scheduler.go`). This was flagged by `go vet -unreachable` and would have been dead code forever.
- Bumped panel version constant to `ELECTRUVPN BETA1 V8.1`.
- Regenerated complete `go.sum` for faster/reliable `go mod` on first install.
- Re-verified release-source manifest, `scripts/validate.sh`, `scripts/release-audit.sh`, shell syntax, frontend syntax and routing matrix.

## V8.1 UI/UX pass
- Redesigned **Inbound create/edit modal** as a proper wizard: sticky header + progress steps (Basic → Transport → Security → Review), wider panel, sticky action footer, Escape-to-close, click-outside-to-close, smoother open animation.
- Clearer page title on the Inbounds tab and stronger primary “New Inbound” CTA.
- Global form polish: consistent focus rings, `.field` styles, section cards, review summary helpers, improved sidebar hover motion and KPI card lift.

## V8.2 Anti-Filter suite
- **TLS Fragment** stored on inbound and advertised in share links (`fragment=tlshello,100-200,10-20`) for Hiddify / modern clients.
- **Mux** settings on inbound + link metadata.
- **Iran-aware xray routing** by default: `geoip:ir` + `geosite:category-ir` → direct, private → block.
- **Encrypted DNS** defaults: Cloudflare + Google + Quad9 DoH.
- **Multi-SNI / multi shortId REALITY** on quick creates; SpiderX support.
- **Rotate REALITY keys** API + UI action menu item.
- **One-click Anti-Filter** inbound (VLESS + REALITY + Vision + Fragment + Mux).
- UI: Fragment/Mux form section, anti-filter preset, Anti-Filter toolbar button.
