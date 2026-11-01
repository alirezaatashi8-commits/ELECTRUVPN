# Part 11 — Import of a single 3x-ui inbound failed with "not a readable SQLite database or 3x-ui SQLite backup"
- Cause: `XUIImport` chose the parser from the file extension. Anything not ending in .json/.sql/.dump (e.g. the `.txt` or extension-less file produced by 3x-ui's per-inbound "Export") was opened as SQLite and rejected. A single-inbound export was also not understood by the JSON parser (it required a top-level `inbounds` array and object-typed settings).
- internal/api/handlers/xui_import_handler.go: file type is now detected from content (SQLite magic header, PGDMP, leading `{`/`[`, SQL text). `parseXrayJSON` now accepts a single 3x-ui inbound export (settings/streamSettings as JSON strings), arrays of them, `{"obj": ...}` API responses, and full Xray configs; `clientStats` traffic is carried over. Share links (vless://…) get a clear explanation instead of the SQLite error. SQLite open errors now include the underlying cause.
- web/index.html: file pickers also accept `.txt`.
- Added internal/api/handlers/xui_import_single_test.go.
- RELEASE_SOURCE_MANIFEST.txt regenerated.
