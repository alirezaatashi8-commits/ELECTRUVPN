# ELECTRUVPN BTA1 — Multi-Hop Tunnel UI expansion

## Why
Large admins need clear multi-stage tunnels (e.g. client → panel entry → Iran mid → optional second relay → EU exit). Backend already supported Mid + Mid2 + Exit with dialerProxy nesting; the panel UI only exposed Mid + Exit.

## UI
- Full-width card: **سیستم تانلینگ پیشرفته · Multi-Hop**
- Live path diagram: کاربر → Entry → Mid → [Mid2] → Exit → اینترنت
- Separate sections for Mid / Mid2 / Exit with node picker, protocol (VLESS Reality, Trojan, SS2022, WireGuard), Reality keys, SNI
- Clear buttons, active host status for all three hops, last error
- Auto strategy includes round_robin

## Frontend (app.js)
- State, load, save, and `onHopNodePick` cover Mid2 + Reality fields for every hop
- Credentials mapped correctly per protocol (UUID vs password)

## Backend
- Save persists `active_mid2_host`
- Xray chain builder unchanged (already nested mid → mid2 → exit balancer)

## Operator tip
1. Put the Iran (or other DPI-front) relay as Mid.
2. Optional second country as Mid2.
3. Final free-net exit as Exit (+ rotate addresses for failover).
4. Enable chain → Xray regenerates with nested dialerProxy.
