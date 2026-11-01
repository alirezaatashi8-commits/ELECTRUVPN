# V9.12 — Security Hardening & Bugfix Pass

## Application-layer DoS / abuse
- Global rate limit: 240 req/min/IP
- TMA group: 90/min + body ≤64KB; sensitive endpoints tighter (trial 5, admin/auth 8, restore 8, buy 20, promo 30)
- Agent endpoints: 120/min, body ≤1MB
- Login: 20/min + LoginGuard (5 fails → 5 min lockout)
- Rate-limit key falls back to URL.Path when FullPath empty

## Auth / session
- TMA AdminAuth: no query-string initData (log/referrer leak), max 4KB, HMAC + 1h max age
- Non-admin denial does not echo Telegram ID
- Session TTL for TMA admin reduced to 8h
- RequireAuth accepts cookie or Bearer (TMA)
- sameOrigin CSRF relaxed only when Bearer present (not cookie-only CSRF)

## Promo integrity
- Code normalized to [A-Z0-9_-], max 64 chars
- CommitPromoUsage: row lock + atomic `used_count+1 WHERE used_count < max`

## Existing controls retained
- Metrics gated by ELECTRU_METRICS_TOKEN
- Agent auth via token hash
- Security headers (CSP, nosniff, COOP/CORP)
- Body limits on login / API / public tickets
- Trusted proxies configurable via ELECTRU_TRUSTED_PROXIES

## Ops note
Infrastructure DDoS (SYN flood, volumetric) must be handled by reverse-proxy / CDN / host firewall.
This pass hardens application-layer abuse and auth surfaces.
