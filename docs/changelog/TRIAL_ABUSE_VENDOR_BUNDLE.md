# Trial anti-abuse + vendor bundle

## Trial anti-abuse
- One trial per Telegram account (existing)
- Max trials per client IP per UTC day (default 3, setting `trial_max_per_ip_day`)
- Max trials per device fingerprint (default 1, setting `trial_max_per_device`)
- TMA sends `X-Device-Fingerprint` / `deviceFp` on claim
- Trial clients forced to LimitIP=1, MaxActiveSessions=1, DeviceBindingEnabled

## Vendor bundle
- `web/assets/vendor/` includes: tailwind.js, alpine.min.js, chart.umd.min.js, qrcode.min.js
- Panel login/index already reference local paths
- TMA + portal switched from CDN to `/assets/vendor/...`
- install.sh / docker/fetch-vendor.sh remain as refresh path
