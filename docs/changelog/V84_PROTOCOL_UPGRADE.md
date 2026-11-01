# ElectruVPN BETA1 V8.4 — Protocol & Panel Upgrade (2026)

## Highlights

### Protocols & transports (2026 best practice)
- **VLESS + REALITY + Vision** still primary for strong censorship environments
- **VLESS + XHTTP + REALITY (stream-one)** promoted as recommended modern stack
- XHTTP default mode: `stream-one` under REALITY, `auto` otherwise
- REALITY multi-SNI expanded (Microsoft, Apple, Cloudflare, Google)
- REALITY `requireMlkem` support (opt-in post-quantum hybrid key share)
- Stronger TLS-hello **fragment** defaults (`50-120` / `10-20`)
- **Shadowsocks 2022** (`2022-blake3-aes-256-gcm`) as default SS method
- Presets for **AnyTLS**, **ShadowTLS**, **Trojan+REALITY**, **Hysteria2 gaming**, **TUIC v5**

### Panel
- Expanded one-click inbound presets in UI
- Quick Anti-Filter uses updated 2026 defaults
- PostgreSQL primary (from V8.3)
- Speed limit fully removed (from V8.3)

### Already supported cores
| Core | Protocols |
|------|-----------|
| Xray | VLESS, VMess, Trojan, Shadowsocks + TCP/XHTTP/gRPC/WS/HTTPUpgrade/mKCP |
| sing-box | Hysteria2, TUIC, WireGuard, AnyTLS, ShadowTLS |

## Recommended stacks (2026)

1. **Strong DPI / Iran / CN**: VLESS + TCP + REALITY + Vision + Fragment + Mux  
2. **Modern direct**: VLESS + XHTTP + REALITY (stream-one)  
3. **Behind CDN**: VLESS + XHTTP/HTTPUpgrade + TLS  
4. **Weak network / gaming**: Hysteria2 + TLS  
5. **Simple AEAD**: Shadowsocks 2022  

