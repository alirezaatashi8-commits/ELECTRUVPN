# V30 — Global Inbound Search

## What changed

- Added a dedicated server-side `GET /api/inbounds/search` endpoint.
- Search is performed directly against the database, so the number of stored inbounds does not define a search ceiling.
- The API returns only the current page to the browser; `pageSize` controls returned rows, not the searchable dataset.
- Search supports inbound name/remark matching anywhere in the name and optional protocol filtering.
- Search results are paginated (default 50, maximum 100 per response).
- Empty results return a successful response with `total: 0`; the UI displays `No inbound found`.
- Added a database index on the inbound `remark` column for indexed name operations where the database can use it.
- Search input is intentionally submitted by the Search button or Enter key instead of firing a request for every keystroke.
- Existing Clients remain inside each Inbound; opening a result uses the existing inbound row and expansion UI.
- Query parameters are bound through GORM rather than interpolated into SQL.

## API

`GET /api/inbounds/search?q=<name>&page=1&pageSize=50`

Optional:

`protocol=vless|vmess|trojan|...`

The `total` value is the number of matching database rows, while `hasMore` indicates whether another page exists.

## Scalability behavior

There is no configured maximum number of inbounds that can be searched. A database containing 100,000, 500,000, or more inbounds is searched server-side; the browser never receives the complete dataset just to locate one name.
