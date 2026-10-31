# Advanced Honeypot & Multi-Hop Tunnel Chaining (2026)

## Honeypot (Advanced Cyber Security)

- **Multi-port tarpit**: primary listen + optional `honeypot_extra_ports` (e.g. 2222,3389,8080) for fake SSH/RDP/HTTP.
- **Fake admin login page**: realistic HTML login form when path looks like /admin,/login,/panel (wastes scanners, optional credential capture on POST).
- **Auto-ban**: after N probes in window → record in `honeypot_bans`, call fail2ban-client / ufw / iptables.
- **Telegram alert**: rate-limited notification to admin bot with IP, mode, probe count, banned flag.
- **Optional global blacklist**: POST banned IP to configurable webhook URL.
- New settings: `honeypot_auto_ban`, `honeypot_ban_threshold`, `honeypot_ban_window_min`, `honeypot_ban_duration_h`, `honeypot_telegram_alert`, `honeypot_fake_login`, `honeypot_extra_ports`, `honeypot_global_list`.

### Fail2ban jail
```
cp scripts/electruvpn-honeypot.fail2ban.filter /etc/fail2ban/filter.d/electruvpn-honeypot.conf
cp scripts/electruvpn-honeypot.fail2ban.local /etc/fail2ban/jail.d/electruvpn-honeypot.conf
systemctl reload fail2ban
```

## Multi-Hop Tunnel Chaining

- Existing mid + exit chain extended with **optional Mid2 hop**.
- Path: Client → Panel entry → Mid (e.g. Iran) → Mid2 (optional relay) → Exit (e.g. Germany) → Internet.
- Each hop uses `dialerProxy` / sockopt so traffic is nested (strong against DPI).
- Panel API / model fields: `mid2Address`, `mid2Port`, `mid2Protocol`, `mid2UUID`, … `mid2NodeId`, `activeMid2Host`.
- Auto / manual selection still applies; only one chain enabled at a time.

## Migration
- `EnsureSettings` seeds new honeypot keys.
- AutoMigrate will add `honeypot_bans` table and new TunnelChain columns on next panel start.
