#!/usr/bin/env bash
# Read-only Xray config preflight. Does not print private keys or client IDs.
set -euo pipefail

if [[ $# -ne 1 || ! -r "$1" ]]; then
  echo "Usage: $0 /path/to/xray-config.json" >&2
  exit 2
fi
CONFIG=$1
python3 - "$CONFIG" <<'PY'
import json, sys
from pathlib import Path

path = Path(sys.argv[1])
try:
    data = json.loads(path.read_text())
except Exception as exc:
    print(f"FAIL: config is not valid JSON ({exc})")
    raise SystemExit(1)

inbounds = data.get("inbounds")
if not isinstance(inbounds, list):
    print("FAIL: top-level 'inbounds' must be an array")
    raise SystemExit(1)

known_networks = {"raw", "tcp", "ws", "grpc", "xhttp", "httpupgrade", "kcp", "mkcp", "quic", "http", "domainsocket"}
issues = 0
print(f"Config JSON: valid; inbounds: {len(inbounds)}")
for i, inbound in enumerate(inbounds):
    if not isinstance(inbound, dict):
        print(f"Inbound #{i+1}: FAIL (not an object)"); issues += 1; continue
    port = inbound.get("port")
    protocol = str(inbound.get("protocol", "unknown"))
    stream = inbound.get("streamSettings") or {}
    security = str(stream.get("security", "none")).lower()
    network = str(stream.get("network", "tcp")).lower()
    label = str(inbound.get("tag") or f"#{i+1}")[:48]
    problems = []
    if not isinstance(port, int) or isinstance(port, bool) or not 1 <= port <= 65535:
        problems.append("invalid port")
    if network not in known_networks:
        problems.append("unrecognized network name")
    if security == "reality":
        reality = stream.get("realitySettings") or {}
        if not isinstance(reality, dict):
            problems.append("realitySettings must be an object")
        else:
            if not reality.get("privateKey"):
                problems.append("REALITY privateKey missing")
            names = reality.get("serverNames")
            if not isinstance(names, list) or not any(isinstance(n, str) and n.strip() for n in names):
                problems.append("REALITY serverNames missing")
            if network not in {"raw", "tcp", "xhttp", "grpc"}:
                problems.append("REALITY may be incompatible with this transport; verify against installed Xray docs")
    elif security == "tls":
        tls = stream.get("tlsSettings") or {}
        if not isinstance(tls, dict):
            problems.append("tlsSettings must be an object")
    status = "WARN" if problems else "OK"
    print(f"{status}: {label} | protocol={protocol} | port={port if isinstance(port, int) else 'invalid'} | network={network} | security={security}")
    for problem in problems:
        print(f"  - {problem}")
    issues += bool(problems)

xray_test = None
import shutil, subprocess
xray = shutil.which("xray")
if xray:
    try:
        result = subprocess.run([xray, "run", "-test", "-config", str(path)], text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=20)
        print("Xray config test: " + ("PASS" if result.returncode == 0 else "FAIL"))
        if result.returncode != 0:
            print("  Xray reported a config error; inspect locally (output may contain deployment details).")
            issues += 1
    except Exception as exc:
        print(f"Xray config test: unavailable ({type(exc).__name__})")
else:
    print("Xray config test: SKIPPED (xray binary not found)")

print("Preflight result: " + ("PASS" if not issues else f"REVIEW REQUIRED ({issues} issue(s))"))
raise SystemExit(1 if issues else 0)
PY
