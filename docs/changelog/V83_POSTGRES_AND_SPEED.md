# ElectruVPN BETA1 V8.3 — PostgreSQL + Speed Limit Removal

## What changed

1. **Speed limit removed completely**
   - No more `speedMbps` on service plans.
   - UI, API and model cleaned.
   - Users always get the full available node bandwidth.

2. **PostgreSQL is the primary database (2026)**
   - Preferred: set `ELECTRU_DB_DSN` to a PostgreSQL 16/17 connection string.
   - SQLite remains only as a zero-config local/dev fallback when `ELECTRU_DB_DSN` is empty.
   - Docker Compose ships `postgres:17-alpine` and wires the panel automatically.
   - Backup: on Postgres the panel archives configs + a marker file; use `pg_dump` for full DB dumps.
   - Health checks and system info report the active backend (`postgres` / `sqlite`).

## Quick start (recommended)

```bash
# From the project root
docker compose up -d --build
# Panel: http://SERVER_IP:2087
# Postgres: 127.0.0.1:5432  user/pass/db = electruvpn
```

## Native install with PostgreSQL

1. Install PostgreSQL 16 or 17 and create a role/database.
2. Set in `/etc/electruvpn/electruvpn.env`:

```env
ELECTRU_DB_DSN=host=127.0.0.1 user=electruvpn password=YOUR_SECRET dbname=electruvpn port=5432 sslmode=disable TimeZone=UTC
```

3. Run the usual `install.sh` or start the systemd unit.
4. Panel auto-migrates schema on first start.

## Migrating existing SQLite data

There is no automatic SQLite→Postgres data migrator in this release.
Recommended path:
- Fresh install on Postgres.
- Or export clients/inbounds via the panel UI / API and re-import after switching DSN.

## Full database backup (Postgres)

```bash
pg_dump -h 127.0.0.1 -U electruvpn -d electruvpn -Fc -f electruvpn-$(date +%Y%m%d).dump
```

