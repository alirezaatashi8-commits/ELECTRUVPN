# V9.8 — Speed Test · Usage Charts · Smart Notify

## 1) In-App Speed / Auto-Ping (TMA)
- Tab **سرعت** in Mini App
- `GET /api/tma/speedtest/targets` — node + inbound probe list with server load
- Client-side RTT (3 samples, best) via `fetch` no-cors from the user's device
- Ranking score = latency × (1 + load%)
- Best node highlighted with badge (عالی/خوب/متوسط/ضعیف)
- Optional `POST /api/tma/speedtest/server-ping` for panel-side TCP secondary signal

## 2) Smart Expiry & Low-Quota Notifications
- Enhanced `RunExpiryNotices` (every 15m)
- Windows: ~24h and ~2h before expiry
- Volume: ≤10% and ≤2% remaining (supports TotalGB + TrafficLimitBytes)
- **Re-notify after renew**: watermark-based dedupe; resets when remain >15% or expiry pushed out
- CTA: «تمدید در مینی‌اپ» (WebApp) + «تمدید سریع» + «وضعیت»

## 3) Usage Charts (7-day)
- `GET /api/tma/usage/history?days=7`
- Daily deltas from `usage_snapshots`
- Lightweight CSS bar chart on Home tab
