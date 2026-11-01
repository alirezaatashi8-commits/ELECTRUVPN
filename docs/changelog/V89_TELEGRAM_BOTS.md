# V8.9 — Interactive Telegram Bots

## Admin Bot
Configure in **Settings → Telegram Bots**:
1. Create bot with @BotFather → paste **Admin Bot Token**
2. Get your numeric ID from @userinfobot → **Admin numeric IDs** (comma-separated for multiple admins)
3. Save → Test

Commands (only listed admin IDs):
| Command | Action |
|---------|--------|
| /stats | Live panel report |
| /backup | Create backup now |
| /clients | Client counts |
| /inbounds | Inbound list |
| /nodes | Node status |
| /tickets | Open tickets |

## User Bot
Separate bot token (another @BotFather bot recommended).

Users send:
```
/status <sub_id>
/status client@email
```
or just the sub ID / email as plain text.

Reply shows traffic % / used / limit and days remaining.

## API
- `GET/POST /api/pro/telegram/config`
- `POST /api/pro/telegram/test-admin`
- `POST /api/pro/telegram/test-user`

Bots use long-polling; no public webhook URL required. They start with the panel process and pick up new tokens within ~15s.

