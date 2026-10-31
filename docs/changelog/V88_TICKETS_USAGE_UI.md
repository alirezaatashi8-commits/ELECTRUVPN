# V8.8 — Ticketing + Subscription usage UI

## Support tickets
- Models: `Ticket`, `TicketMessage`
- Admin API under `/api/pro/tickets`
- End-user API via subscription token:
  - `GET/POST /sub/:subId/tickets`
  - `GET /sub/:subId/tickets/:id`
  - `POST /sub/:subId/tickets/:id/reply`
- Public usage JSON: `GET /sub/:subId/usage` (percent, days left)

## Subscription Center UI
- Traffic usage progress bar (green → amber → red)
- Days remaining progress bar
- Tickets panel: list, thread, reply, close, open ticket for selected client

## Status values
`open` | `pending` | `closed`

