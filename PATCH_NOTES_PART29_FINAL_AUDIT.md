# PART 29 — Final audit pass (stability, no features removed)

## Fixed
- `auditAction` (platform_handler.go) started the webhook dispatcher with a bare `go`:
  - a panic inside it would terminate the whole panel (and the Xray / sing-box it manages) -> now `safe.Go`.
  - it passed `c.ClientIP()` evaluated inside the goroutine, i.e. after the HTTP handler
    returned and gin had already recycled the `*gin.Context` (data race) -> IP is now read before the goroutine starts.

## CPU: end-user subscription speedometer (sub_speedometer.go)
- Every open subscription page polled `/sub/<id>/live` every 2 s forever, and each poll could make the
  panel spawn an `xray api statsquery` process (~20-30 MB, repeated every 1.5-2 s per open page).
- Polling is now adaptive: 2 s while the user is connected, 6 s after 3 idle/offline polls,
  15 s while the panel is unreachable; paused while the tab is hidden and refreshed immediately on return.
  Live speed while connected is unchanged.

## Verified (no change needed)
- `node --check web/assets/js/app.js`, `bash -n` on every script, `py_compile` on every python script.
- All polling timers are visibility-gated and cleaned up (online tab, client live modal, dashboard, subscription, logs).
- Remaining bare goroutines already have `recover()` or are child-process pipes.
- All outbound HTTP clients have timeouts; no unbounded `io.ReadAll`.

## Not done (environment limits)
- Go toolchain and network were unavailable, so `go build`/`go test` and dependency upgrades
  (`go get -u`) were NOT run. Run on the server: `go mod tidy && go build ./... && go test ./...`
- `RELEASE_SOURCE_MANIFEST.txt` regenerated for the changed files.

## Diagnostics
- `scripts/ram-report.sh` (read-only): shows RSS of the panel, Xray and sing-box, open connections, DB/WAL size,
  journald size and the systemd memory counters, so a RAM problem can be traced to the exact process.

## Second pass
- `chain_handler.go` Delete: deleting a non-existent tunnel chain still regenerated + restarted Xray
  (dropping every live connection) and returned success even on a DB error. Now it restarts Xray only
  when a row was really deleted and returns a 500 on a DB error.
- `traffic_sync.go`: every 2 min (and on remote flush) the inbound up/down totals were rewritten for
  EVERY inbound row, including large JSON settings columns, even when nothing changed (needless SQLite/WAL
  writes). The same UPDATE now touches only inbounds whose totals actually changed (result identical;
  verified on SQLite).
- Cross-checked every function called from web/index.html against app.js: no undefined references.
- Checked: backups (daily by default, verified archive), Telegram long-poll vs HTTP timeout, SQL string
  building (only the escaped VACUUM path), defer-in-loop (none).

## Third pass — more accurate offline time
Before: a user who disconnected stayed "online" for up to ~1 minute. Reason: every connected IP was stamped with
the time of OUR poll (not Xray's), and a 45 s window kept the client online on IP presence alone.
- `internal/xray/devices.go`: IPs are now stored with the last-seen time Xray itself reports
  (`parseOnlineIPTimes` / `recordOnlineIPTimes`; implausible or missing timestamps fall back to the old
  behaviour, so every Xray build keeps working).
- New `liveDeviceOnlineFresh = 25 s`: an IP keeps a client online on its own only if Xray saw it within 25 s
  (Xray itself forgets an IP after ~20 s). The device COUNT window is 60 s so the number stays stable.
- `internal/xray/live.go`: online = traffic moved within 15 s, OR a fresh IP, OR open sing-box connections.
- Result: offline is detected about 25 s after the last activity instead of up to ~65 s. Admin pages still
  refresh every 8–12 s, and the user's own subscription page every 2 s.
- Tests added in `devices_test.go` (`go test ./internal/xray/`).
