# PART 26 — Full project disk + RAM optimization

## System / disk
- journald `SystemMaxUse`: 64M → **32M**
- journald `RuntimeMaxUse`: 32M → **16M**
- log vacuum default `ELECTRU_LOG_MAX_SIZE`: 64M → **32M**
- systemd panel service: `GOMEMLIMIT=384MiB`, `GOGC=40` (Go reclaims memory more aggressively)

## Frontend RAM
- Leaving **inbounds**: clear all nested `clients[]`, collapse expand, clear virt cache
- Leaving **subscription**: clear `clients` + `logs` arrays (reload on next open)
- Subscription client fetch: pages of 200, hard cap **1000** (was 2000)
- Inbound map preload in sub center: max ~300 inbounds
- netHistory: **12** points
- Xray log buffer: **80** lines
- Live overview poll: **12s**; heavy batch: **180s**; logs: **8s**

## Notes
- Xray/Sing-box child processes are separate; GOMEMLIMIT applies to the panel binary only.
- After update, apply journald drop-in if not already linked:
  `cp deploy/90-electruvpn-journald.conf /etc/systemd/journald.conf.d/ && systemctl restart systemd-journald`
- Cache bust: `?v=beta53`
