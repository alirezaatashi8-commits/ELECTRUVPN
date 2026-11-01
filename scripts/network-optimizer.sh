#!/usr/bin/env bash
# Safe, idempotent TCP tuning for ELECTRUVPN. This configures the algorithm
# available in the running kernel; it does not pretend to install BBRv3.
set -euo pipefail
BACKUP_DIR=/var/lib/electruvpn
BACKUP="$BACKUP_DIR/bbr-backup.json"
CONF=/etc/sysctl.d/99-electruvpn-bbr.conf

sysctl_get() { sysctl -n "$1" 2>/dev/null || true; }
status() {
  echo "kernel=$(uname -r)"
  echo "available=$(sysctl_get net.ipv4.tcp_available_congestion_control)"
  echo "current=$(sysctl_get net.ipv4.tcp_congestion_control)"
  echo "qdisc=$(sysctl_get net.core.default_qdisc)"
  if [[ -f /etc/electruvpn/bbrv3-kernel-installed ]]; then
    echo "bbrv3=confirmed-marker"
  else
    echo "bbrv3=not-verified"
  fi
}
require_root() { [[ ${EUID:-$(id -u)} -eq 0 ]] || { echo 'root required' >&2; exit 1; }; }
apply() {
  require_root
  local available current qdisc mtu
  available="$(sysctl_get net.ipv4.tcp_available_congestion_control)"
  if ! grep -qw bbr <<<"$available"; then
    modprobe tcp_bbr >/dev/null 2>&1 || true
    available="$(sysctl_get net.ipv4.tcp_available_congestion_control)"
  fi
  if ! grep -qw bbr <<<"$available"; then
    echo "BBR is not available in the running kernel; no settings changed." >&2
    return 2
  fi
  mkdir -p "$BACKUP_DIR" /etc/sysctl.d
  chmod 0750 "$BACKUP_DIR"
  current="$(sysctl_get net.ipv4.tcp_congestion_control)"
  qdisc="$(sysctl_get net.core.default_qdisc)"
  mtu="$(sysctl_get net.ipv4.tcp_mtu_probing)"
  if [[ ! -f "$BACKUP" ]]; then
    printf '{"congestion_control":"%s","default_qdisc":"%s","tcp_mtu_probing":"%s"}\n' "$current" "$qdisc" "$mtu" > "$BACKUP"
    chmod 0600 "$BACKUP"
  fi
  # Apply settings one by one; don't persist unless all runtime changes work.
  sysctl -w net.core.default_qdisc=fq
  sysctl -w net.ipv4.tcp_congestion_control=bbr
  sysctl -w net.ipv4.tcp_mtu_probing=1
  cat > "$CONF.tmp" <<'CFG'
# Managed by ELECTRUVPN Network Optimizer.
# Uses the kernel-registered BBR implementation (v1 or v3 depending on kernel).
net.core.default_qdisc=fq
net.ipv4.tcp_congestion_control=bbr
net.ipv4.tcp_mtu_probing=1
CFG
  chmod 0644 "$CONF.tmp"
  mv "$CONF.tmp" "$CONF"
  status
}
rollback() {
  require_root
  [[ -f "$BACKUP" ]] || { echo 'no backup'; exit 1; }
  local cc q mtu
  cc=$(sed -n 's/.*"congestion_control":"\([^"]*\)".*/\1/p' "$BACKUP")
  q=$(sed -n 's/.*"default_qdisc":"\([^"]*\)".*/\1/p' "$BACKUP")
  mtu=$(sed -n 's/.*"tcp_mtu_probing":"\([^"]*\)".*/\1/p' "$BACKUP")
  [[ -n "$q" ]] && sysctl -w net.core.default_qdisc="$q"
  [[ -n "$cc" ]] && sysctl -w net.ipv4.tcp_congestion_control="$cc"
  [[ -n "$mtu" ]] && sysctl -w net.ipv4.tcp_mtu_probing="$mtu"
  rm -f "$CONF"
  sysctl --system >/dev/null 2>&1 || true
  rm -f "$BACKUP"
  status
}
# ---------- high-concurrency kernel headroom (raise-only, reversible) ----------
HC_CONF=/etc/sysctl.d/98-electruvpn-highconn.conf
HC_BACKUP="$BACKUP_DIR/highconn-backup.env"
# key:value pairs. Numeric keys are only ever RAISED, never lowered.
HC_RAISE_KEYS=(
  "net.core.somaxconn:65535"
  "net.core.netdev_max_backlog:16384"
  "net.ipv4.tcp_max_syn_backlog:16384"
  "net.core.rmem_max:7500000"
  "net.core.wmem_max:7500000"
  "fs.file-max:2097152"
  "fs.nr_open:1048576"
)
sysctl_path() { echo "/proc/sys/${1//./\/}"; }
apply_highconn() {
  require_root
  mkdir -p "$BACKUP_DIR" /etc/sysctl.d
  chmod 0750 "$BACKUP_DIR"
  local entry key want cur lines="" n_applied=0
  local mem_kb; mem_kb="$(awk '/^MemTotal:/ {print $2}' /proc/meminfo 2>/dev/null || echo 0)"
  local list=("${HC_RAISE_KEYS[@]}")
  # Conntrack only matters when the module is loaded; skip tiny VPSs.
  if [[ -e /proc/sys/net/netfilter/nf_conntrack_max && "${mem_kb:-0}" -ge 1500000 ]]; then
    list+=("net.netfilter.nf_conntrack_max:262144")
  fi
  : > "$HC_CONF.tmp"
  [[ -f "$HC_BACKUP" ]] || : > "$HC_BACKUP"
  for entry in "${list[@]}"; do
    key="${entry%%:*}"; want="${entry##*:}"
    [[ -e "$(sysctl_path "$key")" ]] || continue
    cur="$(sysctl_get "$key")"
    [[ "$cur" =~ ^[0-9]+$ ]] || continue
    if (( cur >= want )); then continue; fi
    grep -q "^$key=" "$HC_BACKUP" || echo "$key=$cur" >> "$HC_BACKUP"
    if sysctl -w "$key=$want" >/dev/null 2>&1; then
      echo "$key=$want" >> "$HC_CONF.tmp"
      n_applied=$((n_applied+1))
    fi
  done
  # Long-lived VPN streams: do not collapse cwnd after short idle gaps.
  if [[ -e /proc/sys/net/ipv4/tcp_slow_start_after_idle ]]; then
    cur="$(sysctl_get net.ipv4.tcp_slow_start_after_idle)"
    if [[ "$cur" != "0" ]]; then
      grep -q '^net.ipv4.tcp_slow_start_after_idle=' "$HC_BACKUP" || echo "net.ipv4.tcp_slow_start_after_idle=$cur" >> "$HC_BACKUP"
      if sysctl -w net.ipv4.tcp_slow_start_after_idle=0 >/dev/null 2>&1; then
        echo "net.ipv4.tcp_slow_start_after_idle=0" >> "$HC_CONF.tmp"
        n_applied=$((n_applied+1))
      fi
    fi
  fi
  if (( n_applied == 0 )); then
    rm -f "$HC_CONF.tmp"
    [[ -s "$HC_BACKUP" ]] || rm -f "$HC_BACKUP"
    echo "highconn: kernel already has equal or higher limits; nothing changed."
    return 0
  fi
  { echo "# Managed by ELECTRUVPN Network Optimizer (high-concurrency headroom)."; \
    echo "# Raise-only values; undo with: electruvpn-network-optimizer rollback-highconn"; \
    cat "$HC_CONF.tmp"; } > "$HC_CONF.new"
  rm -f "$HC_CONF.tmp"
  chmod 0644 "$HC_CONF.new"
  mv "$HC_CONF.new" "$HC_CONF"
  chmod 0600 "$HC_BACKUP"
  echo "highconn: applied $n_applied setting(s)."
  highconn_status
}
highconn_status() {
  local k
  for k in net.core.somaxconn net.core.netdev_max_backlog net.ipv4.tcp_max_syn_backlog \
           net.core.rmem_max net.core.wmem_max fs.file-max fs.nr_open \
           net.ipv4.tcp_slow_start_after_idle net.netfilter.nf_conntrack_max; do
    [[ -e "$(sysctl_path "$k")" ]] && echo "$k=$(sysctl_get "$k")"
  done
  [[ -f "$HC_CONF" ]] && echo "highconn=active" || echo "highconn=not-applied"
}
rollback_highconn() {
  require_root
  if [[ -f "$HC_BACKUP" ]]; then
    local line k v
    while IFS= read -r line; do
      [[ "$line" =~ ^[a-z0-9_.]+=[0-9]+$ ]] || continue
      k="${line%%=*}"; v="${line#*=}"
      sysctl -w "$k=$v" >/dev/null 2>&1 || true
    done < "$HC_BACKUP"
    rm -f "$HC_BACKUP"
  fi
  rm -f "$HC_CONF"
  sysctl --system >/dev/null 2>&1 || true
  highconn_status
}

case "${1:-status}" in
  status) status ;;
  apply) apply ;;
  rollback) rollback ;;
  apply-highconn) apply_highconn ;;
  highconn-status) highconn_status ;;
  rollback-highconn) rollback_highconn ;;
  *) echo 'usage: network-optimizer.sh {status|apply|rollback|apply-highconn|highconn-status|rollback-highconn}' >&2; exit 2 ;;
esac
