# ELECTRUVPN BETA — Multi-Node / Multi-Server Agent Fabric

ElectruVPN can act as a **Master/Control Plane** and manage multiple remote Node/Edge servers through an outbound HTTPS Agent connection.

## Architecture

```text
                         ElectruVPN Master
                    ┌─────────────────────────┐
                    │ Panel / API / SQLite    │
                    │ Node Fabric / Jobs      │
                    └────────────┬────────────┘
                                 │ HTTPS outbound from nodes
              ┌──────────────────┼──────────────────┐
              │                  │                  │
       ┌──────▼──────┐    ┌──────▼──────┐    ┌──────▼──────┐
       │ Node DE-01  │    │ Node FI-01  │    │ Node US-01  │
       │ Agent       │    │ Agent       │    │ Agent       │
       │ Xray/s-box  │    │ Xray/s-box  │    │ Xray/s-box  │
       └─────────────┘    └─────────────┘    └─────────────┘
```

The Agent does not require an inbound SSH control channel. It authenticates to the Master using a per-node bearer token whose SHA-256 hash is stored by the Master.

## Enrollment

1. In **Pro Center → Server nodes**, create the remote node.
2. Optionally set the explicit Master Agent endpoint ending in `/api/agent`.
3. Generate an Agent Token.
4. On the remote server, from the ElectruVPN project bundle:

```bash
sudo bash scripts/install-agent.sh "https://MASTER/<base-path>/api/agent" "ONE_TIME_AGENT_TOKEN"
```

The installer stores the token in `/etc/electruvpn/agent.env` with mode `0600` and enables `electruvpn-agent.service`.

## Agent telemetry

The Agent sends:

- Agent version
- OS / architecture
- Hostname
- Persistent Agent ID
- Remote source IP as observed by the Master
- CPU usage
- Memory usage
- Disk usage
- RX/TX counters
- Last error

Heartbeat failures use exponential reconnect backoff up to 60 seconds. The Agent also reports
master TCP reachability RTT and separate used/total memory and disk capacity metrics.

## Remote operations

The Master can queue allowlisted jobs to a specific node:

- Xray restart/status
- sing-box restart/status
- BBR apply/status
- Network status
- Xray/sing-box config validation
- Xray/sing-box config deployment with SHA-256 verification
- Versioned remote config history (last 10 snapshots per engine)
- Explicit remote config rollback with validation and health-check rollback-on-failure
- Leased job queue with stale-job recovery and a three-attempt safety limit
- Agent enable/disable is independent from the node's enabled state
- Node removal cleans Agent, telemetry, queued jobs and deployment records

For config deployment, the Agent validates the configuration **before** replacing the active file. It keeps a temporary previous configuration and automatically restores it if the service restart/health check fails.

## Token lifecycle

- Generate token: one-time display.
- Rotate token: immediately invalidates the previous token.
- Disable Agent: blocks further authenticated Agent requests.
- Re-enable Agent: permits the node to reconnect using its current token.

All administrative token operations are audited.

## Security model

- HTTPS is required for explicit Agent endpoints.
- Tokens are never stored in plaintext in the Master database.
- Agent token hashes use SHA-256.
- Job targets are validated against the registered node ID.
- Job kinds are allowlisted on both Master and Agent.
- Agent result submission is bound to the target node.
- Config deployment requires a SHA-256 checksum match.
- Xray/sing-box configs are validated before application.
- Failed service health checks trigger config rollback.

## Important scope

This release provides centralized **Agent connectivity, telemetry, remote operational jobs, versioned configuration deployment and explicit rollback**. It does not automatically replicate every local database object (clients/inbounds) to every node; full client/inbound replication should be introduced as a separate versioned, node-scoped deployment workflow so a node cannot accidentally receive configuration intended for another server.
