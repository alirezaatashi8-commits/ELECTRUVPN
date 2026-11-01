# Part 12 — Bug audit, section 1: Inbounds
- **Import JSON (`POST /api/inbounds/import`) always failed when the file had clients.** The export stores clients both inside `inbound` and in `clients`; GORM inserted the first copy with old IDs and the explicit loop then violated the unique email/subId indexes. Now clients are inserted once; email is de-duplicated, a fresh subId is issued (old one would have shared a subscription URL), traffic bases are reset, remark is made unique, the inbound goes through the same validation as Create, and the port check is network-aware.
- **Persian/UTF-8 text was cut in the middle of a character** (client prefix on Create, 3x-ui import names, Mass-provision prefix length) → new rune-safe `truncateRunes`. Labels are limited by characters, not bytes (Persian labels were capped at 24 chars).
- Create: remark is stored trimmed (the duplicate check already trimmed it).
- Mass provision with clients: every copy re-used the same Trojan/SS password, the same WireGuard keys/address and old traffic baselines. Now regenerated/reset.
- Bulk enable also clears the maintenance lock; bulk action on non-existent IDs returns 404 instead of "success".
- Rollback accepts an optional `{"revisionId": N}` (default: latest, as before).
- 3x-ui import checks only the ports the inbound's transport really needs (TCP-only inbound was rejected when UDP of that port was busy).
