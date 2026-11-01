#!/usr/bin/env bash
# ELECTRUVPN RAM report (read-only). Run on the server:  bash scripts/ram-report.sh
# Prints which process really uses the memory so it can be fixed precisely.
set -u
hr(){ printf '\n=== %s ===\n' "$1"; }

hr "Memory / swap"
free -m 2>/dev/null || true

hr "Top 12 processes by RSS (MB)"
ps -eo pid,rss,pcpu,etimes,comm --sort=-rss 2>/dev/null | awk 'NR==1{print "PID    RSS_MB  CPU%  UPTIME_S  CMD"; next} NR<=13{printf "%-6s %-7.0f %-5s %-9s %s\n",$1,$2/1024,$3,$4,$5}'

hr "ELECTRUVPN components (RSS MB)"
for n in electruvpn xray sing-box; do
  mb=$(ps -C "$n" -o rss= 2>/dev/null | awk '{s+=$1} END{printf "%.0f", s/1024}')
  cnt=$(ps -C "$n" -o pid= 2>/dev/null | wc -l)
  printf '%-12s procs=%-3s total_rss=%s MB\n' "$n" "$cnt" "${mb:-0}"
done

hr "Open connections"
ss -s 2>/dev/null | head -4 || true

hr "Panel data"
for d in /opt/electruvpn/data /var/lib/electruvpn /etc/electruvpn; do
  [ -d "$d" ] && du -sh "$d" 2>/dev/null
done
for f in $(find /opt/electruvpn /var/lib/electruvpn -maxdepth 3 \( -name '*.db' -o -name '*.db-wal' \) 2>/dev/null); do
  ls -lh "$f" | awk '{print $5, $9}'
done

hr "Logs"
journalctl --disk-usage 2>/dev/null || true

hr "Panel service limits"
systemctl show electruvpn -p MemoryCurrent -p MemoryPeak -p MemoryMax -p Environment 2>/dev/null | sed 's/MemoryCurrent=\([0-9]*\)/MemoryCurrent=\1 bytes/' || true
echo
echo "Send this whole output back so the exact process can be tuned."
