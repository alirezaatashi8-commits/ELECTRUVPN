# ELECTRUVPN BETA1 — Consolidated UX and resilience changes

## Inbound
The existing four-step wizard is retained. Forward navigation validates the prior step; Review performs server-side draft validation; final creation checks the selected port and validates the draft before submitting.

## Client
Added a dedicated three-step client creation wizard: Identity, Limits, Review. Limits use integer GB because the API request type is an integer. Expiry is entered in days and converted to a millisecond timestamp at submission. The API remains responsible for authoritative quota, role, and inbound compatibility checks.

## Mobile
Both wizards use independent scrollable bodies, fixed action footers, minimum touch targets, 16px form text on narrow screens, and safe-area padding.

## Network notes
No protocol can be guaranteed undetectable or unfilterable. Preserve a known-good configuration, benchmark latency/throughput/loss before and after each change, and use an independently routed backup endpoint for availability rather than relying on port changes alone.

## Read-only REALITY preflight
`scripts/reality-preflight.sh /path/to/xray-config.json` validates JSON shape, common inbound fields, and REALITY essentials without printing private keys or changing server state. If an Xray binary is present, it also runs Xray's config test. This is a configuration preflight, not a DPI-resistance guarantee or a network throughput test.
