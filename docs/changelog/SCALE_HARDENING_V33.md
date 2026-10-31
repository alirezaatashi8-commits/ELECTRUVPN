# ELECTRUVPN BETA V33 — Scale Hardening

This release keeps Clients nested inside each Inbound while removing the largest dashboard payload bottleneck.

## Changes

- Inbound list is server-side paginated (50 default, 10–100 accepted).
- Inbound search is server-side paginated and no longer preloads Clients.
- Each inbound returns `clientCount` instead of embedding every Client.
- Added `GET /api/inbounds/:id/clients` with page/pageSize pagination.
- Added authenticated `GET /api/clients` with limit/offset pagination and optional `inboundId` / `q` filters.
- Inline Clients remain inside the Inbound row; expanding an Inbound lazy-loads its Clients.
- Inline Client pagination prevents a large Inbound from rendering every Client at once.
- Subscription dashboard no longer depends on nested Clients being preloaded by `/api/inbounds`; it uses paginated client retrieval.
- Viewer responses continue to redact client credentials and inbound sensitive settings.

## Compatibility

The UI remains backward-compatible with the existing Inbound layout. Clients are not moved to a top-level navigation section.

## Validation limitation

Static validation, formatting, frontend syntax, shell syntax, routing tests, privacy regression tests, and release audit were run in the build environment. A full dependency-backed Go test/build was not possible there because the environment could not resolve `proxy.golang.org`; the installer retains the official Go 1.27.1 toolchain path for online builds.
