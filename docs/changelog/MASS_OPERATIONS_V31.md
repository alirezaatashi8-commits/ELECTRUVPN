# ELECTRUVPN BETA V31 — Mass Operations

This release keeps the existing V30 global inbound search and adds the missing high-scale operations layer.

## Included

- Global server-side inbound search remains database-backed; the number of stored inbounds does not define the search domain. Pagination limits only returned rows.
- Bulk inbound enable/disable/delete remains available.
- Bulk client actions remain available.
- Config templates remain available.
- Inbound health/doctor-style checks remain available.
- Platform job/operation infrastructure remains available.
- New **Mass Provisioning** endpoint: `POST /api/inbounds/mass-provision`.
- New **Dry Run** mode validates names, ports, and capacity without modifying the database.
- Mass provisioning clones a validated source inbound, assigns unique names/tags and either sequential or automatically selected free ports.
- Optional client cloning creates new UUID/subscription identifiers rather than reusing credentials.
- The complete batch is committed in one database transaction.
- Runtime configuration is regenerated once after the batch is committed.
- If runtime apply fails, the newly created clients/inbounds are removed and runtime is regenerated again.
- Frontend adds a dedicated **⚙ Mass Provision** action and a Dry Run button.

## Scale and safety

Mass provisioning accepts 1–10,000 records per operation. This is an operation safety boundary, not an inbound/search capacity limit. Search itself has no fixed inbound-count ceiling; only page size controls the number returned to the browser.

Search and operation inputs use parameterized database queries. Go's official `database/sql` guidance recommends passing parameter values separately rather than formatting them into SQL strings to avoid SQL injection.
