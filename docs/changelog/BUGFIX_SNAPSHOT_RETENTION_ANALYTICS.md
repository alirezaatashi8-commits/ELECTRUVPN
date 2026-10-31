# Bug-fix pass: usage snapshots, analytics, retention

1. `usage_snapshots` was never pruned (grows without bound on large fleets).
   New `database.PruneUsageSnapshots` runs every 6h in small batches; keep
   period via `ELECTRU_SNAPSHOT_RETENTION_DAYS` (default 90, min 30). Client
   usage counters are not touched.
2. Multi-node flush (`FlushRemoteTrafficDeltas`) documented "at most one
   snapshot per hour" but did not enforce it, writing a row per active client
   on every 30s flush. The hourly rule is now enforced.
3. Inbound Analytics used SQLite-only `strftime()`; on PostgreSQL it failed
   silently and returned no data. Now uses `to_char()` on PostgreSQL and
   returns an error instead of hiding query failures.
4. `/pro/usage/snapshots` used `ORDER BY created_at ASC LIMIT 2000`, i.e. the
   OLDEST 2000 rows; it now returns the newest 2000, oldest-to-newest.

Not changed (documented limit): WireGuard client addresses come from a fixed
`10.77.<inboundId%250+1>.x/32` pool, so one WireGuard inbound holds at most
253 clients and inbound IDs differing by 250 share a subnet.
