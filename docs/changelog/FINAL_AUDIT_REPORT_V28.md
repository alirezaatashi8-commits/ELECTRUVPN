# ELECTRUVPN BETA — V28 Final Build Audit

Date: 2026-09-30

## Release goal

This release fixes the fresh-Ubuntu installer/build blockers observed during the V27 server installation and keeps the isolated build design.

## Fixed blockers

1. `install.sh: port_is_listening: command not found`
   - Added a self-contained `port_is_listening()` helper before the installer uses it.
   - Uses `ss -ltnH` and safely handles numeric TCP ports.

2. `InboundHandler.buildClientLinks undefined`
   - `InboundHandler` now delegates to the existing `ClientHandler.buildClientLinks` implementation instead of calling a nonexistent method.

3. `deviceModelSource declared and not used`
   - The value is now applied to `ClientDevice.ModelSource` during subscription device registration.
   - `Source` is explicitly set to `subscription_refresh` for this path.

4. V1 DTO type mismatch
   - `models.Protocol`, `models.Network`, and `models.Security` are explicitly converted to strings when building the public V1 DTO.

## Installer/build hardening retained

- Isolated build workspace under `/tmp/electruvpn-build.*`.
- Go module cache outside the project tree under `/var/lib/electruvpn/go`.
- Atomic staging deployment and rollback behavior from V27.
- Release source manifest verification.
- `-trimpath` production build.
- Ubuntu/Debian + systemd preflight.
- Go 1.27.x baseline.

## Validation performed here

- `bash -n install.sh` passed.
- Modified source references were checked directly.
- Release manifest regenerated for all critical modified files.

The final production compilation still needs to occur with the installer's Go 1.27.x toolchain on the target server because the current build container only has Go 1.23.2 and has no external network access for downloading Go 1.27.1.
