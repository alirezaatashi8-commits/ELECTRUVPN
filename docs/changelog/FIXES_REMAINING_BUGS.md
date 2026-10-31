# Remaining bug-fix pass

## Bugs fixed

1. **CustomGB JSON broken in panel** — `CustomGBConfig` had no `json` tags; UI read `enabled`/`pricePerGB` as undefined. Added tags + explicit gin maps on GET/POST.

2. **Winback settings never loaded/saved** — UI called `loadWinbackSettings` / `saveWinbackSettings` but methods did not exist (Promise.all rejected; Save button no-op). Implemented both against `/api/pro/shop/winback`.

3. **Winback save response shape** — POST returned raw struct with `PlanIDs` array; UI expects `planIds` string. Response now matches GET.

4. **Empty Xray inbound tag** — Generated config could emit empty `tag` for legacy rows. Fallback to `inbound-<port|id>` in config generation and live apply.

5. **Live apply disk inconsistency** — After successful `adu`/`rmu`, failed config write still reported success and skipped restart. Now fail-closed (returns false → full restart path).

6. **Client Update always restarted** — Enable-only toggles can use live path; empty update payloads no longer restart Xray.

7. **gofmt** on touched Go files; `validate.sh` + routing self-test + `node --check` pass.
