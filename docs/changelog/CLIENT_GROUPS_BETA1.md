# Client Groups (Organizational) — ELECTRUVPN BETA1

## Feature
Admin can organize clients (configs) into named groups so that multiple configs belonging to the same person / reseller / package are easy to identify and manage.

Examples:
- «احمد — بسته ۱۰۰ کانفیگ»
- «فروشگاه فلان — VIP»
- «Reseller-A»

## What was added

### Backend
- `POST /api/clients/bulk-action` now accepts action `set_group` with body field `group` (max 64 chars). Pure metadata — no Xray reload.
- `GET /api/clients/groups` — list distinct group names + member counts (includes `(ungrouped)`).
- `POST /api/clients/groups/rename` — rename a group across all its members (`{from, to}`).

### Frontend (Inbounds → expand inbound)
- Filter dropdown: show only clients of a selected group.
- Bulk toolbar when clients are selected:
  - «افزودن به گروه» → prompt for group name (existing names suggested).
  - Enable / Disable / Delete selected.
- Group badge on each client card (📁 GroupName).
- Existing free-text `Client.Group` field is reused (no migration required). Existing data is preserved.

## How to use
1. Open an Inbound and expand it.
2. Tick the checkboxes of the clients that belong to one person.
3. Click «افزودن به گروه» and type a clear name (e.g. the person's name).
4. Later use the group filter dropdown to see only that person's configs.

## Notes
- Group is an admin organizational label only; it does not affect subscription URLs or runtime behavior.
- For combined multi-client subscription links, continue to use the existing Subscription Groups feature.

## UI polish (follow-up)
- **Node Health / Topology / Fabric RTT tooltips**: hovering RTT shows Packet Loss, Congestion, Qdisc and related metrics.
- **Inbounds table Traffic column**: total traffic (up+down) per inbound with hover showing separate ↑ / ↓.
