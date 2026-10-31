# Part 13 — Bug audit, section 2: Clients
- **Auto-disabled clients could not be revived.** When the traffic worker disables a client (quota/expiry) it sets `subscription_status = expired`. Enabling the client from the panel left that status in place, and "Reset traffic" (single and bulk) did not re-enable it at all. Now: enable/disable (single, bulk, kick) keep `subscription_status` in sync (`active`/`disabled`; `revoked` is never touched), and a traffic reset re-enables a client that was switched off only because of its quota (not if it is past its expiry date) and applies it to xray.
- **Reseller credit/quota leaked on failure.** If applying a new client to xray failed, the client was removed but the reseller's wholesale credit, `allocated_gb` and `client_count` were not given back. Fixed.
- **Bulk delete** did not release reseller quota/count or refund wholesale credit (single delete did). Fixed.
- **Client volume edit:** the allocated-GB adjustment was computed after the row had been updated (could always be 0); it now uses the value from before the update.
- Persian/UTF-8: bulk-create prefix, email length, group names are now counted/truncated by characters, not bytes.
- Bulk reset checks that the DB transaction actually began.
