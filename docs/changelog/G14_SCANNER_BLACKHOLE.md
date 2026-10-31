# G14 — Scanner IP Blackhole

Global L7 middleware (`l7shield.DefaultBlackhole`) runs before general rate limits.

## Behavior
- Detects probe paths (`.env`, `wp-admin`, `phpmyadmin`, IoT CGI, etc.)
- Never classifies `/api`, `/sub`, `/tma`, `/portal`, `/assets` as scanner paths
- After 2 scanner hits in 5 minutes → ban IP for 2 hours (tarpit + empty body)
- Banned IPs get 403 + short delay on all panel HTTP traffic

## Admin API
- `GET /api/pro/l7shield/stats` — includes `blackhole` counters
- `GET /api/pro/l7shield/blackhole` — active bans list
- `POST /api/pro/l7shield/blackhole/ban` — `{ip, reason?, hours?}`
- `POST /api/pro/l7shield/unblock` — clears shield + blackhole for IP

Note: this protects the **panel HTTP surface**, not Xray/data-plane ports (use firewall/fail2ban there).
