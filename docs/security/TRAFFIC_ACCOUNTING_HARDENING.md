# Traffic accounting hardening notes

## Changes in this package

- Usage is always the raw measured byte count; no other quantity is ever billed.
- The local Xray accounting path derives client usage from cumulative byte counters and stores upload/download separately.
- Agent-reported negative counters are now rejected before they enter the in-process traffic queue. The API response includes a `rejected` count.
- Added regression tests asserting raw counters are accounted as measured and missing subjects are ignored.

## Important limits: this is not a 100% anti-tamper guarantee

A source-code patch cannot guarantee that traffic accounting is impossible to bypass if an attacker has root access to the host, direct database write access, the panel signing/agent secrets, or control of a trusted node agent. In the current design, an enrolled agent submits absolute counters for email labels; the master aggregates the largest counter by email. The source archive does not establish a strict binding between each agent/node and the set of client identities it is allowed to report. A compromised or malicious enrolled agent may therefore be able to submit inflated counters for another known client. Do not describe this as fully prevented by the changes above.

## Required production controls for stronger assurance

1. Bind each agent cryptographically to a node and authorize every reported client against that node's assigned inbounds/client set on the server. Reject unknown or unassigned identities.
2. Keep agent tokens, database credentials, and panel admin credentials outside the source tree; rotate them after any suspected exposure.
3. Restrict database/network access, run services as unprivileged users, use TLS, and audit all agent traffic submissions with node ID and monotonic counter observations.
4. Persist per-node/per-client last-seen absolute counters and only accept monotonic increases; handle Xray restarts with explicit authenticated epochs rather than accepting arbitrary regressions/increases.
5. Treat panel UI/API validation as convenience only. Enforce accounting invariants in the server transaction and database permissions; prevent direct writes to usage columns except through the accounting service.
6. Add alerting for implausible counter jumps, replayed samples, counter resets, and agent identity changes.

## Verification

The Go test suite should be run in the project build environment. This archive has not been deployed to your server, and no claim is made that the entire project compiles or that production behavior has been verified from this environment.
