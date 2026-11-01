# V8.6 — Smart Load Balancing & Failover

## Behaviour

### Failover
- Nodes with `drainState=drain|draining` are skipped.
- Agent `healthState` unhealthy / down is skipped when `healthFilter=healthy`.
- If every preferred node is down and `failover=true`, one least-bad enabled node is still returned so clients are not left without a link.

### Load balancing strategies (`NodeGroup.strategy`)
| Strategy | Ranking |
|----------|---------|
| `health` (default) | Healthy first, then priority, then weight |
| `latency` | Lower RTT from performance snapshots ranks higher |
| `least_load` | Lower CPU/memory utilization ranks higher |
| `weighted` | Explicit member weights |
| `priority` | Lower priority number wins |

### Where it applies
1. **Subscription groups** (`/sub-group/:token`) — uses group's `nodeGroupId`, `healthFilter`, and the group's strategy/failover flags.
2. **Single client subscription** when `subscription_smart=true` — appends links for up to 8 healthy ranked nodes.
3. **Preview API** — `GET /api/pro/platform/node-selection?groupId=&strategy=health&healthFilter=healthy`

### Subscription extras in this release
- Group sub supports `format=encrypted&password=...` (Argon2id envelope).
- Group JSON response includes `strategy` and `healthyNodes`.
- Smart single-sub de-duplicates node links.

## Operator checklist
1. Register nodes (ServerNode + optional Agent).
2. Create a Node Group with strategy e.g. `least_load` or `latency`.
3. Add members with priority/weight.
4. Create Subscription Group with `nodeGroupId` + `healthFilter=healthy` + `smartEnabled=true`.
5. Assign clients to that subscription group name.

