# PART16 — Workers, auth, Hysteria2 (v2.9-era features)

Note: "Hysteria 3" does not exist upstream (latest is Hysteria 2, app/v2.9.x). Hysteria2 support was upgraded instead.

## Traffic worker
- Auto-extend no longer re-enables clients an admin disabled (only enabled or auto-expired clients are revived).
- Expiry auto-disable never overwrites a `revoked` subscription status.

## Auth
- LoginGuard failures now decay (15 min window) so old typos do not accumulate into a lockout; firstFail map is bounded/evicted.

## Hysteria2
- New settings: masquerade proxy/file/string, ignoreClientBandwidth, allowInsecure, pinSHA256, portHopping, salamander obfs validation.
- Share link emits sni, insecure, pinSHA256, mport (port hopping).
- Port hopping needs a server-side UDP range redirect (iptables/nftables) to the listen port.
- Not compiled here (no Go toolchain download); run `go build ./... && go test ./...`.
