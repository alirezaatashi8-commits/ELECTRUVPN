# PART 25 — Deeper RAM / CPU reduction

## Polling (less API + less main-thread work)
- Dashboard live overview: 6s → **10s**
- Heavy dashboard batch: 90s → **120s**
- Xray logs auto-tail: 3s → **6s**
- Online users: 5s → **10s**
- Client live modal: 5s → **8s**

## Memory caps
- netHistory points: 24 → **16**
- Xray logs kept in UI: max **100** lines
- Subscription access logs: 80 → **40**
- Subscription client preload hard cap: 10000 → **2000**

## Virtual list CPU fix
- Templates no longer call `virtClientView()` 4 times per render
- Use `virtSubCache` / `virtClientCache` (single recompute on scroll / load / filter)
- Chart.js animation duration → **0**

## GPU / compositor
- List cards (inbound clients, sub rows): **no backdrop-filter**
- Global glass blur: 12px → **8px**
- float/drift/pulse animations **paused** until hover
- Aurora / live border slower + lighter

## Cache bust
`?v=beta52`
