# ELECTRUVPN BETA v21 — Quick gRPC Audit

## Implemented
- Added authenticated `POST /api/inbounds/quick-grpc`.
- Added Inbounds UI button: `Quick gRPC`.
- Quick flow creates VLESS + gRPC + REALITY plus exactly one client.
- Inputs: GB quota, days, explicit port or random free port, IP mode/address type, optional names.
- `port=0` selects a free random high port (20000–65000) using cryptographic randomness.
- Manual port is checked against DB allocations, panel/Xray API reservations, and actual local sockets.
- gRPC service name, UUID, REALITY keypair and short ID are generated server-side.
- Client expiry and traffic quota are applied at creation.
- Runtime apply is atomic in behavior: if Xray restart/apply fails, created rows are removed and previous runtime is restored.
- Advanced Inbound editor remains unchanged and available for custom gRPC settings.
- Fixed Dual Stack port probing on Linux when `net.ipv6.bindv6only=0`.
- Updated OpenAPI and README documentation.

## Security
- Quick wizard intentionally does not create public VLESS+gRPC with `security=none`.
- REALITY is generated automatically using the installed Xray binary.
- Private REALITY key is never accepted from the browser.
- Endpoint requires owner/admin/operator authentication.
- No 2FA, TOTP, Google Authenticator or Google OAuth/Login code was added.

## Validation
PASS: gofmt
PASS: Node.js syntax check
PASS: shell syntax checks
PASS: web routing self-test
PASS: OpenAPI YAML parse
PASS: release audit
PASS: runtime 2FA/Google-auth scan
PASS: archive hygiene

LIMITATION: Full Go tests could not execute in this offline build environment because the module requires Go 1.27 and the environment could not download the Go 1.27 toolchain from proxy.golang.org. No compile error was observed before that toolchain-download stop condition.
