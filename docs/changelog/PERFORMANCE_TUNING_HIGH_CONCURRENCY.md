# High-concurrency resource tuning

Scope: infrastructure limits only. No Go source, traffic accounting, limits or
speed behaviour was changed.

## What changed
- `deploy/electruvpn.service`: `LimitNOFILE` 65535 -> 1048576 and `TasksMax`
  512 -> 16384. Both apply to the whole unit, including the xray and sing-box
  child processes, so the old values were a real ceiling under many users.
- `scripts/network-optimizer.sh`: new `apply-highconn`, `highconn-status`,
  `rollback-highconn`. Writes `/etc/sysctl.d/98-electruvpn-highconn.conf`.
  Numeric values are raise-only (never lowered), the previous values are saved
  to `/var/lib/electruvpn/highconn-backup.env` and restored on rollback.
  Covers listen/SYN backlog, NIC backlog, UDP/QUIC socket buffers (Hysteria2,
  TUIC), file handles, conntrack (only on >= ~1.5 GB RAM) and
  `tcp_slow_start_after_idle=0`.
- `install.sh`: runs `apply-highconn` after the BBR step (failure is non-fatal).
- `docker-compose.yml`: `nofile` ulimit 1048576 for the panel container.
- `.env.example`: documented optional `GOMEMLIMIT` / `GOGC`.

## Verify on the server
    systemctl show electruvpn -p LimitNOFILE -p TasksMax
    electruvpn-network-optimizer highconn-status
    cat /proc/$(pgrep -f xray | head -1)/limits | grep "open files"

## Undo
    electruvpn-network-optimizer rollback-highconn
