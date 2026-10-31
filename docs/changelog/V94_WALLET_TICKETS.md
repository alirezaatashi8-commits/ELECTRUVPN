# V9.4 — Wallet, Auto-Renew, Ticket Bridge

## Wallet (user bot → 💰 کیف پول)
- Balance in Toman
- Top-up amounts: 50k/100k/200k/500k → card-to-card → receipt photo → admin ✅
- Auto-renew on/off + select plan; worker debits wallet when expired/over-quota

## Orders
- `kind=plan` (subscription) or `kind=wallet_topup`
- Admin bot / panel: same Approve / Reject buttons

## Tickets → Telegram
- New/reply tickets notify admin bot with Reply / Close buttons
- Admin taps Reply, sends next message → delivered to user bot chat

