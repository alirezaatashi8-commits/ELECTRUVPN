# V9.6 — Multi-Node State Sync & High-Concurrency Hardening

## 1. Lightweight state / messaging (no external broker)

Agents still connect **outbound HTTPS** only (same security model). On the Master:

| Component | Role |
|-----------|------|
| `internal/syncbus` | In-process bus: node registry + bounded traffic-delta queue |
| `POST /api/agent/heartbeat` | Updates DB **and** `syncbus` node snapshot (CPU/mem/disk/RTT/health) |
| `POST /api/agent/traffic` | Accepts compact `{email: [up, down]}` batches; enqueues without row-level DB writes |
| Worker every 15s | `FlushRemoteTrafficDeltas` → merge max counters → one batched `SyncTrafficWithStats` |
| Worker every 2m | Prune stale node snapshots (>10m) |

This is the practical equivalent of a light message bus for ElectruVPN’s agent model without introducing NATS/gRPC infrastructure on every edge node.

## 2. Agent 1.4.0

- Shared `http.Transport` (connection reuse)
- After each successful heartbeat, reports local Xray `statsquery` to `/api/agent/traffic` when Xray is present
- Capability flag `trafficReport: true`

## 3. Traffic sync at scale

- Bulk profile-subject load (eliminates N+1)
- Chunked snapshot queries (800 IDs)
- Skip `UPDATE` when counters unchanged
- Bulk disable for expired/over-quota clients
- `CreateInBatches` for usage snapshots
- Remote + local paths share one mutex-serialized apply

## 4. Connection pool & observability

- Postgres: existing `ELECTRU_DB_MAX_OPEN` / `IDLE` / lifetime
- SQLite: open 8 / idle 4 + idle lifetime (readers under concurrent subs)
- Prometheus: `electruvpn_syncbus_*`, goroutines, heap alloc/objects

## 5. Routing

`nodeLooksHealthy` reads the in-memory bus first (90s freshness) before hitting AgentNode rows — lowers DB load on high-rate smart subscriptions.
