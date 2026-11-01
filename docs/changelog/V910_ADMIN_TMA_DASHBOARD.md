# V9.10 — Admin Telegram WebApp Dashboard

## Flow
1. Admin opens **🖥 پنل وب مدیریت** in admin bot (WebApp button).
2. Mini App loads `/tma/admin/` and POSTs `initData` to `/api/tma/admin/auth`.
3. Backend verifies HMAC with **admin bot token** (fallback: user bot token),
   checks Telegram ID is `owner`/`admin`, issues panel JWT (cookie + bearer, 12h).
4. SPA calls existing `/api/*` routes with `Authorization: Bearer` + cookies.

## Security
- Telegram WebApp initData HMAC-SHA256 validation
- Role gate: only owner/admin Telegram IDs
- Session recorded in PanelSession
- RequireAuth accepts cookie or Bearer
- CSRF same-origin still applies for cookie mutations; Bearer works for TMA

## Requirements
- `public_host` or `subscription_base_url` set to HTTPS panel URL
- Admin Telegram ID in `telegram_admin_ids` / `telegram_admin_roles`
- Admin bot token configured
- At least one panel `owner` user exists

## Files
- `web/tma/admin/index.html` — mobile admin SPA
- `internal/api/handlers/tma_admin_auth.go`
- `internal/auth/middleware.go` — Bearer support
- `internal/telegram/admin_bot.go` — WebApp button
