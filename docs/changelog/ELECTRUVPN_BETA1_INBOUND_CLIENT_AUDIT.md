# ELECTRUVPN BETA1 — Inbound / Client audit

## Applied
- Inbound creation now performs local validation and asks the server to validate the generated draft before POSTing the create request.
- The UI exposes validation-in-progress state and disables the final create action while validation is running.
- REALITY destination host:port and private key are validated before draft submission.
- Client creation validates the selected inbound, label, finite traffic limit, integer IP/device limit, and expiry value before API submission.
- Added an IP/device limit input to the inline client form and made it responsive on small screens.
- New inbound defaults are speed-first: fragment obfuscation, TCP masks, and Mux are disabled by default because they may add overhead or reduce compatibility. They remain available for explicit opt-in.

## Limitations / verification
- `node --check web/assets/js/app.js` passed.
- `go test ./...` could not run because Go requested toolchain `go1.27.1` and network access to `proxy.golang.org` was unavailable.
- No live Xray integration, throughput benchmark, browser device matrix, or production API test was possible in this environment.
- Existing UI still renders the full Inbound field set in one scrollable form; the step indicator validates progression but the field groups are not yet fully isolated into separate step panels. This remains a known UX follow-up rather than being represented as completed.

## Speed notes
There is no universal setting that guarantees higher VPN throughput. Speed is affected by server CPU and kernel, route/ISP, protocol, MTU, congestion, client implementation, and network conditions. This pass only changes new-inbound defaults to avoid enabling potentially costly fragmentation, TCP masks, and Mux without a clear need. Benchmark on a staging node before production rollout.
