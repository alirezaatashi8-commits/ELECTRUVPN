# ELECTRUVPN BTA1 — final bug-fix pass

Panel name / version shown in the UI, API and metrics: **ELECTRUVPN BTA1** (`internal/version`).

## Fixed
1. **Build break (server):** `cmd/server/main.go` called `telegram.MigrateSecretSettings`, which did not exist.
   Implemented in `internal/telegram/settings.go` (encrypts legacy plaintext bot tokens at rest; no-op without
   `ELECTRU_CONFIG_ENCRYPTION_KEY`).
2. **Build break (portal):** unused `net/http` import removed from `internal/api/handlers/portal_handler.go`.
3. **Telegram Mini App dead on load:** `applyDashboard` used `await` inside a non-async function (JS SyntaxError,
   whole script failed). Function and its callers are now async/awaited.
4. **Auto-extend loop:** a client expired by time with only GB auto-extend configured got GB added on every
   sync tick forever. GB extension now cures quota exhaustion, day extension cures expiry; both configured = full renewal.
5. **Version string typo:** `BWTA1` -> `BTA1`; UI no longer prefixes it with "v"; login page "BETA 1 1" duplicate removed.
6. **OpenAPI contract:** 56 registered routes were undocumented; added, duplicate `ID` parameter refs removed.
7. **Release manifest:** `RELEASE_SOURCE_MANIFEST.txt` regenerated (installer verifies SHA-256 of every file).
