# PostgreSQL for ElectruVPN (high concurrency)

## Why PostgreSQL

SQLite serializes writers. Under simultaneous dashboard, agent heartbeats, subscription polls and traffic sync, PostgreSQL handles concurrent transactions far better.

## Quick start (Docker)

```bash
docker compose up -d --build
```

- Panel uses `ELECTRU_DB_DSN` → Postgres 17
- Pool defaults: max open 50, idle 20

## Native

1. Install PostgreSQL 16 or 17, create role/db `electruvpn`.
2. Set in env:

```env
ELECTRU_DB_DSN=host=127.0.0.1 user=electruvpn password=SECRET dbname=electruvpn port=5432 sslmode=disable TimeZone=UTC
ELECTRU_DB_MAX_OPEN=10
ELECTRU_DB_MAX_IDLE=3
ELECTRU_DB_CONN_MAX_LIFETIME_MIN=30
```

3. Start the panel — schema is created with AutoMigrate.

## Migrate existing SQLite data

```bash
# 1) Start panel once with DSN so tables exist
# 2) Stop panel
# 3) Run:
export ELECTRU_DB_PATH=/etc/electruvpn/electruvpn.db
export ELECTRU_DB_DSN="host=127.0.0.1 user=electruvpn password=SECRET dbname=electruvpn port=5432 sslmode=disable"
./scripts/migrate-sqlite-to-postgres.sh
# 4) Start panel with only ELECTRU_DB_DSN set
```

Always keep a copy of the SQLite file until verification.

## Tuning

| Env | Default | Role |
|-----|---------|------|
| ELECTRU_DB_MAX_OPEN | 10 | max concurrent DB connections from panel; raise for high concurrency |
| ELECTRU_DB_MAX_IDLE | 3 | idle pool; lower idle connection memory footprint |
| ELECTRU_DB_CONN_MAX_LIFETIME_MIN | 30 | recycle connections |

Docker Compose Postgres uses small-VPS defaults (`max_connections=50`, `shared_buffers=128MB`, `work_mem=4MB`). Raise them only when measured workload requires it.

