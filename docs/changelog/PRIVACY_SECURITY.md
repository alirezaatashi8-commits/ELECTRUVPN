# ELECTRUVPN privacy/security profile

## What strict mode protects

- Xray and sing-box use encrypted server-side DNS instead of inheriting the host resolver.
- Xray blocks private/local destination ranges by default to reduce accidental LAN access and SSRF-style abuse.
- Subscription generation cannot fall back to the HTTP Host header in strict mode.
- Client device registration stores a keyed hash of the source IP and does not retain the raw User-Agent.
- The panel can be bound to `127.0.0.1` behind nginx/CDN/reverse proxy.
- Public TCP/UDP DNS and DoT ports are blocked by the privacy hardening script.
- VPN endpoint host and panel/subscription origin are independent configuration values.

## Important limitation

A VPN endpoint's public IP cannot be made invisible to a client that connects directly to that endpoint. To keep the **panel origin** private, use a separate panel domain/reverse proxy/CDN and bind the panel to localhost. The VPN endpoint itself necessarily has a reachable address.

## Recommended deployment

- `ELECTRU_PUBLIC_HOST=vpn.example.com` — only for VPN configs.
- `ELECTRU_SUBSCRIPTION_BASE_URL=https://sub.example.com` — separate public subscription hostname.
- `ELECTRU_PANEL_BIND_ADDR=127.0.0.1` — admin panel origin stays local.
- Put the panel behind nginx/CDN with TLS and restrict origin access at the firewall when the provider exposes fixed proxy ranges.
- Do not put analytics scripts, third-party fonts, or external JavaScript into the panel.
- Run `scripts/security-selftest.sh` after deployment and after firewall changes.


## Operator-requested panel access exception

The panel is intentionally **not isolated from the VPN/proxy path**. `ELECTRU_PANEL_VPN_ACCESS=1` keeps the panel listener reachable so an operator can open the panel using a client configuration issued by this installation. This is an explicit exception to the localhost-only origin hardening.

This does not disable the other privacy controls: encrypted DNS, private-network blocking, credential minimization, rate limiting, session controls, secure headers, and runtime validation remain enabled. For internet-facing deployments, use HTTPS and the panel's password/session protections.

### Client-side leak boundary

A server cannot mathematically guarantee that every third-party VPN client prevents WebRTC, split-tunnel, or OS-level IPv6 leaks. WireGuard share data therefore defaults to `0.0.0.0/0` and `::/0` plus explicit DNS servers. For Xray/sing-box clients, use their TUN/full-tunnel mode and route DNS through the tunnel; the panel never claims a browser-level WebRTC guarantee it cannot enforce.
