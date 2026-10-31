
## v27 installer build-isolation fix — 2026-09-30

- Fixed Go builds when the release is extracted under `/root` or another module root that also contains a GOPATH/module cache.
- Installer now creates a clean temporary build workspace under `/tmp/electruvpn-build.*`.
- Go `GOPATH`/`GOMODCACHE` are isolated under `/var/lib/electruvpn/go`, outside the source tree.
- Known cache directories such as `go`, `go-*`, `.electru-go`, and `pkg/mod` are excluded from the build workspace.
- `go build` uses `-trimpath` for a cleaner reproducible binary path.
- Generated `go.sum` is copied back to the source only when the release does not already contain one.
- Build and staging cleanup traps are preserved through deployment.

# ELECTRUVPN BETA — consolidated bug-fix history

This file consolidates the installation, routing, security, performance and
runtime problems found during the Beta development/debugging cycle.

## Installation / toolchain

- Pinned the default Go toolchain to Go 1.23.0 for reproducible fresh installs.
- Added multiple official Go download fallbacks and archive validation.
- Never untars a new Go archive over an existing `/usr/local/go`; the old tree is removed first.
- Added normalized Linux architecture detection:
  - x86_64 / amd64 — Intel or AMD 64-bit
  - x86 / 386 — 32-bit Intel-compatible
  - ARM64 / aarch64
  - ARMv7
  - ARMv6
- Go `GOARCH`/`GOARM` values are selected from the detected architecture.
- sing-box installation now discovers the actual official release asset for the detected architecture instead of assuming a fixed filename.
- sing-box archives are checked as valid tarballs and, when GitHub supplies a SHA-256 digest, verified before installation.
- Installed sing-box is executed once after installation to catch a wrong-architecture or corrupted binary immediately.
- Xray's own official installer remains responsible for the Xray binary, while ElectruVPN disables the standalone Xray systemd service so two Xray instances cannot fight over ports.
- Frontend libraries are served locally after installation; the running admin browser does not need access to Tailwind/Alpine/Chart.js/QRCode CDNs.
- Frontend vendor downloads use retries, fallbacks and atomic temporary files, and installation stops if a required library is missing.
- Installer-generated JWT secrets are random and the production binary refuses the default development JWT secret.
- Installer-generated admin passwords are random, displayed at install time, and the temporary password file is removed after successful first startup.
- Manual first-run database initialization now logs a generated password if no installer/environment password was supplied, avoiding an unreachable random admin account.
- Certificate setup supports domain or automatically detected server IP, with explicit TCP/80 confirmation.
- Firewall and fail2ban configuration are installed as part of the production setup.

## Login / authentication / session

- First-run admin is `admin` with a generated password, not a fixed `admin/admin` credential.
- Passwords are stored as bcrypt hashes.
- Login brute-force protection is keyed by IP + username.
- API request rate limiting is applied to login and authenticated API traffic.
- Session cookies are HttpOnly, SameSite=Lax and Secure when HTTPS is used.
- Session validity checks JWT signature/expiry and `password_changed_at`, so changing a password revokes older sessions.
- Logout and password-change redirects are base-path aware.
- Viewer users are prevented from receiving client UUID/password/private/public key material through inbound listings.
- Role changes protect the owner role and last-owner account.
- State-changing browser API calls use same-origin protection to reduce CSRF risk.

## Base Path / routing / Dashboard

- Randomized non-root panel base paths no longer redirect from `/`, avoiding disclosure of the hidden path.
- The configured base path itself serves Login or Dashboard depending on session validity.
- Root-mounted mode correctly renders `<base href="/">`; the previous invalid `<base href="//">` bug is removed.
- Non-root mode renders `<base href="/panel-.../">` consistently.
- Dashboard history navigation uses the configured base path.
- Refreshing nested dashboard routes no longer causes `/dashboard/assets/...` asset lookups.
- API 401 redirects use the configured base path instead of relative `/dashboard/login`-style paths.
- Login redirects to the base-aware `/dashboard` route.
- Backup download and client export links are base-path aware instead of using relative `./api/...` URLs.
- HTML is sent with `no-store` caching because the same entry URL can represent Login or Dashboard depending on the session.
- CSP `base-uri` is now `self`, matching the intentional `<base>` element used by the SPA.
- Added routing and architecture self-tests to the validation suite.

## Backend / API

- Fixed stale imports and incorrect database delete calls found during native Go builds.
- Fixed WireGuard subscription public-key handling by deriving the public key from the server private key where appropriate.
- Inbound create/update requests use protocol/transport/security validation before configuration generation.
- No-op inbound updates are handled without unnecessary restarts.
- Xray and sing-box generated configurations are validated before write/restart where the corresponding engine supports validation.
- Agent jobs are allowlisted, bound to their target node and claimed atomically.
- Agent result updates validate ownership, status and progress ranges.
- Job target nodes are validated before dispatch.
- Order status values are validated instead of accepting arbitrary strings.
- Webhook delivery blocks private/local destinations and redirects and disables proxy use for SSRF-sensitive delivery.
- Subscription/device registration uses transactions and a composite client/device uniqueness constraint.
- Public subscription and agent endpoints remain separate from browser-session API authentication by design.
- API key scopes and role checks remain explicit for management endpoints.

## Xray / sing-box / networking

- Xray stats API remains bound to loopback rather than being exposed publicly.
- Xray process management is separated from the panel's systemd unit.
- Auto-recovery no longer immediately fights an intentional/manual service stop when disabled.
- BBR rollback restores the correct sysctl keys.
- BBR optimization uses the classic BBR + fq configuration and does not impose per-user bandwidth shaping.
- Service-plan `SpeedMbps` is metadata only; the Beta does not intentionally throttle user bandwidth.
- Load balancing/failover decisions are separate from user speed limits.
- Live client status is explicitly traffic-observation based because Xray stats do not expose a definitive active-session list.

## Database / performance

- SQLite uses WAL, busy timeout, foreign keys, prepared statements and a bounded connection pool.
- Dashboard polling pauses while the browser tab is hidden.
- Traffic synchronization avoids unnecessary database writes when counters have not changed.
- Dashboard/node-fabric query patterns were reduced to avoid common N+1 behavior.
- Usage/performance/job indexes were added where repeated queries need them.
- Scheduled backups and retention are supported; backup finalization and filename collision handling were hardened.

## Reverse proxy / deployment

- The Nginx helper now respects the configured ElectruVPN base path instead of assuming `/`.
- Nginx forwards the original Host/forwarded scheme so secure-cookie and same-origin checks continue to work behind TLS termination.
- The helper binds the panel to localhost after Nginx is ready and removes the direct UFW panel-port rule.
- Deployment rollback reports honestly when no versioned artifact snapshot exists instead of pretending a rollback happened.

## Validation boundary

Static validation, shell syntax, JavaScript syntax, Go formatting, architecture matrix
and routing matrix tests are included. A full `go build`/`go test` of this source archive
cannot be honestly claimed in an environment without access to the Go module proxy/VCS
and without the module cache. The installer therefore performs the real native build on
the target server and stops on any compiler error instead of starting a partial install.

## Inbounds Beta modernization pass

- Added smart/random/recommended port allocation and preflight port checks.
- Added draft config validation before create/apply plus Xray/sing-box preview.
- Added REALITY x25519 keypair + short-ID generation from the installed Xray binary.
- Added inbound runtime status, search/filtering, labels, bulk enable/disable/delete, import/export.
- Added bounded inbound revision snapshots with rollback for edits/deletes.
- Added duplicate remark detection and stronger protocol/transport compatibility validation.
- Viewer role no longer receives raw inbound Settings/StreamSettings secrets.
- Added automatic rollback after failed inbound create/delete runtime apply.
- Added database migration for InboundRevision and inbound labels.

## Beta — Inbound/Client control-center expansion
- Added per-Inbound traffic reset without resetting unrelated users.
- Added per-Client traffic reset using persistent Xray counter baselines; resetting one client no longer calls Xray `statsquery -reset` and no longer affects other clients.
- Added global traffic reset with explicit Xray counter reset plus database baseline reset.
- Added Inbound clone, maintenance mode, health score, and historical traffic analytics endpoints/UI.
- Added Client secret rotation and bulk lifecycle actions (enable/disable/delete/reset/rotate).
- Added client groups and optional automatic traffic/time extension metadata with automatic renewal during traffic synchronization.
- Preserved historical usage snapshots when a current accounting window is reset.

## v6 — full runtime/API hardening

- atomic Xray/sing-box config writes
- graceful process termination with timeout fallback
- startup crash detection
- coordinated runtime rollback across engines
- bounded JSON request bodies
- bounded login-guard memory
- API v1 inbound response no longer exposes client credentials
- webhook URL/SSRF hardening
- transactional bulk client creation
- collision-resistant WireGuard address allocation
- strict initial-client creation failure handling
- added focused Go unit tests

## v12 final audit fixes
- Restored the missing request body size middleware referenced by the API router.
- Bounded attacker-controlled in-memory login/rate-limit maps.
- Validated automation/scheduler inputs and disabled invalid legacy schedules.
- Fixed IPv6-safe client share links and public-host validation.
- Enforced HTTPS for agent enrollment endpoints.
- Removed panel service write access to its own installation directory.
- Reworked backup restore to execute outside the panel service cgroup via systemd-run and stop the live panel before DB replacement.
- Removed long-lived caching of fixed-name frontend assets.

## v20 — final deep audit hardening

- Enforced subscription/device policies consistently across direct refresh, device registration, and subscription groups.
- Fixed new-device approval so unapproved devices remain inactive until administrator approval.
- Made primary subscription address policy effective.
- Validated client subscription address types against the inbound IP mode during create/update.
- Made inbound deletion transactional and preserved all v19 subscription policy fields during inbound cloning.
- Added public subscription-group rate limiting.
- Replaced unsalted User-Agent hashes with keyed HMAC identifiers.
- Hardened IPv4/IPv6/domain-aware security self-tests and privacy-mode endpoint handling.
- Updated CI/installer/toolchain/dependency baselines to Go 1.27 / Go 1.27.1.
- Explicitly preserved the product decision of no 2FA/TOTP or Google/OAuth login.

## v21 — Quick gRPC
- Added Quick gRPC creation flow on the Inbounds page.
- Creates VLESS + gRPC + REALITY with generated service name, UUID and REALITY keys.
- Supports quota in GB, expiry in days, explicit port or random free high port.
- Supports IPv4, IPv6 and Dual Stack address modes.
- Added authenticated `POST /api/inbounds/quick-grpc` endpoint.
- Fixed Dual Stack port probing so Linux `bindv6only=0` does not produce false port conflicts.
- Preserved the Advanced Inbound editor for custom gRPC transport settings.

## v22 — Quick WebSocket / HTTPUpgrade
- Added one-click VLESS+WebSocket creation with quota, expiry, free/random port selection, IPv4/IPv6/Dual and address-type selection.
- Added one-click VLESS+HTTPUpgrade creation with the same controls.
- Quick WS/HTTPUpgrade automatically select a valid managed TLS certificate when available.
- Strict security mode refuses to create insecure Quick WS/HTTPUpgrade configurations without usable TLS material.
- Added routing, OpenAPI and UI controls for both wizards.

## v23 — VLESS Transport Expansion
- Added Quick XHTTP + REALITY.
- Added Quick VLESS TCP + REALITY Vision.
- Added reusable VLESS Config Profiles: one logical Client can reference multiple VLESS inbounds without duplicating quota/expiry/device policy.
- Subscription delivery includes enabled profiles ordered by priority and skips disabled/maintenance profiles.
- Added VLESS Config Doctor API/UI action.
- Added VLESS presets for XHTTP+REALITY, XHTTP+TLS, HTTPUpgrade+TLS and TCP+REALITY Vision.
- No 2FA, TOTP, Google Authenticator or Google OAuth added.


## V24
- Fixed multi-profile VLESS traffic accounting so profile transport usage is aggregated into the owning Client.
- Profile stats aliases now use profile IDs to avoid collisions.
- Fixed client/bulk reset to include profile aliases.
- Fixed Client deletion and Inbound deletion lifecycle cleanup for ClientConfigProfile records.
- Extended inbound rollback snapshots to preserve affected profiles.
- Fixed profile deletion so historical traffic/quota is preserved after runtime reload.
- Added a composite uniqueness constraint for Client + Inbound profiles.
