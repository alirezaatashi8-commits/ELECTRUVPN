# V9.6+ — Syncbus resilience, DB scale, Auto Public IP

## Syncbus / network resilience
- **Bounded traffic queue** (drop + counter under back-pressure; never blocks Agent HTTP handlers)
- **Dead-Letter Queue** for failed flush samples (bounded; oldest dropped)
- **Flush timeout** (`WithTimeout`, default 8s) so a stuck DB cannot pin the worker
- **Agent retries**: traffic POST up to 3 attempts with short backoff; heartbeat uses capped exponential backoff (max 60s)
- **Single agent loop** — no extra goroutines per failure (no goroutine leak on long filter outages)
- Prometheus: `electruvpn_syncbus_dlq_len`, `electruvpn_syncbus_dlq_dropped`

## Database indexes & pooling
- `usage_snapshots (client_id, created_at)` composite (existing)
- `node_performance_snapshots (node_id, created_at)` composite (existing)
- `audit_logs.created_at` index (added)
- `tickets.created_at` / `updated_at` indexes (added)
- Postgres pool via `ELECTRU_DB_MAX_OPEN/IDLE`; SQLite open=8 for concurrent readers
- Traffic sync: bulk profile load, chunked IN queries, skip unchanged rows, batch snapshot insert

## Auto Public IP (new)
When the **server IP changes** (filter / rebuild) and public host was configured as an **IP**:
1. On panel start → detect outbound IPv4/IPv6
2. Update in-memory `PublicHost` / `PublicHostV4` / `PublicHostV6`
3. Persist to `electruvpn.env` and panel settings
4. Share links & subscriptions use the new IP automatically

**Domains are never auto-changed.** Operator sets a new domain in:
- Settings → «آدرس عمومی سرور» → دامنه جدید
- or `POST /api/pro/ops/public-host/domain` `{ "domain": "vpn.example.com" }`

Disable auto-IP: `ELECTRU_AUTO_PUBLIC_IP=0`

API: `GET /api/pro/ops/public-host` (also triggers adapt check)
