# RAM footprint tuning — 2026-10-29

This release reduces default database memory reservations for small VPS deployments while preserving the ability to tune connection pools through environment variables.

- PostgreSQL pool defaults: 10 maximum open connections and 3 idle connections (previously 50/20). Existing explicit `ELECTRU_DB_MAX_OPEN` and `ELECTRU_DB_MAX_IDLE` values still take precedence.
- SQLite pool defaults: 4 maximum open connections and 2 idle connections (previously 8/4).
- SQLite page cache target: 4 MiB per connection (`_cache_size=-4096`) instead of 20 MiB (`-20000`). SQLite still uses WAL, a 5-second busy timeout, normal synchronous mode, and foreign-key enforcement.
- Docker Compose PostgreSQL defaults: `max_connections=50`, `shared_buffers=128MB`, `work_mem=4MB`, `maintenance_work_mem=64MB`, and `wal_buffers=8MB`; shared memory is set to 192 MiB to leave overhead above shared_buffers.

These changes reduce avoidable database memory use on small servers; actual system RAM usage depends on PostgreSQL/Xray, the Go heap, Linux page cache, and current workload. They do not guarantee a specific RAM percentage. For high-concurrency installations, increase pool/database settings based on measurements.
