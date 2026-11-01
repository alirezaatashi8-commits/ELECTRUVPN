# Part 10 — Import 3x-ui DB button missing on phones/tablets (frontend only, no backend changes)
- Cause: the whole inbound toolbar (Import JSON / Import 3x-ui DB) has class `inbound-desktop-controls`, which style.css hides below 1024px; the mobile "create" menu had no import entries.
- web/index.html: mobile menu ("سایر") now has Import JSON and Import 3x-ui DB (own hidden file inputs `inboundImportM` / `xuiImportM`, same handlers as desktop).
- Backend endpoint POST /api/inbounds/import-3x-ui unchanged.
- RELEASE_SOURCE_MANIFEST.txt regenerated.
