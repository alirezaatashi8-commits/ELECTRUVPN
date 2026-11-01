# CPU low-load follow-up — 2026-10-29

## Changes

- Reduced periodic node/agent health scanning from 45s to 90s.
- Reduced scheduled automation polling from 30s to 60s.
- Reduced local Xray traffic accounting from 45s to 120s. This pass scans client quota/expiry state and usage snapshots as well as querying Xray stats, so it is the most expensive recurring panel-side path identified in this source review. Quota/expiry enforcement can consequently be delayed by up to about two minutes between passes.
- Reduced remote traffic-delta flush cadence from 15s to 30s; samples are still batched.
- Reduced outbound chain health checks from 90s to 180s.
- Increased dashboard overview cache TTL to 30s.
- Dashboard polling now runs every 90s and only fetches dashboard data while the dashboard tab is visible; returning to the browser refreshes data for the active tab only.

## Important limitations

These are conservative load reductions, not a guarantee of 1% CPU. Xray/Sing-box data-plane traffic, client count, DB size, TLS/network probes, and other host processes can dominate CPU. Validate on the target VPS with per-process CPU measurements before treating the target as met.
