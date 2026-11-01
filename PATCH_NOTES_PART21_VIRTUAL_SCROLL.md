# PART 21 — Lightweight Virtual Scroll for long lists

Files: `web/assets/js/app.js`, `web/assets/css/style.css`, `web/index.html`

## Goal
When an inbound list or a client list inside an expanded inbound grows large, only the visible window of rows is rendered. This keeps scrolling smooth and reduces DOM / RAM pressure without changing pagination or any existing feature.

## Behaviour
- Activates automatically when the current list has more than ~35 visible items.
- Uses a fixed row height estimate + spacer elements above/below the visible window.
- Works with existing server-side pagination (page size stays 50).
- No API or data model change.

## Scope
- Desktop inbound table rows
- Clients list inside an expanded inbound (when many clients)

## Safety
- Feature flags and all existing actions (edit, QR, bulk, expand, search) remain identical.
- Falls back to normal full render when the list is short.
