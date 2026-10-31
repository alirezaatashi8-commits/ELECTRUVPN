# Part 9 — Real live stats (online / devices / speed) for every protocol + subscription speedometer

## Root cause of "everything is 0"
- internal/xray/config.go: the Xray config had a local `api` dokodemo inbound but **no routing rule for it**, and the first
  routing rule blocks `geoip:private`. `xray api statsquery` / `statsonlineiplist` therefore never reached the StatsService, so
  online users, speeds and per-client counters stayed at 0. A first rule `inboundTag api -> outboundTag api` is now generated
  (privacy_test updated to assert it). **Existing installs need one config regeneration / xray restart** (save any inbound or restart
  the panel) to pick it up.

## Live layer (all protocols)
- internal/core/live.go (new): `Server.RefreshLive()` — one shared, rate-limited sampler (1.5 s) used by dashboard, online users,
  client live, inbound live and the public subscription endpoint.
- internal/xray/devices.go (new): connected-device tracking. `statsUserOnline` policy enabled; online IPs are read with
  `xray api statsgetallonlineusers` / `statsonlineiplist` (both CLI argument styles are tried; silently skipped when the installed
  Xray build lacks them). IPs are kept in memory only; only counts are exposed.
- internal/xray/live.go: `LiveSample` now has `devices` and `connections`; a client with a connected device counts as online even
  when no counter moved in the last seconds.
- internal/singbox/live.go (new) + config.go + manager.go: Hysteria2 / TUIC / Naive / AnyTLS / ShadowTLS / WireGuard are now live too.
  sing-box gets a loopback-only Clash API (`ELECTRU_SINGBOX_API_PORT`, default 10086, secret derived from the JWT secret); the panel polls
  `/connections` for per-inbound speed, open connections and distinct source IPs, and matches connections to users through
  sing-box's own "[user] inbound connection from IP" log line. TUIC users now carry `name = email` so they can be identified.
  If the installed sing-box build rejects the Clash API block, the panel falls back to a plain config and disables sing-box live stats
  instead of failing to start (core.generateValidatedSingBox).

## Admin UI
- Inbound Live modal: works for every protocol; shows devices connected, open connections, inbound speed, and devices / limit per client.
- Client Live modal: devices connected (with IP limit) and open connections.
- Online tab: new "Live devices" column and total; assets `?v=beta45`.

## Subscription page speedometer
- New public endpoint `GET /sub/:subId/live` (rate limited, token-gated like the sub page): returns only the client's own
  `downloadSpeed`, `uploadSpeed` (bytes/s), `devices`, `connections`, `online`. No IPs.
- The subscription landing page now has two Speedtest-style dials (download / upload): red at low speed, turning to green as speed
  rises, animated needle, digital KB/s or MB/s readout, online badge and device count; refreshes every 2 s while the tab is visible.
- Code: internal/api/handlers/sub_speedometer.go.

## Tests
- internal/xray/devices_test.go, internal/singbox/live_test.go (new); privacy_test updated. The stdlib-only live/devices packages were run with
  `go test -race`; the remaining Go packages were gofmt/syntax-checked only (the full module needs Go 1.27.1 + module downloads).

## Notes / limits
- Xray has no per-connection API: "devices" = distinct source IPs Xray reported in the last ~45 s; a device that stays connected but fully
  idle for longer can drop out until it sends a new connection. sing-box protocols report real open connections.
- Multi-node (agent) servers: live stats cover the panel host's own Xray/sing-box; remote nodes are still accounted for traffic only.
