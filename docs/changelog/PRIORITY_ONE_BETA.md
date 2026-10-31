# ELECTRUVPN BETA — Priority One Control Plane

This increment adds the first four architectural priorities without changing the existing client traffic model or imposing bandwidth/speed limits.

## 1. Core / Control Plane safety
- Durable `OperationRecord` with unique operation ID and optional idempotency key.
- Operation phases: queued → preflight/executing → verified, or rollback/failed/cancelled.
- Platform jobs and config deployments receive operation IDs.
- Agent job lease expiry also closes the linked operation safely.
- Safe cancellation endpoint for unfinished control-plane operations.
- Existing config validation, versioned snapshots and agent rollback remain the final safety boundary.

## 2. Domain & SSL foundation
- `DomainRecord` with primary/backup priority, health state, failure counter and latency.
- HTTPS health checks with public-DNS validation and redirects disabled.
- Repeated failures are required before automatic domain failover.
- Automatic failover selects only a currently healthy enabled domain.
- Certificate revisions track fingerprint, issuer, expiry and active/superseded state.
- Existing Let's Encrypt/certbot certificates under `/etc/letsencrypt` are renewed individually by lineage name when due.
- Certificate changes are fingerprinted before a new revision is stored.

This layer intentionally does not silently rewrite client subscriptions or perform CDN/Domain-Fronting changes. Failover changes the control-plane primary domain state only; downstream distribution remains an explicit operation.

## 3. Node Fabric
- Node groups with health/latency/capacity/weighted strategy metadata.
- Group membership with priority and weight.
- Routing selection can be restricted to a node group.
- Node drain state prevents new routing selection while allowing the node to remain enabled for maintenance/recovery.

## 4. Agent
- Agent capability discovery is reported in heartbeat telemetry.
- Agent version is now 1.3.0.
- Remote diagnostics job checks Xray, sing-box, disk, memory, routes and BBR state.
- Diagnostics are queued through the same leased job/operation system.
- Existing token rotation, lease protection, config snapshots and rollback remain active.

## Validation
- `scripts/validate.sh`: passed.
- `node --check web/assets/js/app.js`: passed.
- `gofmt -d`: clean.
- Full `go test ./...` cannot be completed in the offline build environment because the Go module cache/go.sum dependencies are unavailable. The installer remains responsible for `go mod download` on a networked server.
