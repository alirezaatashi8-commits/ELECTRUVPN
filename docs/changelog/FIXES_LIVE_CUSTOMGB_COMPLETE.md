# ElectruVPN BETA — Completion pass (Custom GB UI + Live User Apply + Flags)

## Completed / activated

### 1. Custom GB (خرید حجم دلخواه) — panel UI
- Backend + Telegram already existed; panel Settings → Shop had no controls.
- Added full UI under **فروشگاه تلگرام**: enable, price/GB, min/max GB, days, inbound ID.
- JS: `shop.customGB` state, `loadCustomGB()`, `saveCustomGB()` wired to `/api/pro/shop/custom-gb`.

### 2. Live user apply (hot path without full Xray restart)
- Xray generated API now exposes **StatsService + HandlerService**.
- New `internal/xray/handler_api.go`: `LiveAddUser`, `LiveRemoveUser`, `PingAPI`, `LiveApplyEnabled`.
- `core.Server`: `LiveUserApplyEnabled`, `RegenerateConfigOnly`, `TryLiveAddClient`, `TryLiveRemoveClient`.
- Client **Create** and **Delete** try live HandlerService first when Settings flag `flag_liveUserApply` is on; on failure they fall back to full `RegenerateAndRestartXray` (safe).
- On successful live apply, config is still written to disk so the next cold start matches runtime.

### 3. Experimental feature flags (functional, not decorative)
- `flag_liveUserApply` — enables live user path (default off for safety).
- `flag_experimentalLoadBalancer` — smart subscription selection uses `weighted` strategy and more candidates when on.
- UI Feature Flags list + Observability/Metrics help panel updated.
- Metrics remain env-gated (`ELECTRU_METRICS_TOKEN`); UI documents how to enable.

### 4. Prior gofmt fix
- `internal/models/shop.go` and `ticket.go` formatting corrected so `scripts/validate.sh` passes.

## How to activate

1. **Custom GB**: Settings → فروشگاه → enable + set price → Save. User bot shows «حجم دلخواه».
2. **Live user apply**: Settings → Beta Feature Flags → enable `liveUserApply` → Save. Then create/delete clients; Xray may skip process restart when API is healthy.
3. **Experimental LB**: enable `experimentalLoadBalancer` for stricter multi-node ranking on smart subscriptions.
4. **Metrics**: `electruvpn metrics enable` or set `ELECTRU_METRICS_TOKEN` in env.

## Safety notes
- Live apply is **opt-in**. Structural inbound changes still use full restart.
- sing-box protocols (Hysteria2/TUIC/WG/AnyTLS/ShadowTLS) always use full restart.
- If `xray api adu/rmu` fails, panel automatically falls back to restart + rollback on error.
