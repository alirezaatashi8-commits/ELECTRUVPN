# Panel speed upgrade

No change to traffic accounting, limits, protocols or generated VPN configs.

- Static assets (`/assets/*.js|css|svg|json`) are now served pre-compressed
  (gzip, built once per file version, exact Content-Length, ETag/304 kept).
  ~1.2 MB of JS/CSS per cold load drops to roughly a quarter.
- The SPA shell (`index.html` / `login.html`) is gzip-compressed too (cached
  until the source or base path changes). Still `no-store`.
- `chart.umd.min.js` and `qrcode.min.js` are `defer`red so they no longer block
  first paint (they are only used by `app.js` after the DOM is ready).
- `GIN_MODE=release` set in the systemd unit and the Docker image.

Already good and left as is: API gzip, dashboard overview cache + single-flight,
paginated client lists, polling paused on hidden tabs, DB indexes.

## Known remaining cost (not changed)
`web/assets/vendor/tailwind.js` is the Tailwind Play CDN runtime: it compiles
CSS in the browser on every load (~280 KB parse + style generation). The
biggest remaining frontend win is replacing it with a prebuilt stylesheet
(Tailwind CLI, needs network/npm on a dev machine).

## sing-box log level
`info` is intentionally kept: the live per-user/device mapping parses sing-box
INFO connection lines, so lowering it would break live stats.
