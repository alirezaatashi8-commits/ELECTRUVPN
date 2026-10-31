# Deep audit — panel sections (2026-10-23)

## Fixed
1. **Control tab empty clients** — `/clients` returns `data`, but `loadUltra` read `obj` → always []. Now uses `data` + pagination.
2. **loadTab wiring** — Subscription loads tickets; Control loads shop; Settings reloads telegram+shop.
3. **Finance routeMap** (prior) — tab reachable.
4. **Subscription clients without inbound** (prior).
5. **Bot /start shop|renew|wallet** (prior).
6. **loadPro / loadFinance / loadSubscription** — partial API failure isolation.

## Verified OK
- TMA routes match frontend calls (shop/buy, pin, configs, devices/profile, gifts, trial, …)
- Portal `/api/portal/*` matches portal UI
- DB AutoMigrate includes shop, wallet, referral, tunnel, tickets, devices, …
- Finance API routes registered under `/api/pro/finance/*`

## Still recommend on server
`go build ./...` and smoke-test each sidebar tab once after deploy.
