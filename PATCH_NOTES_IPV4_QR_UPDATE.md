# ElectruVPN IPv4 / Subscription / QR update

This patch includes:

- Public IPv4 detection fallback for NAT-based VPS setups, using validated HTTPS IP-echo services when the outbound socket address is private/unavailable.
- Automatic IPv4 preference when a subscription URL is opened through a literal IPv4 without an explicit `family` query.
- When auto/manual IPv4 changes, explicitly configured subscription base URLs are rewritten only if their URL host is the old IPv4 literal; domain-based subscription URLs remain unchanged.
- Subscription URL generation from the current configured IPv4 and panel port/base path when no custom subscription base URL is configured.
- Diagnostic server logs for the specific branches that return HTTP 503 while building a subscription.
- Responsive, scrollable QR modals with visible configuration details and a persistent mobile subscription QR panel.
- Two-column action menus so actions next to clients/inbounds are not rendered as one long vertical list.
- Regenerated source manifest so the release integrity check matches the source files packaged here.

## Upgrade an existing server

Use the existing in-place upgrade command, not a fresh install:

```bash
sudo electruvpn update /path/to/ELECTRUVPN_BETA1_IPV4_QR_UPDATE.zip
```

The CLI creates a database backup before calling the release installer. The installer keeps `/etc/electruvpn/electruvpn.env`, uses a staged runtime swap, and rolls back the runtime if the service health check fails. Keep a separate VPS snapshot/backup as an additional precaution.

## Important IP migration limitation

The new IPv4 is applied to generated configuration links after a successful subscription refresh. If the old IPv4 stops routing to this VPS, clients holding a subscription URL that points to the old IPv4 cannot reach the server to refresh themselves. For fully automatic refresh across future IP changes, use a stable domain/DNS name for the subscription endpoint (and a valid TLS certificate for that domain). The panel can still embed the current IPv4 in the returned configuration links. If using a raw IP over HTTPS, the TLS certificate must be valid for the new IP or client apps may reject the subscription.

## Validation notes

JavaScript syntax and shell syntax are checked by the build-preparation process. The current build environment does not have Go 1.27.1 and cannot download the toolchain, so a full Go build/test run must be completed on a build host with the version required by `go.mod`.

## BETA1.2 — Client menu and Live monitor UI (2026-10-27)
- Moved single-client creation, one-click generated client creation, and bulk client creation entry points into the Create Config dropdown, grouped by inbound.
- Removed the old inline create-client action strip from the expanded client list; client listing, grouping, and bulk management remain available there.
- The Live client monitor now stays centered on mobile instead of inheriting the global bottom-sheet alignment. Its panel is height-limited and scrollable.
- Added a subtle, slow, full-spectrum RGB border around the Live client monitor, with reduced-motion support.
- Existing installer/update workflow and server-side logic are otherwise unchanged.
