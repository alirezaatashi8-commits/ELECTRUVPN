# ELECTRUVPN BETA — Routing / SPA refresh fix

Fixes included:

- SPA HTML now injects the configured panel base path and uses a `<base>` URL so relative assets/API paths continue to resolve on `/dashboard/<tab>` and randomized base paths.
- API 401, logout, and password-change redirects use the configured base path instead of relative `./login` URLs.
- Root `/` no longer redirects to a randomized panel base path, preventing disclosure of the hidden base path.
- Login/dashboard HTML is served with `Cache-Control: no-store, no-cache, must-revalidate, private` and `Pragma: no-cache` because the same SPA shell can represent different session states.
- The root entry checks the database-backed session revocation timestamp, matching the authenticated API middleware rather than validating JWT signature/expiry alone.
- Browser history routing uses the injected base path rather than deriving it from the current `/dashboard/...` URL.

Static validation performed:
- `scripts/validate.sh`
- `node --check web/assets/js/app.js`
- `bash -n install.sh`
- `gofmt -l` on Go sources

## Base path root fix

When `ELECTRU_BASE_PATH=/`, the HTML base tag must be `<base href="/">`, not `<base href="//">`. The server therefore leaves the injected base attribute empty at the root because the HTML template supplies the trailing slash. For a non-root base such as `/panel-a1b2c3d4`, the resulting tag is `<base href="/panel-a1b2c3d4/">`.

If a non-root base path is configured, the root `/` intentionally does not redirect to it; use the full printed panel URL (including the base path).
