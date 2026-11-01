# DNS Provider Selection — ELECTRUVPN BETA 1

## What it does
Operators can pick **Cloudflare (1.1.1.1)**, **Google (8.8.8.8)**, **Quad9**, **System**, or a **custom DoH/DoT list** in panel Settings. The choice is stored in `panel_settings` and applied to **Xray + sing-box** DNS so tunnel name resolution prefers that provider.

Iranian domains (`.ir` / geosite:category-ir) still prefer **localhost** first (split DNS).

## API
- `GET  /api/pro/dns` — current provider + effective resolvers + presets
- `POST /api/pro/dns` — `{ "provider": "cloudflare"|"google"|"quad9"|"system"|"custom"|"auto", "customResolvers": "...", "applyNow": true }`

On save with `applyNow: true`, engines regenerate/restart so changes take effect without a full panel process restart when possible.

## UI
Panel → **Settings** → **Xray / Sing-box Defaults** → **DNS Provider (تونل)** → Apply.

## Notes
- This is **server-side** DNS inside the VPN core, not the panel web UI browser DNS.
- Traffic accounting is unaffected by DNS provider settings.
- Env `ELECTRU_DNS_RESOLVERS` remains the fallback when provider is `auto`.
