# PART 23 — Client tags & admin notes

Files: `internal/models/client.go`, `internal/api/handlers/client_handler.go`, `web/assets/js/app.js`, `web/assets/css/style.css`, `web/index.html`

## What
Admins can attach **tags** (comma-separated labels) and a free-form **notes** field to each client.

## Backend
- New columns on `clients`: `tags` (varchar 256, indexed), `notes` (varchar 1024).
- AutoMigrate on next panel start creates the columns (SQLite & PostgreSQL).
- `PUT/POST` client update accepts `tags` and `notes`.
- Tags are normalized: trimmed, empty dropped, max 12 tags, max 32 chars each.
- Notes trimmed, max 1000 chars. Never exposed on public subscription pages.

## Frontend
- Edit Client modal: tags input + notes textarea.
- Client rows show tag pills and a notes preview (full text on hover).
- Filter dropdown «تگ» next to the existing group filter inside an expanded inbound.

## Safety
- Group / subscription fields unchanged.
- Existing clients get empty tags/notes (nullable-safe).
- No public/sub portal leakage of notes.
- Cache bust: `?v=beta50`.
