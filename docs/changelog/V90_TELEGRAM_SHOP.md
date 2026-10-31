# V9.0 — In-Bot Shop & Billing (Card-to-Card)

## Panel: Settings → فروشگاه تلگرام
1. Enable shop, set **card number**, holder name, payment instructions
2. Add **plans**: name, GB, days, price (Toman), **Inbound ID** (which config template)
3. Review **orders** → Approve / Reject
4. Optional **Broadcast** to all linked Telegram customers

## User bot flow
- Menu: وضعیت اشتراک من | خرید / تمدید | راهنما
- Status card with progress bar `[██████░░░░] 60%`, days left, node name
- Shop: pick plan → show card number (copyable) → user sends receipt photo
- Admin notified → approve → client created/renewed → sub link sent

## Admin bot
- `/orders` list pending
- `/approve_123` / `/reject_123`

## Auto notices
Every ~6h: users with expiry in ~3 days or ~1 day get reminder + «تمدید سریع»

