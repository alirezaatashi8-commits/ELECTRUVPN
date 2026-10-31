# ELECTRUVPN BETA v27 — Installer Build-Isolation Audit

Date: 2026-09-30

## Scope

This release addresses the installer failure observed when the project was extracted under `/root` and Go's default GOPATH/module cache also existed under `/root/go` (or was moved to another directory still inside the module tree).

## Root cause

Go recursively evaluates packages below the current module root. When the source module was `/root` and a Go module cache existed at `/root/go/pkg/mod`, cached module directories such as `github.com/davecgh/go-spew@v1.1.1` were interpreted as source packages. This produced errors of the form:

`import path "github.com/electruvpn/electruvpn/go/pkg/mod/...@vX" should not have @version`

## v27 changes

- Installer verifies the release source manifest before compilation.
- Installer creates an isolated temporary build tree under `/tmp/electruvpn-build.*`.
- Known cache directories (`go`, `go-*`, `.electru-go`, `pkg/mod`) are excluded from the build tree.
- `GOPATH` and `GOMODCACHE` are set to `/var/lib/electruvpn/go` and `/var/lib/electruvpn/go/pkg/mod`.
- Build uses `go build -trimpath`.
- If the release does not contain `go.sum`, a successful `go mod tidy` generates it and copies it back to the source tree.
- Build/staging cleanup remains active through the atomic deployment phase.
- Runtime deployment and systemd configuration continue to use the isolated build output.

## Static validation performed in the release workspace

- `bash -n install.sh` — passed.
- Release source manifest hashes — passed after updating the installer hash.
- The source tree contains the required `go.mod`, server entry point, web entry point, deploy unit, and installer scripts.

## Server-side validation already completed before v27 packaging

On the target Ubuntu ARM64 server:

- Ubuntu 24.04.4 LTS — detected.
- Architecture `aarch64` — detected.
- Go `1.27.1 linux/arm64` — detected.
- `go mod tidy` — completed successfully after moving the cache outside the module root.
- `go build -o /tmp/electruvpn-server ./cmd/server` — completed successfully.
- Resulting binary — approximately 35 MB.
- Runtime/linker checks — no build failure observed.
- Final check script — `FAIL: 0`, `FINAL RESULT: PASS`.

## Known non-fail warnings from the pre-install server check

- `electruvpn.service` was not yet installed — expected before installation.
- `/opt/electruvpn` was not yet present — expected before installation.
- The temporary test GOPATH was under `/root` — v27 moves the installer build workspace and Go cache to `/var/lib/electruvpn/go`.

## Test limitation

A complete Go test suite was not claimed in this environment. The release was validated by the successful server-side dependency resolution and ARM64 production build plus installer/static checks described above.
