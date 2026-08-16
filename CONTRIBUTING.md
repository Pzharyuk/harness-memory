# Contributing

## Prerequisites

- Go 1.24 or newer
- Docker (for local Postgres)
- Helm (only needed to lint the chart)

## Setup

Copy `.env.example` to `.env` and adjust if needed. Start Postgres:

```
make dev
```

Compose is Postgres-only for now (`55432:5432`). `memoryd` will be added later.

## Tests

```
make test
```

Run one package:

```
go test ./internal/config/ -count=1
```

Integration tests talk to a real Postgres and **skip** unless `MEMORY_TEST_DATABASE_URL` is set. CI always sets it. Locally, after `make dev`:

```
MEMORY_TEST_DATABASE_URL=postgres://memory:memory@127.0.0.1:55432/memory?sslmode=disable go test ./...
```

## Adding a migration

1. Add a new numbered SQL file under `db/migrations/` (for example `002_foo.sql`).
2. Keep it additive and Postgres 16 compatible. Do **not** add `pgvector` or an `embedding` column.
3. Cover the schema change with a store test that uses `MEMORY_TEST_DATABASE_URL`.

## Lint and build

```
make lint
make build
```
