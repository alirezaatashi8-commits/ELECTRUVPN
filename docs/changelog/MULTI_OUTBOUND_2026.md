# Multi-Outbound Load Balancing, Health Check & Smart Geo (2026)

**Panel version remains ELECTRU VPN BETA 1** — no version bump.

## 1. Load Balancing & Failover
- Xray `Routing.Balancers` with `roundRobin` strategy across multiple exit hosts.
- Exit candidates come from `TunnelChain.ExitAddress` + `RotateAddresses`.
- Only **healthy** exits (see health check below) are placed into the balancer.
- When the active exit fails, `ActiveExitHost` is rotated automatically and xray config is regenerated — clients keep their connection (no disconnect).
- Fallback tag guarantees at least one exit remains selectable.

## 2. Automatic Outbound Health Check
- Background worker every **90 seconds** (`cmd/server/main.go`).
- TCP connect probe to each exit host:port (timeout 4s, max RTT 800ms).
- Consecutive failure threshold (2) before marking unhealthy; 2 successes for recovery.
- In-memory health map + persistence of `LastFailAt` / `LastError` / `ActiveExitHost` on the chain.
- Telegram alerts on:
  - exit becomes unhealthy
  - exit recovers
  - automatic failover to another exit
  - all exits unhealthy
- `routing.HealthyExitHosts()` is used by `ApplyEnabledChain` so dead exits never receive new traffic.

## 3. GeoIP / GeoSite Smart Routing
- Domestic IR traffic (geoip:ir, geosite:category-ir, banks, Digikala, Snapp, Divar, Shaparak, …) → **direct**.
- Private / bogon → **block**.
- When a chain is enabled, additional rules steer:
  - AI / LLM (OpenAI, Claude, Grok/x.ai, Gemini, Perplexity, …) → chain balancer
  - Payment (PayPal, Stripe, Wise, Binance, Coinbase, …) → chain balancer
  - Gaming (Steam, Epic, Riot, EA, …) → chain balancer
- All remaining TCP/UDP → chain balancer (or single exit).

## 4. Multi-hop chaining
- Optional mid hop with dialerProxy / ProxySettings.
- Protocols on hops: VLESS (Reality/TLS), Trojan, Shadowsocks-2022, WireGuard.
- Auto node selection still uses group strategies (least_load, latency, round_robin, …).

## Operator checklist
1. Create a TunnelChain, set `ExitAddress` + optional comma-separated `RotateAddresses`.
2. Enable the chain and set `RotateOnFail=true` (default).
3. Ensure Telegram admin bot is configured for alerts.
4. Watch logs / Telegram for health and failover events.
5. No DB migration required.

## Files touched
- `internal/routing/outbound_health.go` (new)
- `internal/routing/selector.go` (extra strategies)
- `internal/xray/config.go` (Balancers, ProxySettings, enhanced geo rules)
- `internal/xray/chain.go` (healthy-only exits + balancer + smart categories)
- `cmd/server/main.go` (90s health worker)
