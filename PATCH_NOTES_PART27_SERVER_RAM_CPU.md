# PART 27 — Server-side RAM / CPU optimization (no features removed)

## API gzip middleware (`internal/api/gzip.go`)
- `gzip.Writer` is now **pooled** (`sync.Pool`) instead of allocated per API request
  (each fresh writer allocates several hundred KB → large GC churn / RSS).
- Level `BestSpeed` for dynamic JSON (much less CPU, still ~5-8x smaller).
- Compressor attached **lazily** on first body write: 204/304/bodyless responses and
  handlers that already set `Content-Length` / `Content-Encoding` are passed through
  untouched (previously they got a bogus `Content-Encoding: gzip` + empty gzip stream).
- `Flush()` flushes the compressor first, so streaming endpoints stay live.
- HEAD and `Connection: Upgrade` requests skip compression.

## Rate limiter (`internal/auth/ratelimit.go`)
- Under a flood of unique keys every request used to scan the whole 10k-entry map
  (twice) while holding the mutex. Expired-bucket sweep is now throttled to once per
  second and the fallback eviction is O(1).

## Connection leaks
- `automation.CheckDomain`: built a new `http.Transport` per call whose keep-alive
  connection was never closed; now one request (HEAD carries the TLS cert too) with
  keep-alives disabled. Halves outbound requests per domain health check.
- Webhook client (`safeWebhookClient`): per-call transport now `DisableKeepAlives`.

## Database load
- `/api/clients/online` (polled every ~10 s): selects only needed columns for clients
  and inbounds (inbound rows hold large JSON), de-duplicates inbound IDs, and runs the
  cheap in-memory "is online" check **before** the per-client permission check
  (reseller permission check used to cost a DB query per client).
- Subscription fetch device tracking: no more full-row `UPDATE` on every poll when the
  device is unchanged and was seen < 30 s ago.

Nothing was removed; API responses and DB schema are unchanged.

# PART 27b — Subscription center: accurate online / last-seen detection

Root cause: the Subscription page decided "online" from the *device last-seen time,
which is only updated when an app downloads the sub link* (usually hourly). A connected
user could show offline for hours, and a user who merely refreshed the link showed online.

- Online state now comes from the live tracker (`/online-users`: Xray online IPs + moving
  traffic counters), refreshed every 8 s while the Subscription tab is visible.
- KPI "Devices online" = real connected devices; "Last online" uses real activity.
- Each client row shows `● N dev / online now`, or `seen <date>` when offline.
- New `clients.last_active_at` column (auto-migrated, nullable): stamped by traffic sync
  whenever real traffic moves, so "last seen" survives panel restarts.
- Device last-seen write throttle is 30 s (sub fetch time stays accurate).
- Cache bust: `?v=beta54`

# PART 28 — Full debug pass (stability) + extra optimization

## Stability (panel can no longer be crashed by a background panic)
- New `internal/safe` package (`Run` / `Go` / `Supervise`). A panic in any goroutine started
  with a bare `go` terminates the whole panel process (and the Xray/sing-box it manages);
  only HTTP handlers and the Telegram update handler were protected before.
- All periodic workers (`runWorker`) now recover per tick; Telegram bots, expiry/report/
  security/survey/auto-renew/win-back loops and the honeypot are supervised (auto-restart with
  backoff after a panic). Honeypot per-connection handlers (fed by attacker traffic), ban
  helpers, login-lockout callback, speed-test probes, online-IP polling, update runner and
  `syncbus` guarded calls are panic-safe.
- `xray.PingAPI`: replaced goroutine + `cmd.Process.Kill()` (raced with process start, could
  nil-deref / leak) with `exec.CommandContext` timeout.

## Unbounded growth fixed
- `subscription_access_logs` (one row per subscription fetch) was "cleaned" only when a request
  arrived in the exact second `unix%3600==0`, i.e. never. Added a 6-hour batched retention worker
  (`ELECTRU_ACCESS_LOG_RETENTION_DAYS`, default 90, min 7).

## Performance / RAM
- `RegenerateAndRestartXray` bursts are coalesced (ticket scheme): N concurrent client/inbound
  changes (bulk create, bot purchases) now trigger one regenerate+restart instead of N serial
  ones, so live connections are dropped far less often and CPU/RAM spikes are avoided.
- Online-IP polling uses 3 concurrent `xray api` processes instead of 6 (each CLI call is a
  ~20-30 MB transient process outside GOMEMLIMIT).

## Verified (no change needed)
- `bash -n` on every script, `py_compile` on every python script, YAML parse of openapi/compose,
  `check-duplicate-routes`, `openapi-route-selftest`, `web-routing-selftest`, `node --check app.js`.
