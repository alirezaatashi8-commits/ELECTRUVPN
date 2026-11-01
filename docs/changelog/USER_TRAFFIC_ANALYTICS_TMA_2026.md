# User Traffic Analytics Dashboard (TMA) — ELECTRUVPN BETA 1

## Features
- Daily / weekly / monthly usage charts in Telegram Mini App
- Day vs night split (06–18 vs 18–06 UTC)
- Peak, average, total for selected window
- Built from `usage_snapshots` (raw byte accounting)

## API
`GET /api/tma/usage/history?range=day|week|month`

Response includes `points`, `dayNight`, `peakBytes`, `totalBytes`, `avgBytes`.

## Branding
Panel display name: **ELECTRUVPN BETA 1**
