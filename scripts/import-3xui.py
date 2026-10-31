#!/usr/bin/env python3
"""Convert a 3x-ui / x-ui SQLite database into ELECTRUVPN inbound import files.

    python3 scripts/import-3xui.py /etc/x-ui/x-ui.db [output-dir]

One JSON file is written per supported inbound (vless, vmess, trojan,
shadowsocks). In the panel open Inbounds -> Import and select the files one by
one. The source database is opened read-only and never modified.

Everything the panel cannot represent is reported as a WARNING instead of being
silently converted, so review the summary before importing.
"""
import json
import math
import os
import secrets
import sqlite3
import string
import sys
from urllib.parse import quote

GB = 1024 ** 3
SUPPORTED = {"vless", "vmess", "trojan", "shadowsocks"}
ALNUM = string.ascii_lowercase + string.digits

warnings = []


def warn(msg):
    warnings.append(msg)
    print("WARNING: " + msg)


def jload(raw, default):
    if raw is None or raw == "":
        return default
    try:
        v = json.loads(raw)
        return v if v is not None else default
    except (TypeError, ValueError):
        return default


def to_gb(num_bytes):
    num_bytes = int(num_bytes or 0)
    if num_bytes <= 0:
        return 0
    return max(1, math.ceil(num_bytes / GB))


def pos_ms(value, label):
    value = int(value or 0)
    if value < 0:
        warn(f"{label}: 'start counting after first use' expiry is not supported; imported as no expiry")
        return 0
    return value


def rand_token(n=16):
    return "".join(secrets.choice(ALNUM) for _ in range(n))


def convert_stream(ib, label):
    stream = jload(ib["stream_settings"], {})
    proto = ib["protocol"]
    net = (stream.get("network") or "tcp").lower()
    sec = (stream.get("security") or "none").lower()
    out = {}

    mapped = {"tcp": "tcp", "raw": "tcp", "ws": "ws", "grpc": "grpc", "httpupgrade": "httpupgrade",
              "xhttp": "xhttp", "splithttp": "xhttp", "kcp": "mkcp", "mkcp": "mkcp"}.get(net)
    if mapped is None:
        warn(f"{label}: transport '{net}' is not supported by ELECTRUVPN; inbound skipped")
        return None
    if net == "kcp":
        warn(f"{label}: mKCP tuning (seed/header/mtu) is not imported; defaults will be used")

    if mapped == "tcp":
        header = ((stream.get("tcpSettings") or stream.get("rawSettings") or {}).get("header") or {}).get("type", "none")
        if header not in ("", "none"):
            warn(f"{label}: TCP header type '{header}' (HTTP obfuscation) is not imported")
    elif mapped == "ws":
        ws = stream.get("wsSettings") or {}
        out["wsPath"] = ws.get("path") or "/"
        out["wsHost"] = ws.get("host") or (ws.get("headers") or {}).get("Host", "") or ""
    elif mapped == "grpc":
        out["grpcServiceName"] = (stream.get("grpcSettings") or {}).get("serviceName") or ""
    elif mapped == "httpupgrade":
        hu = stream.get("httpupgradeSettings") or {}
        out["httpUpgradePath"] = hu.get("path") or "/"
        out["httpUpgradeHost"] = hu.get("host") or ""
    elif mapped == "xhttp":
        xs = stream.get("xhttpSettings") or stream.get("splithttpSettings") or {}
        out["xhttpPath"] = xs.get("path") or "/"
        out["xhttpHost"] = xs.get("host") or ""
        out["xhttpMode"] = xs.get("mode") or "auto"
        if xs.get("extra"):
            out["xhttpExtraJson"] = json.dumps(xs["extra"])

    if sec == "tls":
        tls = stream.get("tlsSettings") or {}
        certs = tls.get("certificates") or []
        cert = certs[0] if certs else {}
        if cert.get("certificateFile") and cert.get("keyFile"):
            out["tlsSettings"] = {"certFile": cert["certificateFile"], "keyFile": cert["keyFile"],
                                  "sni": tls.get("serverName") or ""}
        else:
            warn(f"{label}: TLS uses inline/missing certificate files; set the certificate in the panel after import")
            out["tlsSettings"] = {"certFile": "", "keyFile": "", "sni": tls.get("serverName") or ""}
    elif sec == "reality":
        r = stream.get("realitySettings") or {}
        rs = r.get("settings") or {}
        out["realitySettings"] = {
            "dest": r.get("dest") or r.get("target") or "",
            "serverNames": r.get("serverNames") or [],
            "privateKey": r.get("privateKey") or "",
            "publicKey": rs.get("publicKey") or r.get("publicKey") or "",
            "shortIds": r.get("shortIds") or [],
        }
        if not out["realitySettings"]["publicKey"]:
            warn(f"{label}: REALITY public key missing in the source; generate a new keypair in the panel")
    elif sec != "none":
        warn(f"{label}: security '{sec}' is not supported; inbound skipped")
        return None

    out["network"] = mapped
    out["security"] = sec
    return mapped, sec, out


def main():
    if len(sys.argv) < 2 or sys.argv[1] in ("-h", "--help"):
        print(__doc__)
        sys.exit(1)
    db_path = sys.argv[1]
    out_dir = sys.argv[2] if len(sys.argv) > 2 else "electruvpn-import"
    if not os.path.isfile(db_path):
        sys.exit(f"Database not found: {db_path}")
    os.makedirs(out_dir, exist_ok=True)

    con = sqlite3.connect("file:" + quote(os.path.abspath(db_path)) + "?mode=ro", uri=True)
    con.row_factory = sqlite3.Row
    tables = {r[0] for r in con.execute("SELECT name FROM sqlite_master WHERE type='table'")}
    if "inbounds" not in tables:
        sys.exit("This does not look like a 3x-ui database (no 'inbounds' table).")

    traffic = {}
    if "client_traffics" in tables:
        for r in con.execute("SELECT * FROM client_traffics"):
            d = dict(r)
            traffic[d.get("email", "")] = d

    used_emails, used_subs = set(), set()
    written = skipped = client_total = 0

    for row in con.execute("SELECT * FROM inbounds ORDER BY id"):
        ib = dict(row)
        label = f"inbound #{ib['id']} '{ib.get('remark') or ''}' (port {ib['port']})"
        proto = (ib.get("protocol") or "").lower()
        if proto not in SUPPORTED:
            warn(f"{label}: protocol '{proto}' is not supported; skipped")
            skipped += 1
            continue
        ib["protocol"] = proto
        converted = convert_stream(ib, label)
        if converted is None:
            skipped += 1
            continue
        network, security, stream_out = converted
        if security == "reality" and network not in ("tcp", "xhttp", "grpc"):
            warn(f"{label}: REALITY is only supported on TCP/XHTTP/gRPC; skipped")
            skipped += 1
            continue
        if not ib.get("enable", 1):
            warn(f"{label}: was DISABLED in 3x-ui; it will import as enabled, disable it in the panel afterwards")

        settings_src = jload(ib.get("settings"), {})
        if proto == "shadowsocks":
            method = settings_src.get("method") or "aes-256-gcm"
            if method.startswith("2022-"):
                warn(f"{label}: Shadowsocks-2022 server key is not imported; re-create this inbound in the panel")
            settings_out = {"method": method}
        else:
            settings_out = {}

        sniff = jload(ib.get("sniffing"), {})
        clients_out = []
        for c in settings_src.get("clients") or []:
            email = (c.get("email") or "").strip()
            if not email:
                email = f"user-{ib['id']}-{len(clients_out) + 1}"
            base, n = email, 1
            while email in used_emails:
                n += 1
                email = f"{base}-{n}"
            if email != base:
                warn(f"{label}: duplicate client email '{base}' renamed to '{email}'")
            used_emails.add(email)

            sub = (c.get("subId") or "").strip()
            if not sub or sub in used_subs:
                if sub:
                    warn(f"{label}: subscription id of '{base}' is shared with another client; a new one was generated")
                sub = rand_token()
            while sub in used_subs:
                sub = rand_token()
            used_subs.add(sub)

            t = traffic.get(c.get("email", ""), {})
            enabled = bool(c.get("enable", True)) and bool(t.get("enable", 1))
            if not enabled:
                warn(f"{label}: client '{email}' was disabled in 3x-ui; it will import as enabled")
            cl = {
                "email": email,
                "enable": True,
                "limitIp": int(c.get("limitIp") or 0),
                "totalGB": to_gb(c.get("totalGB")),
                "expiryTime": pos_ms(c.get("expiryTime"), f"{label} client '{email}'"),
                "up": int(t.get("up") or 0),
                "down": int(t.get("down") or 0),
                "subId": sub,
                "subscriptionEnabled": True,
                "subscriptionStatus": "active",
                "subscriptionTokenVersion": 1,
                "subscriptionRefreshMin": 15,
                "subscriptionAddressType": "all",
                "subscriptionAutoUpdate": True,
                "primaryAddressType": "domain",
            }
            if proto in ("vless", "vmess"):
                cl["uuid"] = c.get("id") or ""
                if proto == "vless" and c.get("flow"):
                    cl["flow"] = c["flow"]
            else:
                cl["password"] = c.get("password") or ""
            clients_out.append(cl)

        inbound_out = {
            "remark": ib.get("remark") or f"imported-{ib['id']}",
            "enable": True,
            "listen": "" if ib.get("listen") in (None, "", "0.0.0.0") else ib["listen"],
            "ipMode": "ipv4",
            "subscriptionEnabled": True,
            "subscriptionDefaultAddress": "ipv4",
            "subscriptionPreset": "standard",
            "port": int(ib["port"]),
            "protocol": proto,
            "network": network,
            "security": security,
            "settings": json.dumps(settings_out),
            "streamSettings": json.dumps(stream_out),
            "sniffing": bool(sniff.get("enabled", True)),
            "tag": f"imported-{ib['id']}",
            "labels": "imported",
            "expiryTime": pos_ms(ib.get("expiry_time"), label),
            "totalGB": to_gb(ib.get("total")),
            "up": int(ib.get("up") or 0),
            "down": int(ib.get("down") or 0),
        }
        name = f"inbound-{ib['id']}-{proto}-{ib['port']}.json"
        with open(os.path.join(out_dir, name), "w", encoding="utf-8") as fh:
            json.dump({"inbound": inbound_out, "clients": clients_out}, fh, indent=2, ensure_ascii=False)
        os.chmod(os.path.join(out_dir, name), 0o600)
        written += 1
        client_total += len(clients_out)
        print(f"OK: {name} ({len(clients_out)} clients)")

    con.close()
    print()
    print(f"Done: {written} inbound file(s), {client_total} client(s) written to {out_dir}/ ; {skipped} skipped; {len(warnings)} warning(s).")
    print("Next: panel -> Inbounds -> Import, then select the files one by one.")
    print("Files contain client secrets: keep them private and delete them after importing.")


if __name__ == "__main__":
    main()
