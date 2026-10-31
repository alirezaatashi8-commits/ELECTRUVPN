# Performance & Observability Hardening V2

This pass preserves the BETA1 feature set and focuses on low-risk modernization.

## Go

- Migrated process/application logs from `log` to Go's standard `log/slog`.
- JSON is the default format for Loki/ELK-compatible structured ingestion.
- `ELECTRU_LOG_FORMAT=text` enables human-readable local logs.
- `ELECTRU_LOG_LEVEL=debug|info|warn|error` controls verbosity.
- `ELECTRU_LOG_SOURCE=1` includes source locations.
- Automation now has `TickContext` and propagates request/worker cancellation into GORM.
- Live Xray samples use `RWMutex` for read-heavy access and prune entries older than five minutes.
- Exported handler methods have GoDoc comments.

## OpenAPI

- Added reusable HTTP error responses for 400/401/403/404/500.
- Added schemas for automation rules and scheduled tasks.
- Documented automation, scheduler, operations, device and subscription endpoints introduced by the v3.5 surface.
- Documented query/path parameters and request bodies where the implementation validates them.

## Assets

- Static assets receive ETag + one-day browser caching in the Go server.
- Reverse proxy adds one-day asset caching and gzip for CSS/JS/JSON/SVG.
- Added a pinned esbuild production pipeline at `scripts/build-assets.sh` and `package.json`.
- The source tree intentionally keeps source asset paths as the compatibility default; release builds can generate `app.min.js` and `style.min.css` without breaking existing deployments.

## Validation

PASS:

- OpenAPI YAML parsing
- Web routing matrix
- JavaScript syntax
- Reverse-proxy shell syntax
- Asset build script shell syntax
- Go formatting

Blocked by environment:

- Full Go test/build requires Go 1.27; the current execution environment has Go 1.23.2 and cannot download a newer toolchain.
- The minified asset build requires esbuild; it is declared and pinned but is not installed in the offline execution environment.
