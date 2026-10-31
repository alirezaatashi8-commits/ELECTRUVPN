# ELECTRUVPN BETA — Multi-Node Agent Fixes

## Fixed in this build

- Agent jobs now use a lease with a unique lease token.
- Expired jobs are requeued automatically; after three expired attempts they fail safely.
- Late results from an old Agent process cannot mutate a newly requeued job.
- Job completion clears the lease token atomically.
- Xray/sing-box status actions are available through the Master allowlist.
- Xray/sing-box configuration deployments create versioned Deployment records.
- Remote configuration keeps the last 10 snapshots per engine on the Node.
- Explicit Xray/sing-box rollback is now a real Agent operation with validation, restart and health-check rollback.
- Automatic rollback remains active if a new configuration fails to start or fails its health check.
- Agent enable/disable is independent from the Node enabled state.
- Node deletion removes Agent state, performance history, queued/running jobs and deployment records in one transaction.
- Agent endpoint validation rejects credentials, query strings, fragments and invalid ports.
- Agent registration and token rotation update Node + Agent records transactionally.
- Agent authentication verifies both the Master token hash and the Agent record hash.
- Agent telemetry now reports used/total RAM and disk and a TCP RTT to the Master.
- Capacity routing uses RAM total instead of comparing used RAM to itself.
- Agent reconnect backoff starts correctly and caps at 60 seconds.
- Agent heartbeat handles missing telemetry safely.
- Agent payloads above 5 MiB are rejected.
- Agent installation runs `go mod download` before compiling so a fresh source bundle can populate module checksums.
- UI no longer claims that an empty Job target means local execution; a real Node ID is required.
- Node UI displays Agent state/version separately from Node enabled state.

## Validation

Passed:

- `bash scripts/validate.sh`
- Shell syntax validation
- JavaScript syntax validation
- Python syntax validation
- Go formatting validation
- Dedicated Agent type-check using local API stubs
- ZIP integrity check

The full `go test ./...` suite still requires network/module downloads because this build environment has no Go module cache and cannot resolve `proxy.golang.org`. The installer now performs `go mod download` on a networked server before building.
