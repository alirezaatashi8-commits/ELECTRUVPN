# ELECTRUVPN BETA1 — Deep Audit

## Fixes
- Removed duplicated V35 Command Center UI block.
- Recovery status now computes `latestVerified` from the actual latest backup archive instead of a hardcoded false value.
- Backup verification checks archive readability, recorded size, and presence/readability of the database entry.
- Preflight only evaluates enabled inbounds and only requires Xray or sing-box when an enabled inbound actually uses that engine.
- Removed stale Settings Center wording that suggested two-factor authentication.
- Added `scripts/deep-audit-v35.sh` regression checks.

## Verification limitation
The environment used for this release has Go 1.23.2 while `go.mod` requires Go 1.27.x. Full Go build/test therefore requires an environment with Go 1.27.x and dependency access. Static, frontend, shell, Python, routing, privacy and release audits can still be run locally and are included in the release process.
