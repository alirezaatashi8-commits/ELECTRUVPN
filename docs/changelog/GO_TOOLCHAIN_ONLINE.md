# ELECTRUVPN BETA — Online Go Toolchain

The installer does **not** bundle a Go binary.

On Ubuntu/Debian it downloads the pinned official Go 1.27.1 archive from `https://go.dev/dl/`, verifies SHA-256, and installs it under `/opt/electruvpn-toolchain/go1.27.1`.

Supported Linux archives in this release:

- `go1.27.1.linux-amd64.tar.gz`
  - URL: `https://go.dev/dl/go1.27.1.linux-amd64.tar.gz`
  - SHA256: `63d339f0da5ab53635a56f2490a7984dfe12dfcff22ad749f63edaf590168445`
- `go1.27.1.linux-arm64.tar.gz`
  - URL: `https://go.dev/dl/go1.27.1.linux-arm64.tar.gz`
  - SHA256: `3450b45a3f9ee8568792736a5c5e70a1f2e9b36c35a8f74958c03e51d7d92bec`
- `go1.27.1.linux-386.tar.gz`
  - URL: `https://go.dev/dl/go1.27.1.linux-386.tar.gz`
  - SHA256: `3b72028095439d2bc0ce84e271cc70328a878d879020c5721eaa46df5f72fbc0`
- `go1.27.1.linux-armv6l.tar.gz`
  - URL: `https://go.dev/dl/go1.27.1.linux-armv6l.tar.gz`
  - SHA256: `44893f200fb034791d4188df9fc9b9e73eadbb5fceafd5166703f0b9bab73fc2`

Module downloads use:

- `GOTOOLCHAIN=local`
- `GOPROXY=https://proxy.golang.org,direct`
- `GOSUMDB=sum.golang.org`
- `GOPATH=/var/lib/electruvpn/go`
- `GOMODCACHE=/var/lib/electruvpn/go/pkg/mod`

The Go module cache is deliberately outside the source tree so Go never scans module-cache directories as project packages.
