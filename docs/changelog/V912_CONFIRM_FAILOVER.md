# V9.12 — Confirm-before-Failover (Semi-Automatic Outbound)

## Scenario
When an outbound exit host becomes unhealthy (high RTT / TCP fail) and the chain has
`RotateOnFail=true` + `ConfirmBeforeFailover=true` (default):

1. Health probe marks the host unhealthy.
2. Instead of automatic rotation, an interactive Telegram message is sent to admin IDs
   with buttons:
   - ✅ Yes — pick best healthy server and switch
   - ❌ No — leave traffic as-is
   - Optional direct-pick buttons for top 3 alternatives (with measured RTT)
3. Only after admin confirmation does `ActiveExitHost` change.
4. Pending requests expire after 5 minutes (no silent switch).

## Settings (TunnelChain)
| Field | Default | Meaning |
|-------|---------|---------|
| `rotateOnFail` | true | Enable rotation logic at all |
| `confirmBeforeFailover` | **true** | Require Telegram confirmation before switch |

Set `confirmBeforeFailover=false` to restore classic automatic failover.

## Files touched
- `internal/models/tunnel_chain.go` — new field
- `internal/routing/outbound_health.go` — confirm path + pending store + Apply/Deny
- `internal/notify/telegram.go` — `TelegramWithMarkup`
- `internal/telegram/admin_bot.go` — `fo:yes` / `fo:no` / `fo:pick` callbacks
- `internal/api/handlers/chain_handler.go` — persist new field

## Operator flow
1. Keep multiple exits in `RotateAddresses` (comma-separated).
2. Leave `ConfirmBeforeFailover=true` (safe default for 10–20 nodes behind central exit).
3. When a node dies you receive a Telegram prompt → confirm → best RTT host is selected.
