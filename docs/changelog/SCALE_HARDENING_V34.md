# V34 Deep Audit / Hardening

## Changes
- Viewer API/UI responses now redact `Client.SubID`, because it is the credential embedded in the public subscription URL.
- Inbound update/delete runtime rollback now restores the exact revision captured for that operation rather than whichever revision happens to be newest later.
- Removed the obsolete latest-revision rollback helper to prevent accidental reintroduction of the race-prone path.
- Backup pruning now processes at most 200 rows per batch instead of loading the entire backup history into memory.
- V33 scale architecture retained: paginated Inbounds, lazy per-Inbound Clients, and paginated global Clients.

## Verification limits
- Static syntax and project self-tests can run locally.
- Full Go build/test requires Go 1.27.x and dependency access. The current audit container only has Go 1.23.2 and cannot reach proxy.golang.org, so full dependency-backed `go test ./...` is not claimed as passed here.
