# V9.5 — Mini App, Devices, Admin RBAC

## Telegram Mini App
- URL: `{public_base}/tma/` (set subscription_base_url or public_host)
- User bot shows «داشبورد گرافیکی» WebApp button
- API `/api/tma/*` authenticated via Telegram initData HMAC
- Usage bar, share link, QR, plans, devices, reset UUID

## Devices (bot + TMA)
- List connected devices
- Revoke one / all
- Reset UUID (invalidate old configs)

## Admin RBAC (panel Telegram settings)
`adminRoles` JSON: `{"123":"owner","456":"finance","789":"support"}`
- owner: full
- finance: orders + reports
- support: tickets only
Also keep adminIds list.

