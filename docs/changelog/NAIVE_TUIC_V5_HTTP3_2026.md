# NaiveProxy + TUIC v5 with HTTP/3 Camouflage (2026)

## Overview

ElectruVPN now supports two high-stealth protocols designed to defeat DPI/filtering
that targets classic VLESS/Trojan fingerprints:

### 1. TUIC v5 (QUIC / HTTP/3 camouflage)

- Engine: **sing-box** (`type: tuic`)
- Transport: QUIC with ALPN `h3` — looks like Chrome loading a heavy site over HTTP/3
- Defaults:
  - `congestion_control`: `bbr`
  - `udp_relay_mode`: `quic`
  - `alpn`: `["h3"]`
  - optional `zero_rtt_handshake`, `sni`, `heartbeat`
- Share link (client-compatible):

```
tuic://UUID:PASSWORD@host:port?congestion_control=bbr&udp_relay_mode=quic&alpn=h3&sni=www.cloudflare.com&version=5#remark
```

**Recommended inbound settings (JSON in panel):**
```json
{
  "congestionControl": "bbr",
  "udpRelayMode": "quic",
  "zeroRTTHandshake": true,
  "alpn": ["h3"],
  "sni": "www.cloudflare.com",
  "heartbeat": "10s"
}
```
- Network: `quic`
- Security: `tls` (certificate required)
- Clients need UUID + Password

### 2. NaiveProxy-style (Chrome HTTP/2 or HTTP/3)

- Engine: **sing-box** (`type: http` + TLS + ALPN h2/h3)
- Traffic is standard HTTPS CONNECT through a TLS endpoint that advertises
  Chrome-like ALPNs — DPI sees ordinary browser traffic to your camouflage SNI.
- Share link:

```
naive+https://user:pass@host:port?padding=true&alpn=h2,http/1.1&sni=www.microsoft.com#remark
```

**Recommended inbound settings:**
```json
{
  "padding": true,
  "http3": false,
  "alpn": ["h2", "http/1.1"],
  "sni": "www.microsoft.com"
}
```
- For HTTP/3 preference: `"http3": true, "alpn": ["h3"]` and network `quic`
- Network: `tcp` (classic Naive) or `quic` when http3=true
- Security: `tls`
- Clients use Email (or username) + Password

## Client apps

| Protocol | Compatible clients |
|----------|-------------------|
| TUIC v5  | NekoBox, v2rayN (with TUIC), sing-box, Clash Meta, Streisand |
| Naive    | NekoBox, naiveproxy binary, some Clash Meta builds |

## Migration

1. Extract this release / pull changes
2. Restart panel (sing-box regenerates)
3. Create new inbound:
   - Protocol: `tuic` or `naive`
   - Security: `tls` + valid cert
   - Settings JSON as above
4. Assign clients (UUID+Password for TUIC; Password for Naive)
5. Copy share link from subscription / client page

## Notes

- TUIC and Naive are **sing-box only** (not xray-core).
- Use a realistic SNI (Cloudflare, Microsoft, Google) that resolves and has a real certificate chain when possible, or your own domain with a valid cert.
- Combine with multi-hop chain (Iran mid → EU exit) for maximum censorship resistance.
